package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestNewRejectsMissingDependencies(t *testing.T) {
	valid := Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "", nil
		}),
		Responder: responderFunc(func(context.Context, string) (conversation.Result, error) {
			return conversation.Result{}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Player: playerFunc(func(context.Context, audio.Buffer) error { return nil }),
	}

	tests := []struct {
		name   string
		mutate func(*Dependencies)
	}{
		{name: "input", mutate: func(d *Dependencies) { d.Input = nil }},
		{name: "transcriber", mutate: func(d *Dependencies) { d.Transcriber = nil }},
		{name: "responder", mutate: func(d *Dependencies) { d.Responder = nil }},
		{name: "synthesizer", mutate: func(d *Dependencies) { d.Synthesizer = nil }},
		{name: "player", mutate: func(d *Dependencies) { d.Player = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := valid
			test.mutate(&dependencies)
			if _, err := New(dependencies); !IsErrorCode(err, ErrorInvalidApplication) {
				t.Fatalf("New() error = %v, want invalid_application", err)
			}
		})
	}
}

func TestApplicationHandlesCaptureFailureAndNoAudio(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.input = voiceInputFunc(func(context.Context) (audio.Buffer, error) {
		calls.add("input")
		return audio.Buffer{}, errors.New("device unavailable")
	})
	if _, err := app.RunTurn(context.Background()); !IsErrorCode(err, ErrorCaptureFailed) {
		t.Fatalf("capture error = %v", err)
	}

	calls = &callLog{}
	app = newTestApplication(t, calls)
	app.input = voiceInputFunc(func(context.Context) (audio.Buffer, error) {
		calls.add("input")
		return audio.Buffer{}, nil
	})
	result, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("empty audio error = %v", err)
	}
	if !result.Noop || len(calls.snapshot()) != 1 {
		t.Fatalf("empty audio result = %+v calls=%v", result, calls.snapshot())
	}
}

func TestApplicationRejectsInvalidAudioFormats(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.input = voiceInputFunc(func(context.Context) (audio.Buffer, error) {
		calls.add("input")
		return audio.Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 2}, nil
	})
	if _, err := app.RunTurn(context.Background()); !IsErrorCode(err, ErrorCaptureFailed) {
		t.Fatalf("input format error = %v", err)
	}

	calls = &callLog{}
	app = newTestApplication(t, calls)
	app.synthesizer = synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
		calls.add("synthesis")
		return audio.Buffer{Samples: []float32{0.1}, SampleRate: 22050, Channels: 2}, nil
	})
	if _, err := app.RunTurn(context.Background()); !IsErrorCode(err, ErrorSynthesisFailed) {
		t.Fatalf("output format error = %v", err)
	}
}

func TestApplicationSkipsPlaybackForEmptySynthesis(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.synthesizer = synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
		calls.add("synthesis")
		return audio.Buffer{}, nil
	})
	result, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Trace.Outcome != "success" {
		t.Fatalf("outcome = %q", result.Trace.Outcome)
	}
	for _, call := range calls.snapshot() {
		if call == "playback" {
			t.Fatal("empty synthesis invoked playback")
		}
	}
}

func TestApplicationClassifiesDeadlineAndPreCancelledContext(t *testing.T) {
	app := newTestApplication(t, &callLog{})
	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), timeInPast())
	defer cancelDeadline()
	if _, err := app.RunTurn(deadlineCtx); !IsErrorCode(err, ErrorDeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(cancelledCtx); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func TestApplicationAssignsMonotonicTurnIDs(t *testing.T) {
	app := newTestApplication(t, &callLog{})
	first, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("first turn error = %v", err)
	}
	second, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("second turn error = %v", err)
	}
	if first.Trace.TurnID == 0 || second.Trace.TurnID != first.Trace.TurnID+1 {
		t.Fatalf("turn IDs = %d, %d", first.Trace.TurnID, second.Trace.TurnID)
	}
}

func TestApplicationErrorPreservesCauseAndNilApplicationFails(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{Code: ErrorPlaybackFailed, Recoverable: true, Err: cause}
	if !errors.Is(err, cause) || err.Error() == "" {
		t.Fatalf("error does not preserve cause: %v", err)
	}

	var app *Application
	if _, err := app.RunTurn(context.Background()); !IsErrorCode(err, ErrorInvalidApplication) {
		t.Fatalf("nil RunTurn() error = %v", err)
	}
	if err := app.Run(context.Background()); !IsErrorCode(err, ErrorInvalidApplication) {
		t.Fatalf("nil Run() error = %v", err)
	}
}

func timeInPast() time.Time {
	return time.Now().Add(-time.Second)
}
