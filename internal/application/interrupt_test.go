package application

import (
	"context"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestCoordinatorInterruptDuringStreamingPlaybackStopsOnce(t *testing.T) {
	firstWrite := make(chan struct{})
	player := &failingStreamingPlayer{firstWrite: firstWrite}
	responder := &blockingStreamingResponder{firstWrite: firstWrite}
	coordinator := newStreamingFailureCoordinator(t, responder, streamingTestSynthesizer(), player)
	source := newFakeInterruptSource()
	coordinator.SetInterruptSource(source)

	done := make(chan turnOutcome, 1)
	go func() {
		result, err := coordinator.RunTurn(context.Background())
		done <- turnOutcome{result: result, err: err}
	}()
	<-firstWrite
	source.emit(InterruptEvent{TurnID: 1, Reason: "speech_started"})
	source.emit(InterruptEvent{TurnID: 1, Reason: "duplicate"})

	outcome := <-done
	if !IsErrorCode(outcome.err, ErrorInterrupted) {
		t.Fatalf("RunTurn() error = %v, want interrupted", outcome.err)
	}
	if outcome.result.Trace.Outcome != "interrupted" {
		t.Fatalf("Trace.Outcome = %q, want interrupted", outcome.result.Trace.Outcome)
	}
	if !traceContains(outcome.result.Trace.States, StateInterrupted) {
		t.Fatalf("states = %v, want interrupted", outcome.result.Trace.States)
	}
	if player.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want exactly 1", player.stopCalls)
	}
}

func TestCoordinatorInterruptWhileThinkingDoesNotStopPlayback(t *testing.T) {
	started := make(chan struct{})
	responder := &interruptBlockingResponder{started: started}
	player := &failingStreamingPlayer{}
	coordinator := newStreamingFailureCoordinator(t, responder, streamingTestSynthesizer(), player)
	source := newFakeInterruptSource()
	coordinator.SetInterruptSource(source)

	done := make(chan error, 1)
	go func() {
		_, err := coordinator.RunTurn(context.Background())
		done <- err
	}()
	<-started
	source.emit(InterruptEvent{TurnID: 1, Reason: "speech_started"})

	if err := <-done; !IsErrorCode(err, ErrorInterrupted) {
		t.Fatalf("RunTurn() error = %v, want interrupted", err)
	}
	if player.stopCalls != 0 {
		t.Fatalf("Stop calls = %d, want 0 before audio", player.stopCalls)
	}
}

func TestCoordinatorIgnoresStaleInterruptTurnID(t *testing.T) {
	coordinator := newStreamingFailureCoordinator(
		t,
		&fixedStreamingResponder{deltas: []string{"Actual."}, finalReply: "Actual."},
		streamingTestSynthesizer(),
		&failingStreamingPlayer{},
	)
	source := newFakeInterruptSource()
	source.emit(InterruptEvent{TurnID: 999, Reason: "stale"})
	coordinator.SetInterruptSource(source)

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Trace.Outcome != "success" {
		t.Fatalf("Trace.Outcome = %q, want success", result.Trace.Outcome)
	}
	if traceContains(result.Trace.States, StateInterrupted) {
		t.Fatalf("states = %v; stale event interrupted current turn", result.Trace.States)
	}
}

func TestCoordinatorWithoutInterruptSourcePreservesBehavior(t *testing.T) {
	coordinator := newStreamingFailureCoordinator(
		t,
		&fixedStreamingResponder{deltas: []string{"Normal."}, finalReply: "Normal."},
		streamingTestSynthesizer(),
		&failingStreamingPlayer{},
	)
	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Reply != "Normal." || result.Trace.Outcome != "success" {
		t.Fatalf("result = %+v", result)
	}
}

type turnOutcome struct {
	result Result
	err    error
}

type fakeInterruptSource struct {
	events chan InterruptEvent
}

func newFakeInterruptSource() *fakeInterruptSource {
	return &fakeInterruptSource{events: make(chan InterruptEvent, 8)}
}

func (s *fakeInterruptSource) Subscribe(context.Context, uint64) <-chan InterruptEvent {
	return s.events
}

func (s *fakeInterruptSource) emit(event InterruptEvent) {
	s.events <- event
}

type interruptBlockingResponder struct {
	started chan struct{}
}

func (r *interruptBlockingResponder) Respond(ctx context.Context, _ string) (conversation.Result, error) {
	close(r.started)
	<-ctx.Done()
	return conversation.Result{}, ctx.Err()
}

func (r *interruptBlockingResponder) RespondStream(ctx context.Context, _ string, _ llm.ContentDelta) (conversation.Result, error) {
	close(r.started)
	<-ctx.Done()
	return conversation.Result{}, ctx.Err()
}

func traceContains(states []State, wanted State) bool {
	for _, state := range states {
		if state == wanted {
			return true
		}
	}
	return false
}

var _ InterruptSource = (*fakeInterruptSource)(nil)
var _ StreamingResponder = (*interruptBlockingResponder)(nil)
var _ Player = (*failingStreamingPlayer)(nil)
var _ Synthesizer = synthesizerFunc(nil)
var _ = audio.Buffer{}
