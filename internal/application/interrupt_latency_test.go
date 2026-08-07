package application

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatorInterruptCopiesConfirmedLatencyIntoTrace(t *testing.T) {
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
	const latency = 275 * time.Millisecond
	source.emit(InterruptEvent{TurnID: 1, Reason: "wake_stop", Latency: latency})

	outcome := <-done
	if !IsErrorCode(outcome.err, ErrorInterrupted) {
		t.Fatalf("RunTurn() error = %v, want interrupted", outcome.err)
	}
	if outcome.result.Trace.InterruptLatency != latency {
		t.Fatalf("InterruptLatency = %s, want %s", outcome.result.Trace.InterruptLatency, latency)
	}
}
