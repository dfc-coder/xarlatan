package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestCoordinatorWakeGateRejectsAmbientTranscriptBeforeResponder(t *testing.T) {
	calls := 0
	observer := &recordingObserver{}
	coordinator, err := NewCoordinator(Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			return audio.Buffer{Samples: []float32{0.2}, SampleRate: 16000, Channels: 1}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "esto es una conversación normal", nil
		}),
		Responder: responderFunc(func(context.Context, string) (conversation.Result, error) {
			calls++
			return conversation.Result{Reply: "should not happen"}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Player:   playerFunc(func(context.Context, audio.Buffer) error { return nil }),
		Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.SetWakeDetector(fakeWakeDetector{match: false})

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if !result.Noop || result.Trace.Outcome != "wake_ignored" {
		t.Fatalf("result = %+v", result)
	}
	if calls != 0 {
		t.Fatalf("responder calls = %d, want 0", calls)
	}
	if traceContains(result.Trace.States, StateWakeDetected) {
		t.Fatalf("states = %v; rejected transcript emitted wake", result.Trace.States)
	}
}

func TestCoordinatorWakeGateEmitsWakeDetectedAndUsesStrippedCommand(t *testing.T) {
	var got string
	observer := &recordingObserver{}
	coordinator, err := NewCoordinator(Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			return audio.Buffer{Samples: []float32{0.2}, SampleRate: 16000, Channels: 1}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "Xarlatan qué hora es", nil
		}),
		Responder: responderFunc(func(_ context.Context, text string) (conversation.Result, error) {
			got = text
			return conversation.Result{Reply: "respuesta"}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Player:   playerFunc(func(context.Context, audio.Buffer) error { return nil }),
		Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.SetWakeDetector(fakeWakeDetector{match: true, command: "qué hora es"})

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "qué hora es" {
		t.Fatalf("responder input = %q", got)
	}
	if !traceContains(result.Trace.States, StateWakeDetected) {
		t.Fatalf("states = %v, want wake_detected", result.Trace.States)
	}
	wantPrefix := []State{StateIdle, StateListening, StateTranscribing, StateWakeDetected, StateThinking}
	if len(result.Trace.States) < len(wantPrefix) || !reflect.DeepEqual(result.Trace.States[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("states prefix = %v, want %v", result.Trace.States, wantPrefix)
	}
}

type fakeWakeDetector struct {
	match   bool
	command string
}

func (f fakeWakeDetector) Detect(string) (string, bool) {
	return f.command, f.match
}

var _ WakeDetector = fakeWakeDetector{}
