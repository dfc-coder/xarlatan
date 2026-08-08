// Package application owns the testable voice-turn pipeline.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

// State identifies an observable application state without conversation data.
type State string

const (
	StateIdle         State = "idle"
	StateListening    State = "listening"
	StateTranscribing State = "transcribing"
	StateThinking     State = "thinking"
	StateSynthesizing State = "synthesizing"
	StateSpeaking     State = "speaking"
	StateStopping     State = "stopping"
)

// ErrorCode identifies the failed voice stage.
type ErrorCode string

const (
	ErrorCaptureFailed       ErrorCode = "capture_failed"
	ErrorTranscriptionFailed ErrorCode = "transcription_failed"
	ErrorAgentFailed         ErrorCode = "agent_failed"
	ErrorSynthesisFailed     ErrorCode = "synthesis_failed"
	ErrorPlaybackFailed      ErrorCode = "playback_failed"
	ErrorCancelled           ErrorCode = "cancelled"
	ErrorDeadlineExceeded    ErrorCode = "deadline_exceeded"
	ErrorInvalidApplication  ErrorCode = "invalid_application"
)

// Error preserves a stable code and whether the loop may start another turn.
type Error struct {
	Code        ErrorCode
	Recoverable bool
	Err         error
}

func (e *Error) Error() string {
	if e == nil {
		return "application error"
	}
	if e.Err == nil {
		return "application " + string(e.Code)
	}
	return fmt.Sprintf("application %s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsErrorCode reports whether err contains the requested stable code.
func IsErrorCode(err error, code ErrorCode) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Code == code
}

// VoiceInput supplies one bounded utterance.
type VoiceInput interface {
	Next(context.Context) (audio.Buffer, error)
}

// EndOfSpeechMetricProvider exposes the monotonic wall-clock boundary attached
// to the most recently consumed utterance. It carries no audio or transcript.
type EndOfSpeechMetricProvider interface {
	ConsumeEndOfSpeech() time.Time
}

// Transcriber converts one audio buffer to text.
type Transcriber interface {
	Transcribe(context.Context, audio.Buffer) (string, error)
}

// Responder executes one input-agnostic conversation turn.
type Responder interface {
	Respond(context.Context, string) (conversation.Result, error)
}

// Synthesizer converts reply text to an audio buffer.
type Synthesizer interface {
	Synthesize(context.Context, string) (audio.Buffer, error)
}

// Player renders one audio buffer.
type Player interface {
	Play(context.Context, audio.Buffer) error
}

// Observer receives metadata-only state transitions.
type Observer interface {
	OnEvent(Event)
}

// View presents user-facing conversation content separately from metrics.
type View interface {
	ShowUser(string)
	ShowAssistant(string)
}

// Event contains no audio or conversation content.
type Event struct {
	TurnID uint64
	State  State
}

// Trace contains only bounded metadata about one turn. v0.6 latency boundaries
// are monotonic durations since the local turn recorder started; derived values
// are differences between those boundaries and never contain conversation data.
type Trace struct {
	TurnID               uint64
	States               []State
	StageDurations       map[State]time.Duration
	TotalDuration        time.Duration
	FirstSTTPartial      time.Duration
	EndOfSpeech          time.Duration
	FinalTranscript      time.Duration
	FirstResponseDelta   time.Duration
	FirstPCM             time.Duration
	FirstAudio           time.Duration
	STTLatency           time.Duration
	AgentTTFT            time.Duration
	TTSFirstChunkLatency time.Duration
	PlaybackLatency      time.Duration
	EOSToFirstAudio      time.Duration
	InterruptLatency     time.Duration
	SampleCount          int
	TranscriptChars      int
	ReplyChars           int
	Outcome              string
	ErrorCode            ErrorCode
}

// Result is the observable outcome of one voice turn.
type Result struct {
	Reply string
	Noop  bool
	Trace Trace
}

// Dependencies are the only boundaries known by Application.
type Dependencies struct {
	Input       VoiceInput
	Transcriber Transcriber
	Responder   Responder
	Synthesizer Synthesizer
	Player      Player
	Observer    Observer
	View        View
}

// Application owns voice-stage ordering and recovery policy.
type Application struct {
	input       VoiceInput
	transcriber Transcriber
	responder   Responder
	synthesizer Synthesizer
	player      Player
	observer    Observer
	view        View
	sequence    atomic.Uint64
}

// New validates and constructs the voice application.
func New(dependencies Dependencies) (*Application, error) {
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
	observer := dependencies.Observer
	if observer == nil {
		observer = nopObserver{}
	}
	view := dependencies.View
	if view == nil {
		view = nopView{}
	}
	return &Application{
		input:       dependencies.Input,
		transcriber: dependencies.Transcriber,
		responder:   dependencies.Responder,
		synthesizer: dependencies.Synthesizer,
		player:      dependencies.Player,
		observer:    observer,
		view:        view,
	}, nil
}

// Run repeats independent voice turns until cancellation or a terminal error.
func (a *Application) Run(ctx context.Context) error {
	if a == nil {
		return invalidApplication("application is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		_, err := a.RunTurn(ctx)
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

// RunTurn executes capture -> STT -> conversation -> synthesis -> playback.
func (a *Application) RunTurn(ctx context.Context) (result Result, err error) {
	if a == nil || a.input == nil || a.transcriber == nil || a.responder == nil || a.synthesizer == nil || a.player == nil {
		return Result{}, invalidApplication("application is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	turnID := a.sequence.Add(1)
	recorder := newTurnRecorder(turnID, a.observer)
	recorder.emit(StateIdle)
	defer func() {
		result.Trace = recorder.finish(result.Trace)
	}()
	if ctxErr := ctx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}

	recorder.emit(StateListening)
	input, stageErr := a.input.Next(ctx)
	if stageErr != nil {
		return a.fail(ctx, recorder, result, ErrorCaptureFailed, stageErr)
	}
	if input.Empty() {
		result.Noop = true
		result.Trace.Outcome = "noop"
		recorder.emit(StateIdle)
		return result, nil
	}
	if validateErr := input.Validate(); validateErr != nil {
		return a.fail(ctx, recorder, result, ErrorCaptureFailed, validateErr)
	}
	recorder.captureEndOfSpeech(a.input)
	result.Trace.SampleCount = len(input.Samples)

	recorder.emit(StateTranscribing)
	transcript, stageErr := a.transcriber.Transcribe(ctx, input.Clone())
	if stageErr != nil {
		return a.fail(ctx, recorder, result, ErrorTranscriptionFailed, stageErr)
	}
	recorder.markFinalTranscript()
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		result.Noop = true
		result.Trace.Outcome = "noop"
		recorder.emit(StateIdle)
		return result, nil
	}
	result.Trace.TranscriptChars = len(transcript)
	a.view.ShowUser(transcript)

	recorder.emit(StateThinking)
	response, stageErr := a.responder.Respond(ctx, transcript)
	if stageErr != nil {
		return a.fail(ctx, recorder, result, ErrorAgentFailed, stageErr)
	}
	result.Reply = strings.TrimSpace(response.Reply)
	result.Trace.ReplyChars = len(result.Reply)
	a.view.ShowAssistant(result.Reply)

	recorder.emit(StateSynthesizing)
	output, stageErr := a.synthesizer.Synthesize(ctx, result.Reply)
	if stageErr != nil {
		return a.fail(ctx, recorder, result, ErrorSynthesisFailed, stageErr)
	}
	if output.Empty() {
		result.Trace.Outcome = "success"
		recorder.emit(StateIdle)
		return result, nil
	}
	if validateErr := output.Validate(); validateErr != nil {
		return a.fail(ctx, recorder, result, ErrorSynthesisFailed, validateErr)
	}
	recorder.markFirstPCM()

	recorder.emit(StateSpeaking)
	if stageErr := a.player.Play(ctx, output.Clone()); stageErr != nil {
		return a.fail(ctx, recorder, result, ErrorPlaybackFailed, stageErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		recorder.emit(StateStopping)
		return result, contextApplicationError(ctxErr)
	}
	result.Trace.Outcome = "success"
	recorder.emit(StateIdle)
	return result, nil
}

func (a *Application) fail(ctx context.Context, recorder *turnRecorder, result Result, code ErrorCode, err error) (Result, error) {
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

func invalidApplication(message string) error {
	return &Error{Code: ErrorInvalidApplication, Err: errors.New(message)}
}

func contextApplicationError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrorDeadlineExceeded, Err: context.DeadlineExceeded}
	}
	return &Error{Code: ErrorCancelled, Err: context.Canceled}
}

func contextCode(err error) ErrorCode {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorDeadlineExceeded
	}
	return ErrorCancelled
}

type nopObserver struct{}

func (nopObserver) OnEvent(Event) {}

type nopView struct{}

func (nopView) ShowUser(string)      {}
func (nopView) ShowAssistant(string) {}

type turnRecorder struct {
	turnID             uint64
	observer           Observer
	started            time.Time
	stageStarted       time.Time
	current            State
	states             []State
	durations          map[State]time.Duration
	endOfSpeech        time.Duration
	finalTranscript    time.Duration
	firstResponseDelta time.Duration
	firstPCM           time.Duration
	firstAudio         time.Duration
}

func newTurnRecorder(turnID uint64, observer Observer) *turnRecorder {
	now := time.Now()
	return &turnRecorder{
		turnID:       turnID,
		observer:     observer,
		started:      now,
		stageStarted: now,
		durations:    make(map[State]time.Duration),
	}
}

func (r *turnRecorder) emit(state State) {
	now := time.Now()
	if r.current != "" {
		r.durations[r.current] += positiveDuration(now.Sub(r.stageStarted))
	}
	if state == StateSpeaking {
		r.markFirstAudio()
	}
	r.current = state
	r.stageStarted = now
	r.states = append(r.states, state)
	r.observer.OnEvent(Event{TurnID: r.turnID, State: state})
}

func (r *turnRecorder) elapsed() time.Duration {
	if r == nil {
		return 0
	}
	return positiveDuration(time.Since(r.started))
}

func (r *turnRecorder) captureEndOfSpeech(input VoiceInput) {
	if r == nil || r.endOfSpeech != 0 {
		return
	}
	if provider, ok := input.(EndOfSpeechMetricProvider); ok {
		r.markEndOfSpeech(provider.ConsumeEndOfSpeech())
	}
}

func (r *turnRecorder) markEndOfSpeech(at time.Time) {
	if r == nil || r.endOfSpeech != 0 {
		return
	}
	if at.IsZero() {
		r.endOfSpeech = r.elapsed()
		return
	}
	duration := at.Sub(r.started)
	if duration <= 0 {
		duration = r.elapsed()
	}
	r.endOfSpeech = positiveDuration(duration)
}

func (r *turnRecorder) markFinalTranscript() {
	if r != nil && r.finalTranscript == 0 {
		r.finalTranscript = r.elapsed()
	}
}

func (r *turnRecorder) markFirstResponseDelta() {
	if r != nil && r.firstResponseDelta == 0 {
		r.firstResponseDelta = r.elapsed()
	}
}

func (r *turnRecorder) markFirstPCM() {
	if r != nil && r.firstPCM == 0 {
		r.firstPCM = r.elapsed()
	}
}

func (r *turnRecorder) markFirstAudio() {
	if r != nil && r.firstAudio == 0 {
		r.firstAudio = r.elapsed()
	}
}

func (r *turnRecorder) finish(trace Trace) Trace {
	now := time.Now()
	if r.current != "" {
		r.durations[r.current] += positiveDuration(now.Sub(r.stageStarted))
	}
	trace.TurnID = r.turnID
	trace.States = append([]State(nil), r.states...)
	trace.StageDurations = make(map[State]time.Duration, len(r.durations))
	for state, duration := range r.durations {
		trace.StageDurations[state] = duration
	}
	trace.TotalDuration = positiveDuration(now.Sub(r.started))
	if trace.FirstSTTPartial == 0 {
		if provider, ok := r.observer.(STTPartialMetricProvider); ok {
			trace.FirstSTTPartial = provider.ConsumeFirstSTTPartial(r.turnID)
		}
	}
	if trace.EndOfSpeech == 0 {
		trace.EndOfSpeech = r.endOfSpeech
	}
	if trace.EndOfSpeech == 0 {
		trace.EndOfSpeech = positiveDuration(r.durations[StateListening])
	}
	if trace.FinalTranscript == 0 {
		trace.FinalTranscript = r.finalTranscript
	}
	if trace.FinalTranscript == 0 && trace.EndOfSpeech > 0 && r.durations[StateTranscribing] > 0 {
		trace.FinalTranscript = trace.EndOfSpeech + positiveDuration(r.durations[StateTranscribing])
	}
	if trace.FirstResponseDelta == 0 {
		trace.FirstResponseDelta = r.firstResponseDelta
	}
	if trace.FirstPCM == 0 {
		trace.FirstPCM = r.firstPCM
	}
	if trace.FirstAudio == 0 {
		trace.FirstAudio = r.firstAudio
	}
	if trace.FirstPCM == 0 && trace.FirstAudio > 0 {
		trace.FirstPCM = trace.FirstAudio
	}
	trace.STTLatency = metricDelta(trace.EndOfSpeech, trace.FinalTranscript)
	trace.AgentTTFT = metricDelta(trace.FinalTranscript, trace.FirstResponseDelta)
	trace.TTSFirstChunkLatency = metricDelta(trace.FirstResponseDelta, trace.FirstPCM)
	trace.PlaybackLatency = metricDelta(trace.FirstPCM, trace.FirstAudio)
	trace.EOSToFirstAudio = metricDelta(trace.EndOfSpeech, trace.FirstAudio)
	return trace
}

func metricDelta(start, end time.Duration) time.Duration {
	if start <= 0 || end <= 0 || end < start {
		return 0
	}
	return positiveDuration(end - start)
}

func positiveDuration(duration time.Duration) time.Duration {
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}
