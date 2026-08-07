package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
)

func TestCoordinatorPublishesPartialBeforeFinalWithoutCallingResponder(t *testing.T) {
	input := newPartialTestInput()
	transcriber := &partialTestTranscriber{}
	observer := newRecordingTranscriptObserver()
	responder := &countingResponder{}
	coordinator, err := NewCoordinator(Dependencies{
		Input:       input,
		Transcriber: transcriber,
		Responder:   responder,
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Player: playerFunc(func(context.Context, audio.Buffer) error { return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.SetTranscriptObserver(observer)

	done := make(chan turnOutcome, 1)
	go func() {
		result, err := coordinator.RunTurn(context.Background())
		done <- turnOutcome{result: result, err: err}
	}()

	input.partials <- audio.PartialAudio{
		Buffer:     audio.Buffer{Samples: []float32{1, 1}, SampleRate: 16000, Channels: 1},
		CapturedAt: time.Now(),
	}
	partial := observer.waitFor(t, TranscriptPartial)
	if partial.Text != "Xarlatan cómo" || partial.TurnID != 1 {
		t.Fatalf("partial = %+v", partial)
	}
	if responder.count() != 0 {
		t.Fatalf("responder calls after partial = %d, want 0", responder.count())
	}

	close(input.releaseFinal)
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("RunTurn() error = %v", outcome.err)
	}
	final := observer.waitFor(t, TranscriptFinal)
	if final.Text != "Xarlatan cómo estás" || final.TurnID != partial.TurnID {
		t.Fatalf("final = %+v; partial = %+v", final, partial)
	}
	if responder.count() != 1 {
		t.Fatalf("responder calls = %d, want 1", responder.count())
	}
	if outcome.result.Trace.FirstSTTPartial <= 0 {
		t.Fatalf("FirstSTTPartial = %s, want >0", outcome.result.Trace.FirstSTTPartial)
	}
}

func TestCoordinatorWithoutPreviewPublishesOnlyFinal(t *testing.T) {
	input := &finalOnlyInput{}
	observer := newRecordingTranscriptObserver()
	coordinator, err := NewCoordinator(Dependencies{
		Input: input,
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "final corto", nil
		}),
		Responder: responderFunc(func(context.Context, string) (conversation.Result, error) {
			return conversation.Result{Reply: "ok"}, nil
		}),
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) { return audio.Buffer{}, nil }),
		Player:      playerFunc(func(context.Context, audio.Buffer) error { return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.SetTranscriptObserver(observer)
	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.FirstSTTPartial != 0 {
		t.Fatalf("FirstSTTPartial = %s, want 0", result.Trace.FirstSTTPartial)
	}
	events := observer.snapshot()
	if len(events) != 1 || events[0].Kind != TranscriptFinal || events[0].Text != "final corto" {
		t.Fatalf("events = %+v, want one final", events)
	}
}

type partialTestInput struct {
	partials     chan audio.PartialAudio
	releaseFinal chan struct{}
}

func newPartialTestInput() *partialTestInput {
	return &partialTestInput{
		partials:     make(chan audio.PartialAudio, 1),
		releaseFinal: make(chan struct{}),
	}
}

func (i *partialTestInput) Next(ctx context.Context) (audio.Buffer, error) {
	select {
	case <-ctx.Done():
		return audio.Buffer{}, ctx.Err()
	case <-i.releaseFinal:
		return audio.Buffer{Samples: []float32{2, 2}, SampleRate: 16000, Channels: 1}, nil
	}
}

func (i *partialTestInput) PartialAudio() <-chan audio.PartialAudio { return i.partials }

type finalOnlyInput struct{}

func (*finalOnlyInput) Next(context.Context) (audio.Buffer, error) {
	return audio.Buffer{Samples: []float32{2}, SampleRate: 16000, Channels: 1}, nil
}

type partialTestTranscriber struct{}

func (*partialTestTranscriber) Transcribe(_ context.Context, buffer audio.Buffer) (string, error) {
	if len(buffer.Samples) > 0 && buffer.Samples[0] == 1 {
		return "Xarlatan cómo", nil
	}
	return "Xarlatan cómo estás", nil
}

type countingResponder struct {
	mu    sync.Mutex
	calls int
}

func (r *countingResponder) Respond(context.Context, string) (conversation.Result, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return conversation.Result{Reply: "bien"}, nil
}

func (r *countingResponder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type recordingTranscriptObserver struct {
	mu     sync.Mutex
	events []TranscriptEvent
	notify chan TranscriptEvent
}

func newRecordingTranscriptObserver() *recordingTranscriptObserver {
	return &recordingTranscriptObserver{notify: make(chan TranscriptEvent, 8)}
}

func (o *recordingTranscriptObserver) OnTranscript(event TranscriptEvent) {
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
	o.notify <- event
}

func (o *recordingTranscriptObserver) waitFor(t *testing.T, kind TranscriptKind) TranscriptEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-o.notify:
			if event.Kind == kind {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s transcript", kind)
		}
	}
}

func (o *recordingTranscriptObserver) snapshot() []TranscriptEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]TranscriptEvent(nil), o.events...)
}

var _ PartialAudioSource = (*partialTestInput)(nil)
var _ VoiceInput = (*finalOnlyInput)(nil)
var _ TranscriptObserver = (*recordingTranscriptObserver)(nil)
