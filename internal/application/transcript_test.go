package application

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestTranscriptBridgePublishesPartialBeforeAuthoritativeFinal(t *testing.T) {
	source := newBridgePartialSource()
	transcriber := &bridgeTestTranscriber{}
	observer := newRecordingTranscriptObserver()
	bridge, err := NewTranscriptBridge(source, transcriber, observer)
	if err != nil {
		t.Fatal(err)
	}

	bridge.OnEvent(Event{TurnID: 1, State: StateListening})
	source.partials <- audio.PartialAudio{
		Buffer:     audio.Buffer{Samples: []float32{1}, SampleRate: 16000, Channels: 1},
		CapturedAt: time.Now(),
	}
	partial := observer.waitFor(t, TranscriptPartial)
	if partial.TurnID != 1 || partial.Text != "Xarlatan cómo" {
		t.Fatalf("partial = %+v", partial)
	}

	finalText, err := bridge.Transcribe(context.Background(), audio.Buffer{
		Samples: []float32{2}, SampleRate: 16000, Channels: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalText != "Xarlatan cómo estás" {
		t.Fatalf("final text = %q", finalText)
	}
	final := observer.waitFor(t, TranscriptFinal)
	if final.TurnID != 1 || final.Text != finalText {
		t.Fatalf("final = %+v", final)
	}
	events := observer.snapshot()
	if len(events) != 2 || events[0].Kind != TranscriptPartial || events[1].Kind != TranscriptFinal {
		t.Fatalf("events = %+v, want partial then final", events)
	}
	if latency := bridge.ConsumeFirstSTTPartial(1); latency <= 0 {
		t.Fatalf("partial latency = %s, want >0", latency)
	}
	if latency := bridge.ConsumeFirstSTTPartial(1); latency != 0 {
		t.Fatalf("second partial latency = %s, want 0", latency)
	}
}

func TestTranscriptBridgeFinalRemainsAuthoritativeWithoutPreview(t *testing.T) {
	source := newBridgePartialSource()
	observer := newRecordingTranscriptObserver()
	bridge, err := NewTranscriptBridge(source, &bridgeTestTranscriber{}, observer)
	if err != nil {
		t.Fatal(err)
	}
	bridge.OnEvent(Event{TurnID: 3, State: StateListening})

	finalText, err := bridge.Transcribe(context.Background(), audio.Buffer{
		Samples: []float32{2}, SampleRate: 16000, Channels: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalText != "Xarlatan cómo estás" {
		t.Fatalf("final text = %q", finalText)
	}
	events := observer.snapshot()
	if len(events) != 1 || events[0].Kind != TranscriptFinal || events[0].TurnID != 3 {
		t.Fatalf("events = %+v, want one final", events)
	}
	if latency := bridge.ConsumeFirstSTTPartial(3); latency != 0 {
		t.Fatalf("partial latency = %s, want 0", latency)
	}

	// A preview arriving after finalization is stale and must never appear.
	source.partials <- audio.PartialAudio{
		Buffer:     audio.Buffer{Samples: []float32{1}, SampleRate: 16000, Channels: 1},
		CapturedAt: time.Now(),
	}
	time.Sleep(20 * time.Millisecond)
	if got := observer.snapshot(); len(got) != 1 {
		t.Fatalf("late preview produced events = %+v", got)
	}
}

func TestTranscriptBridgeStoppingCancelsPreview(t *testing.T) {
	source := newBridgePartialSource()
	observer := newRecordingTranscriptObserver()
	bridge, err := NewTranscriptBridge(source, &bridgeTestTranscriber{}, observer)
	if err != nil {
		t.Fatal(err)
	}
	bridge.OnEvent(Event{TurnID: 7, State: StateListening})
	bridge.OnEvent(Event{TurnID: 7, State: StateStopping})
	source.partials <- audio.PartialAudio{
		Buffer:     audio.Buffer{Samples: []float32{1}, SampleRate: 16000, Channels: 1},
		CapturedAt: time.Now(),
	}
	time.Sleep(20 * time.Millisecond)
	if events := observer.snapshot(); len(events) != 0 {
		t.Fatalf("cancelled preview events = %+v", events)
	}
}

func TestNewTranscriptBridgeValidatesDependencies(t *testing.T) {
	if _, err := NewTranscriptBridge(nil, &bridgeTestTranscriber{}, nil); err == nil {
		t.Fatal("NewTranscriptBridge(nil source) error = nil")
	}
	if _, err := NewTranscriptBridge(newBridgePartialSource(), nil, nil); err == nil {
		t.Fatal("NewTranscriptBridge(nil transcriber) error = nil")
	}
	var bridge *TranscriptBridge
	if _, err := bridge.Transcribe(context.Background(), audio.Buffer{}); err == nil {
		t.Fatal("nil bridge Transcribe error = nil")
	}
}

type bridgePartialSource struct {
	partials chan audio.PartialAudio
}

func newBridgePartialSource() *bridgePartialSource {
	return &bridgePartialSource{partials: make(chan audio.PartialAudio, 4)}
}

func (s *bridgePartialSource) PartialAudio() <-chan audio.PartialAudio { return s.partials }

type bridgeTestTranscriber struct{}

func (*bridgeTestTranscriber) Transcribe(_ context.Context, buffer audio.Buffer) (string, error) {
	if len(buffer.Samples) > 0 && buffer.Samples[0] == 1 {
		return " Xarlatan cómo ", nil
	}
	return " Xarlatan cómo estás ", nil
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

var _ PartialAudioSource = (*bridgePartialSource)(nil)
var _ Transcriber = (*bridgeTestTranscriber)(nil)
var _ TranscriptObserver = (*recordingTranscriptObserver)(nil)
