package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestApplicationSkipsAgentWhenSTTFails(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.transcriber = transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
		calls.add("stt")
		return "", errors.New("decode failed")
	})

	_, err := app.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorTranscriptionFailed) {
		t.Fatalf("RunTurn() error = %v, want transcription_failed", err)
	}
	if got := calls.snapshot(); !reflect.DeepEqual(got, []string{"input", "stt"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestApplicationSkipsAgentWhenTranscriptIsEmpty(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.transcriber = transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
		calls.add("stt")
		return "   ", nil
	})

	result, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if !result.Noop {
		t.Fatal("empty transcript should produce a no-op turn")
	}
	if got := calls.snapshot(); !reflect.DeepEqual(got, []string{"input", "stt"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestApplicationSkipsSynthesisWhenAgentFails(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.responder = responderFunc(func(context.Context, string) (conversation.Result, error) {
		calls.add("agent")
		return conversation.Result{}, errors.New("agent failed")
	})

	_, err := app.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorAgentFailed) {
		t.Fatalf("RunTurn() error = %v, want agent_failed", err)
	}
	want := []string{"input", "stt", "agent"}
	if got := calls.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestApplicationSkipsPlaybackWhenSynthesisFails(t *testing.T) {
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.synthesizer = synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
		calls.add("synthesis")
		return audio.Buffer{}, errors.New("tts failed")
	})

	_, err := app.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorSynthesisFailed) {
		t.Fatalf("RunTurn() error = %v, want synthesis_failed", err)
	}
	want := []string{"input", "stt", "agent", "synthesis"}
	if got := calls.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestApplicationCompletesVoiceTurn(t *testing.T) {
	calls := &callLog{}
	observer := &recordingObserver{}
	view := &recordingView{}
	app := newTestApplication(t, calls)
	app.observer = observer
	app.view = view

	result, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	wantCalls := []string{"input", "stt", "agent", "synthesis", "playback"}
	if got := calls.snapshot(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("calls = %v, want %v", got, wantCalls)
	}
	wantStates := []State{StateIdle, StateListening, StateTranscribing, StateThinking, StateSynthesizing, StateSpeaking, StateIdle}
	if got := observer.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("states = %v, want %v", got, wantStates)
	}
	if result.Reply != "assistant reply" || result.Trace.SampleCount != 3 || result.Trace.TranscriptChars != len("hello") || result.Trace.ReplyChars != len("assistant reply") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !reflect.DeepEqual(view.user, []string{"hello"}) || !reflect.DeepEqual(view.assistant, []string{"assistant reply"}) {
		t.Fatalf("view = user %v assistant %v", view.user, view.assistant)
	}
}

func TestApplicationTraceDoesNotContainConversationContent(t *testing.T) {
	app := newTestApplication(t, &callLog{})
	result, err := app.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	payload, err := json.Marshal(result.Trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	for _, secret := range []string{"hello", "assistant reply"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("trace leaks conversation content %q: %s", secret, payload)
		}
	}
}

func TestApplicationStopsDuringStageCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := &callLog{}
	app := newTestApplication(t, calls)
	app.transcriber = transcriberFunc(func(ctx context.Context, _ audio.Buffer) (string, error) {
		calls.add("stt")
		cancel()
		<-ctx.Done()
		return "", ctx.Err()
	})

	_, err := app.RunTurn(ctx)
	if !IsErrorCode(err, ErrorCancelled) {
		t.Fatalf("RunTurn() error = %v, want cancelled", err)
	}
	want := []string{"input", "stt"}
	if got := calls.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestApplicationRunRecoversAfterPlaybackError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := &callLog{}
	app := newTestApplication(t, calls)
	plays := 0
	app.player = playerFunc(func(context.Context, audio.Buffer) error {
		calls.add("playback")
		plays++
		if plays == 1 {
			return errors.New("device busy")
		}
		cancel()
		return nil
	})

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if plays != 2 {
		t.Fatalf("playback calls = %d, want 2", plays)
	}
}

func newTestApplication(t *testing.T, calls *callLog) *Application {
	t.Helper()
	app, err := New(Dependencies{
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
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return app
}

type voiceInputFunc func(context.Context) (audio.Buffer, error)

func (f voiceInputFunc) Next(ctx context.Context) (audio.Buffer, error) { return f(ctx) }

type transcriberFunc func(context.Context, audio.Buffer) (string, error)

func (f transcriberFunc) Transcribe(ctx context.Context, buffer audio.Buffer) (string, error) {
	return f(ctx, buffer)
}

type responderFunc func(context.Context, string) (conversation.Result, error)

func (f responderFunc) Respond(ctx context.Context, text string) (conversation.Result, error) {
	return f(ctx, text)
}

type synthesizerFunc func(context.Context, string) (audio.Buffer, error)

func (f synthesizerFunc) Synthesize(ctx context.Context, text string) (audio.Buffer, error) {
	return f(ctx, text)
}

type playerFunc func(context.Context, audio.Buffer) error

func (f playerFunc) Play(ctx context.Context, buffer audio.Buffer) error { return f(ctx, buffer) }

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	l.mu.Lock()
	l.calls = append(l.calls, call)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (o *recordingObserver) OnEvent(event Event) {
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
}

func (o *recordingObserver) states() []State {
	o.mu.Lock()
	defer o.mu.Unlock()
	states := make([]State, len(o.events))
	for i, event := range o.events {
		states[i] = event.State
	}
	return states
}

type recordingView struct {
	user      []string
	assistant []string
}

func (v *recordingView) ShowUser(text string)      { v.user = append(v.user, text) }
func (v *recordingView) ShowAssistant(text string) { v.assistant = append(v.assistant, text) }
