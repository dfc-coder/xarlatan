package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

// StreamingResponder is an optional responder extension used only when the
// downstream player also supports response-scoped chunk playback.
type StreamingResponder interface {
	RespondStream(context.Context, string, llm.ContentDelta) (conversation.Result, error)
}

// StreamingPlayer exposes response-scoped chunk playback. Write may be called
// multiple times; Finish drains audible audio and runs the post-playback guard;
// Stop aborts immediately.
type StreamingPlayer interface {
	Write(context.Context, audio.Buffer) error
	Finish(context.Context) error
	Stop() error
}

// Coordinator owns voice-turn lifecycle while long-lived workers own stage work.
type Coordinator struct {
	dependencies Dependencies
	sequence     atomic.Uint64
}

// NewCoordinator validates and constructs the event-driven voice coordinator.
func NewCoordinator(dependencies Dependencies) (*Coordinator, error) {
	if dependencies.Input == nil {
		return nil, invalidApplication("voice input is nil")
	}
	if dependencies.Transcriber == nil {
		return nil, invalidApplication("transcriber is nil")
	}
	if dependencies.Responder == nil {
		return nil, invalidApplication("responder is nil")
	}
	if dependencies.Synthesizer == nil {
		return nil, invalidApplication("synthesizer is nil")
	}
	if dependencies.Player == nil {
		return nil, invalidApplication("player is nil")
	}
	if dependencies.Observer == nil {
		dependencies.Observer = nopObserver{}
	}
	if dependencies.View == nil {
		dependencies.View = nopView{}
	}
	return &Coordinator{dependencies: dependencies}, nil
}

// Run starts one persistent worker set and reuses it across voice turns.
func (c *Coordinator) Run(ctx context.Context) error {
	if c == nil {
		return invalidApplication("coordinator is nil")
	}
	ctx = nonNilContext(ctx)
	workers := newWorkerSet(ctx, c.dependencies)
	workers.start()
	defer workers.stop()

	for {
		if ctx.Err() != nil {
			return nil
		}
		_, err := c.runTurn(ctx, c.sequence.Add(1), workers)
		if err == nil {
			continue
		}
		if IsErrorCode(err, ErrorCancelled) || IsErrorCode(err, ErrorDeadlineExceeded) {
			return nil
		}
		var appErr *Error
		if errors.As(err, &appErr) && appErr.Recoverable {
			continue
		}
		return err
	}
}

// RunTurn executes one isolated turn through worker boundaries.
func (c *Coordinator) RunTurn(ctx context.Context) (Result, error) {
	if c == nil {
		return Result{}, invalidApplication("coordinator is nil")
	}
	ctx = nonNilContext(ctx)
	workers := newWorkerSet(ctx, c.dependencies)
	workers.start()
	defer workers.stop()
	return c.runTurn(ctx, c.sequence.Add(1), workers)
}

func (c *Coordinator) runTurn(ctx context.Context, turnID uint64, workers *workerSet) (result Result, err error) {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	recorder := newTurnRecorder(turnID, c.dependencies.Observer)
	recorder.emit(StateIdle)
	defer func() {
		result.Trace = recorder.finish(result.Trace)
	}()
	if ctxErr := turnCtx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}

	recorder.emit(StateListening)
	capture, stageErr := workers.execute(turnCtx, stageCapture, stageRequest{turnID: turnID, ctx: turnCtx})
	if stageErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorCaptureFailed, stageErr)
	}
	if capture.buffer.Empty() {
		result.Noop = true
		result.Trace.Outcome = "noop"
		recorder.emit(StateIdle)
		return result, nil
	}
	if validateErr := capture.buffer.Validate(); validateErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorCaptureFailed, validateErr)
	}
	result.Trace.SampleCount = len(capture.buffer.Samples)

	recorder.emit(StateTranscribing)
	transcription, stageErr := workers.execute(turnCtx, stageTranscribe, stageRequest{
		turnID: turnID,
		ctx:    turnCtx,
		buffer: capture.buffer.Clone(),
	})
	if stageErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorTranscriptionFailed, stageErr)
	}
	transcript := strings.TrimSpace(transcription.text)
	if transcript == "" {
		result.Noop = true
		result.Trace.Outcome = "noop"
		recorder.emit(StateIdle)
		return result, nil
	}
	result.Trace.TranscriptChars = len(transcript)
	c.dependencies.View.ShowUser(transcript)

	recorder.emit(StateThinking)
	if c.supportsStreaming() {
		return c.completeStreamingResponse(turnCtx, turnID, transcript, workers, recorder, result)
	}
	return c.completeBufferedResponse(turnCtx, turnID, transcript, workers, recorder, result)
}

func (c *Coordinator) supportsStreaming() bool {
	if c == nil {
		return false
	}
	_, responderOK := c.dependencies.Responder.(StreamingResponder)
	_, playerOK := c.dependencies.Player.(StreamingPlayer)
	return responderOK && playerOK
}

func (c *Coordinator) completeBufferedResponse(
	ctx context.Context,
	turnID uint64,
	transcript string,
	workers *workerSet,
	recorder *turnRecorder,
	result Result,
) (Result, error) {
	response, stageErr := workers.execute(ctx, stageRespond, stageRequest{
		turnID: turnID,
		ctx:    ctx,
		text:   transcript,
	})
	if stageErr != nil {
		return c.fail(ctx, recorder, result, ErrorAgentFailed, stageErr)
	}
	result.Reply = strings.TrimSpace(response.response.Reply)
	result.Trace.ReplyChars = len(result.Reply)
	c.dependencies.View.ShowAssistant(result.Reply)
	return c.synthesizeBufferedReply(ctx, turnID, workers, recorder, result)
}

func (c *Coordinator) synthesizeBufferedReply(
	ctx context.Context,
	turnID uint64,
	workers *workerSet,
	recorder *turnRecorder,
	result Result,
) (Result, error) {
	recorder.emit(StateSynthesizing)
	synthesis, stageErr := workers.execute(ctx, stageSynthesize, stageRequest{
		turnID: turnID,
		ctx:    ctx,
		text:   result.Reply,
	})
	if stageErr != nil {
		return c.fail(ctx, recorder, result, ErrorSynthesisFailed, stageErr)
	}
	if synthesis.buffer.Empty() {
		result.Trace.Outcome = "success"
		recorder.emit(StateIdle)
		return result, nil
	}
	if validateErr := synthesis.buffer.Validate(); validateErr != nil {
		return c.fail(ctx, recorder, result, ErrorSynthesisFailed, validateErr)
	}

	recorder.emit(StateSpeaking)
	_, stageErr = workers.execute(ctx, stagePlayback, stageRequest{
		turnID: turnID,
		ctx:    ctx,
		buffer: synthesis.buffer.Clone(),
	})
	if stageErr != nil {
		return c.fail(ctx, recorder, result, ErrorPlaybackFailed, stageErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}
	result.Trace.Outcome = "success"
	recorder.emit(StateIdle)
	return result, nil
}

type streamingTurnState struct {
	deltaSeen       bool
	synthState      bool
	speakingState   bool
	wroteAudio      bool
	firstDelta      bool
	firstAudio      bool
	sentenceBuilder *sentenceBuffer
}

func (c *Coordinator) completeStreamingResponse(
	ctx context.Context,
	turnID uint64,
	transcript string,
	workers *workerSet,
	recorder *turnRecorder,
	result Result,
) (Result, error) {
	state := &streamingTurnState{sentenceBuilder: newSentenceBuffer()}
	if err := workers.submit(ctx, stageRespond, stageRequest{
		turnID:    turnID,
		ctx:       ctx,
		text:      transcript,
		streaming: true,
	}); err != nil {
		return c.fail(ctx, recorder, result, ErrorAgentFailed, err)
	}

	var response stageResult
	for {
		select {
		case <-ctx.Done():
			c.abortStreamingPlayback(ctx, turnID, workers, state)
			recorder.emit(StateStopping)
			return result, contextApplicationError(ctx.Err())
		case <-workers.ctx.Done():
			c.abortStreamingPlayback(ctx, turnID, workers, state)
			return c.fail(ctx, recorder, result, ErrorAgentFailed, workers.ctx.Err())
		case delta := <-workers.deltas:
			if delta.turnID != turnID || delta.text == "" {
				continue
			}
			state.deltaSeen = true
			if !state.firstDelta {
				state.firstDelta = true
				result.Trace.FirstResponseDelta = recorder.elapsed()
			}
			if err := c.playStreamingPhrases(ctx, turnID, workers, recorder, state, state.sentenceBuilder.Push(delta.text)); err != nil {
				c.abortStreamingPlayback(ctx, turnID, workers, state)
				return c.streamingStageFailure(ctx, recorder, result, err)
			}
		case candidate := <-workers.responses:
			if candidate.turnID != turnID || candidate.stage != stageRespond {
				continue
			}
			response = candidate
			goto responseComplete
		}
	}

responseComplete:
	if response.err != nil {
		c.abortStreamingPlayback(ctx, turnID, workers, state)
		return c.fail(ctx, recorder, result, ErrorAgentFailed, response.err)
	}

	// Every delta send completes synchronously before the responder publishes its
	// final result. Once that result is observed, all remaining deltas are already
	// buffered and can be drained deterministically.
	for {
		select {
		case delta := <-workers.deltas:
			if delta.turnID != turnID || delta.text == "" {
				continue
			}
			state.deltaSeen = true
			if !state.firstDelta {
				state.firstDelta = true
				result.Trace.FirstResponseDelta = recorder.elapsed()
			}
			if err := c.playStreamingPhrases(ctx, turnID, workers, recorder, state, state.sentenceBuilder.Push(delta.text)); err != nil {
				c.abortStreamingPlayback(ctx, turnID, workers, state)
				return c.streamingStageFailure(ctx, recorder, result, err)
			}
		default:
			goto deltasDrained
		}
	}

deltasDrained:
	result.Reply = strings.TrimSpace(response.response.Reply)
	result.Trace.ReplyChars = len(result.Reply)
	c.dependencies.View.ShowAssistant(result.Reply)

	if !state.deltaSeen {
		return c.synthesizeBufferedReply(ctx, turnID, workers, recorder, result)
	}
	if err := c.playStreamingPhrases(ctx, turnID, workers, recorder, state, state.sentenceBuilder.Flush()); err != nil {
		c.abortStreamingPlayback(ctx, turnID, workers, state)
		return c.streamingStageFailure(ctx, recorder, result, err)
	}
	if state.wroteAudio {
		if _, stageErr := workers.execute(ctx, stagePlaybackFinish, stageRequest{turnID: turnID, ctx: ctx}); stageErr != nil {
			return c.fail(ctx, recorder, result, ErrorPlaybackFailed, stageErr)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}
	result.Trace.Outcome = "success"
	recorder.emit(StateIdle)
	return result, nil
}

func (c *Coordinator) playStreamingPhrases(
	ctx context.Context,
	turnID uint64,
	workers *workerSet,
	recorder *turnRecorder,
	state *streamingTurnState,
	phrases []string,
) error {
	for _, phrase := range phrases {
		phrase = strings.TrimSpace(phrase)
		if phrase == "" {
			continue
		}
		if !state.synthState {
			recorder.emit(StateSynthesizing)
			state.synthState = true
		}
		synthesis, err := workers.execute(ctx, stageSynthesize, stageRequest{
			turnID: turnID,
			ctx:    ctx,
			text:   phrase,
		})
		if err != nil {
			return &streamingStageError{code: ErrorSynthesisFailed, err: err}
		}
		if synthesis.buffer.Empty() {
			continue
		}
		if err := synthesis.buffer.Validate(); err != nil {
			return &streamingStageError{code: ErrorSynthesisFailed, err: err}
		}
		if !state.speakingState {
			recorder.emit(StateSpeaking)
			state.speakingState = true
		}
		if _, err := workers.execute(ctx, stagePlaybackWrite, stageRequest{
			turnID: turnID,
			ctx:    ctx,
			buffer: synthesis.buffer.Clone(),
		}); err != nil {
			return &streamingStageError{code: ErrorPlaybackFailed, err: err}
		}
		state.wroteAudio = true
		if !state.firstAudio {
			state.firstAudio = true
		}
	}
	return nil
}

type streamingStageError struct {
	code ErrorCode
	err  error
}

func (e *streamingStageError) Error() string { return e.err.Error() }
func (e *streamingStageError) Unwrap() error { return e.err }

func (c *Coordinator) streamingStageFailure(ctx context.Context, recorder *turnRecorder, result Result, err error) (Result, error) {
	var stageErr *streamingStageError
	if errors.As(err, &stageErr) {
		return c.fail(ctx, recorder, result, stageErr.code, stageErr.err)
	}
	return c.fail(ctx, recorder, result, ErrorAgentFailed, err)
}

func (c *Coordinator) abortStreamingPlayback(ctx context.Context, turnID uint64, workers *workerSet, state *streamingTurnState) {
	if state == nil || !state.wroteAudio || workers == nil {
		return
	}
	stopCtx := nonNilContext(ctx)
	if stopCtx.Err() != nil {
		stopCtx = context.Background()
	}
	_, _ = workers.execute(stopCtx, stagePlaybackStop, stageRequest{turnID: turnID, ctx: stopCtx})
	state.wroteAudio = false
}

func (c *Coordinator) fail(ctx context.Context, recorder *turnRecorder, result Result, code ErrorCode, err error) (Result, error) {
	contextErr := ctx.Err()
	if contextErr == nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		contextErr = err
	}
	if contextErr != nil {
		recorder.emit(StateStopping)
		result.Trace.Outcome = "cancelled"
		result.Trace.ErrorCode = contextCode(contextErr)
		return result, contextApplicationError(contextErr)
	}
	result.Trace.Outcome = "error"
	result.Trace.ErrorCode = code
	recorder.emit(StateIdle)
	return result, &Error{Code: code, Recoverable: true, Err: err}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type stage uint8

const (
	stageCapture stage = iota + 1
	stageTranscribe
	stageRespond
	stageSynthesize
	stagePlayback
	stagePlaybackWrite
	stagePlaybackFinish
	stagePlaybackStop
)

type stageRequest struct {
	turnID    uint64
	ctx       context.Context
	buffer    audio.Buffer
	text      string
	target    stage
	streaming bool
}

type stageResult struct {
	turnID   uint64
	stage    stage
	buffer   audio.Buffer
	text     string
	response conversation.Result
	err      error
}

type responseDelta struct {
	turnID uint64
	text   string
}

type workerSet struct {
	ctx          context.Context
	cancel       context.CancelFunc
	dependencies Dependencies
	capture      chan stageRequest
	transcribe   chan stageRequest
	respond      chan stageRequest
	synthesize   chan stageRequest
	playback     chan stageRequest
	results      chan stageResult
	responses    chan stageResult
	deltas       chan responseDelta
	wg           sync.WaitGroup
}

func newWorkerSet(parent context.Context, dependencies Dependencies) *workerSet {
	ctx, cancel := context.WithCancel(nonNilContext(parent))
	return &workerSet{
		ctx:          ctx,
		cancel:       cancel,
		dependencies: dependencies,
		capture:      make(chan stageRequest),
		transcribe:   make(chan stageRequest),
		respond:      make(chan stageRequest),
		synthesize:   make(chan stageRequest),
		playback:     make(chan stageRequest),
		results:      make(chan stageResult, 8),
		responses:    make(chan stageResult, 8),
		deltas:       make(chan responseDelta, 128),
	}
}

func (w *workerSet) start() {
	w.wg.Add(5)
	go w.captureLoop()
	go w.transcribeLoop()
	go w.respondLoop()
	go w.synthesizeLoop()
	go w.playbackLoop()
}

func (w *workerSet) stop() {
	if w == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
}

func (w *workerSet) execute(ctx context.Context, target stage, request stageRequest) (stageResult, error) {
	if err := w.submit(ctx, target, request); err != nil {
		return stageResult{}, err
	}
	result, err := w.await(ctx, request.turnID, target)
	if err != nil {
		return stageResult{}, err
	}
	if result.err != nil {
		return result, result.err
	}
	return result, nil
}

func (w *workerSet) submit(ctx context.Context, target stage, request stageRequest) error {
	request.target = target
	var jobs chan stageRequest
	switch target {
	case stageCapture:
		jobs = w.capture
	case stageTranscribe:
		jobs = w.transcribe
	case stageRespond:
		jobs = w.respond
	case stageSynthesize:
		jobs = w.synthesize
	case stagePlayback, stagePlaybackWrite, stagePlaybackFinish, stagePlaybackStop:
		jobs = w.playback
	default:
		return errors.New("unknown worker stage")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	case jobs <- request:
		return nil
	}
}

func (w *workerSet) await(ctx context.Context, turnID uint64, target stage) (stageResult, error) {
	results := w.results
	if target == stageRespond {
		results = w.responses
	}
	for {
		select {
		case <-ctx.Done():
			return stageResult{}, ctx.Err()
		case <-w.ctx.Done():
			if ctx.Err() != nil {
				return stageResult{}, ctx.Err()
			}
			return stageResult{}, w.ctx.Err()
		case result := <-results:
			if result.turnID != turnID || result.stage != target {
				continue
			}
			return result, nil
		}
	}
}

func (w *workerSet) publish(result stageResult) {
	output := w.results
	if result.stage == stageRespond {
		output = w.responses
	}
	select {
	case <-w.ctx.Done():
	case output <- result:
	}
}

func (w *workerSet) publishDelta(ctx context.Context, delta responseDelta) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ctx.Done():
		return w.ctx.Err()
	case w.deltas <- delta:
		return nil
	}
}

func (w *workerSet) captureLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.capture:
			buffer, err := w.dependencies.Input.Next(request.ctx)
			w.publish(stageResult{turnID: request.turnID, stage: stageCapture, buffer: buffer, err: err})
		}
	}
}

func (w *workerSet) transcribeLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.transcribe:
			text, err := w.dependencies.Transcriber.Transcribe(request.ctx, request.buffer)
			w.publish(stageResult{turnID: request.turnID, stage: stageTranscribe, text: text, err: err})
		}
	}
}

func (w *workerSet) respondLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.respond:
			var response conversation.Result
			var err error
			if request.streaming {
				if streamingResponder, ok := w.dependencies.Responder.(StreamingResponder); ok {
					response, err = streamingResponder.RespondStream(request.ctx, request.text, func(delta string) error {
						return w.publishDelta(request.ctx, responseDelta{turnID: request.turnID, text: delta})
					})
				} else {
					response, err = w.dependencies.Responder.Respond(request.ctx, request.text)
				}
			} else {
				response, err = w.dependencies.Responder.Respond(request.ctx, request.text)
			}
			w.publish(stageResult{turnID: request.turnID, stage: stageRespond, response: response, err: err})
		}
	}
}

func (w *workerSet) synthesizeLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.synthesize:
			buffer, err := w.dependencies.Synthesizer.Synthesize(request.ctx, request.text)
			w.publish(stageResult{turnID: request.turnID, stage: stageSynthesize, buffer: buffer, err: err})
		}
	}
}

func (w *workerSet) playbackLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.ctx.Done():
			return
		case request := <-w.playback:
			var err error
			switch request.target {
			case stagePlayback:
				err = w.dependencies.Player.Play(request.ctx, request.buffer)
			case stagePlaybackWrite:
				streamingPlayer, ok := w.dependencies.Player.(StreamingPlayer)
				if !ok {
					err = fmt.Errorf("player does not support streaming writes")
				} else {
					err = streamingPlayer.Write(request.ctx, request.buffer)
				}
			case stagePlaybackFinish:
				streamingPlayer, ok := w.dependencies.Player.(StreamingPlayer)
				if !ok {
					err = fmt.Errorf("player does not support streaming finish")
				} else {
					err = streamingPlayer.Finish(request.ctx)
				}
			case stagePlaybackStop:
				streamingPlayer, ok := w.dependencies.Player.(StreamingPlayer)
				if !ok {
					err = fmt.Errorf("player does not support streaming stop")
				} else {
					err = streamingPlayer.Stop()
				}
			default:
				err = fmt.Errorf("unknown playback stage %d", request.target)
			}
			w.publish(stageResult{turnID: request.turnID, stage: request.target, err: err})
		}
	}
}
