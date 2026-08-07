package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

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
	response, stageErr := workers.execute(turnCtx, stageRespond, stageRequest{
		turnID: turnID,
		ctx:    turnCtx,
		text:   transcript,
	})
	if stageErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorAgentFailed, stageErr)
	}
	result.Reply = strings.TrimSpace(response.response.Reply)
	result.Trace.ReplyChars = len(result.Reply)
	c.dependencies.View.ShowAssistant(result.Reply)

	recorder.emit(StateSynthesizing)
	synthesis, stageErr := workers.execute(turnCtx, stageSynthesize, stageRequest{
		turnID: turnID,
		ctx:    turnCtx,
		text:   result.Reply,
	})
	if stageErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorSynthesisFailed, stageErr)
	}
	if synthesis.buffer.Empty() {
		result.Trace.Outcome = "success"
		recorder.emit(StateIdle)
		return result, nil
	}
	if validateErr := synthesis.buffer.Validate(); validateErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorSynthesisFailed, validateErr)
	}

	recorder.emit(StateSpeaking)
	_, stageErr = workers.execute(turnCtx, stagePlayback, stageRequest{
		turnID: turnID,
		ctx:    turnCtx,
		buffer: synthesis.buffer.Clone(),
	})
	if stageErr != nil {
		return c.fail(turnCtx, recorder, result, ErrorPlaybackFailed, stageErr)
	}
	if ctxErr := turnCtx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}
	result.Trace.Outcome = "success"
	recorder.emit(StateIdle)
	return result, nil
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
)

type stageRequest struct {
	turnID uint64
	ctx    context.Context
	buffer audio.Buffer
	text   string
}

type stageResult struct {
	turnID   uint64
	stage    stage
	buffer   audio.Buffer
	text     string
	response conversation.Result
	err      error
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
	case stagePlayback:
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
	for {
		select {
		case <-ctx.Done():
			return stageResult{}, ctx.Err()
		case <-w.ctx.Done():
			if ctx.Err() != nil {
				return stageResult{}, ctx.Err()
			}
			return stageResult{}, w.ctx.Err()
		case result := <-w.results:
			if result.turnID != turnID || result.stage != target {
				continue
			}
			return result, nil
		}
	}
}

func (w *workerSet) publish(result stageResult) {
	select {
	case <-w.ctx.Done():
	case w.results <- result:
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
			response, err := w.dependencies.Responder.Respond(request.ctx, request.text)
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
			err := w.dependencies.Player.Play(request.ctx, request.buffer)
			w.publish(stageResult{turnID: request.turnID, stage: stagePlayback, err: err})
		}
	}
}
