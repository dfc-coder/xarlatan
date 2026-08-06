package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestApplicationStopsDuringEveryStageOnCancellation(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Application, *callLog)
		wantCalls []string
	}{
		{
			name: "capture",
			configure: func(app *Application, calls *callLog) {
				app.input = voiceInputFunc(func(context.Context) (audio.Buffer, error) {
					calls.add("input")
					return audio.Buffer{}, context.Canceled
				})
			},
			wantCalls: []string{"input"},
		},
		{
			name: "transcription",
			configure: func(app *Application, calls *callLog) {
				app.transcriber = transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
					calls.add("stt")
					return "", context.Canceled
				})
			},
			wantCalls: []string{"input", "stt"},
		},
		{
			name: "agent",
			configure: func(app *Application, calls *callLog) {
				app.responder = responderFunc(func(context.Context, string) (conversation.Result, error) {
					calls.add("agent")
					return conversation.Result{}, context.Canceled
				})
			},
			wantCalls: []string{"input", "stt", "agent"},
		},
		{
			name: "synthesis",
			configure: func(app *Application, calls *callLog) {
				app.synthesizer = synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
					calls.add("synthesis")
					return audio.Buffer{}, context.Canceled
				})
			},
			wantCalls: []string{"input", "stt", "agent", "synthesis"},
		},
		{
			name: "playback",
			configure: func(app *Application, calls *callLog) {
				app.player = playerFunc(func(context.Context, audio.Buffer) error {
					calls.add("playback")
					return context.Canceled
				})
			},
			wantCalls: []string{"input", "stt", "agent", "synthesis", "playback"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := &callLog{}
			app := newTestApplication(t, calls)
			test.configure(app, calls)

			result, err := app.RunTurn(context.Background())
			if !IsErrorCode(err, ErrorCancelled) {
				t.Fatalf("RunTurn() error = %v, want cancelled", err)
			}
			if got := calls.snapshot(); !reflect.DeepEqual(got, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", got, test.wantCalls)
			}
			if len(result.Trace.States) == 0 || result.Trace.States[len(result.Trace.States)-1] != StateStopping {
				t.Fatalf("states = %v, want stopping last", result.Trace.States)
			}
		})
	}
}

func TestApplicationRunRecoversAfterSynthesisError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := &callLog{}
	app := newTestApplication(t, calls)
	syntheses := 0
	app.synthesizer = synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
		calls.add("synthesis")
		syntheses++
		if syntheses == 1 {
			return audio.Buffer{}, errors.New("temporary synthesis failure")
		}
		return audio.Buffer{Samples: []float32{0.4}, SampleRate: 22050, Channels: 1}, nil
	})
	app.player = playerFunc(func(context.Context, audio.Buffer) error {
		calls.add("playback")
		cancel()
		return nil
	})

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if syntheses != 2 {
		t.Fatalf("synthesis calls = %d, want 2", syntheses)
	}
	plays := 0
	for _, call := range calls.snapshot() {
		if call == "playback" {
			plays++
		}
	}
	if plays != 1 {
		t.Fatalf("playback calls = %d, want 1 after recovery", plays)
	}
}
