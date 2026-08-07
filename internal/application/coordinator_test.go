package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestCoordinatorCompletesTurnThroughWorkers(t *testing.T) {
	calls := &callLog{}
	observer := &recordingObserver{}
	view := &recordingView{}
	coordinator := newTestCoordinator(t, calls, observer, view)

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	wantCalls := []string{"input", "stt", "agent", "synthesis", "playback"}
	if got := calls.snapshot(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("calls = %v, want %v", got, wantCalls)
	}
	wantStates := []State{
		StateIdle,
		StateListening,
		StateTranscribing,
		StateThinking,
		StateSynthesizing,
		StateSpeaking,
		StateIdle,
	}
	if got := observer.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("states = %v, want %v", got, wantStates)
	}
	if result.Trace.TurnID != 1 || result.Reply != "assistant reply" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !reflect.DeepEqual(view.user, []string{"hello"}) || !reflect.DeepEqual(view.assistant, []string{"assistant reply"}) {
		t.Fatalf("view = user %v assistant %v", view.user, view.assistant)
	}
}

func TestCoordinatorReusesWorkerSetAcrossTurns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := &callLog{}
	plays := 0
	coordinator, err := NewCoordinator(Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			calls.add("input")
			return audio.Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			calls.add("stt")
			return "hello", nil
		}),
		Responder: responderFunc(func(context.Context, string) (conversation.Result, error) {
			calls.add("agent")
			return conversation.Result{Reply: "assistant reply"}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			calls.add("synthesis")
			return audio.Buffer{Samples: []float32{0.2}, SampleRate: 22050, Channels: 1}, nil
		}),
		Player: playerFunc(func(context.Context, audio.Buffer) error {
			calls.add("playback")
			plays++
			if plays == 2 {
				cancel()
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	if err := coordinator.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{
		"input", "stt", "agent", "synthesis", "playback",
		"input", "stt", "agent", "synthesis", "playback",
	}
	if got := calls.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestCoordinatorAssignsMonotonicTurnIDs(t *testing.T) {
	coordinator := newTestCoordinator(t, &callLog{}, nil, nil)
	first, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("first turn error = %v", err)
	}
	second, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("second turn error = %v", err)
	}
	if first.Trace.TurnID == 0 || second.Trace.TurnID != first.Trace.TurnID+1 {
		t.Fatalf("turn IDs = %d, %d", first.Trace.TurnID, second.Trace.TurnID)
	}
}

func TestWorkerSetDiscardsStaleTurnResult(t *testing.T) {
	workers := &workerSet{results: make(chan stageResult, 2)}
	workers.results <- stageResult{turnID: 41, stage: stageTranscribe, text: "stale"}
	workers.results <- stageResult{turnID: 42, stage: stageTranscribe, text: "fresh"}

	result, err := workers.await(context.Background(), 42, stageTranscribe)
	if err != nil {
		t.Fatalf("await() error = %v", err)
	}
	if result.text != "fresh" {
		t.Fatalf("result text = %q, want fresh", result.text)
	}
}

func newTestCoordinator(t *testing.T, calls *callLog, observer Observer, view View) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			calls.add("input")
			return audio.Buffer{Samples: []float32{0.1, 0.2, 0.3}, SampleRate: 16000, Channels: 1}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			calls.add("stt")
			return "hello", nil
		}),
		Responder: responderFunc(func(context.Context, string) (conversation.Result, error) {
			calls.add("agent")
			return conversation.Result{Reply: "assistant reply"}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			calls.add("synthesis")
			return audio.Buffer{Samples: []float32{0.4}, SampleRate: 22050, Channels: 1}, nil
		}),
		Player: playerFunc(func(context.Context, audio.Buffer) error {
			calls.add("playback")
			return nil
		}),
		Observer: observer,
		View:     view,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator
}
