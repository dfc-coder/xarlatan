package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestBargeInControllerConfirmsBeforePublishingInterrupt(t *testing.T) {
	candidates := make(chan audio.BargeCandidate, 2)
	source := fakeBargeCandidateSource{candidates: candidates}
	transcriber := &sequenceBargeTranscriber{texts: []string{
		"estoy aquí para ayudarte",
		"Xarlatan para",
	}}
	policy := fakeBargePolicy{confirm: func(text string) bool { return text == "Xarlatan para" }}
	controller, err := NewBargeInController(source, transcriber, policy)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := controller.Subscribe(ctx, 17)

	started := time.Now().Add(-150 * time.Millisecond)
	candidates <- audio.BargeCandidate{
		Buffer:    audio.Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1},
		StartedAt: started,
	}
	candidates <- audio.BargeCandidate{
		Buffer:    audio.Buffer{Samples: []float32{0.2}, SampleRate: 16000, Channels: 1},
		StartedAt: started,
	}

	select {
	case event := <-events:
		if event.TurnID != 17 || event.Reason != "wake_stop" {
			t.Fatalf("event = %+v", event)
		}
		if event.Latency <= 0 {
			t.Fatalf("Latency = %s, want >0", event.Latency)
		}
		if transcriber.calls != 2 {
			t.Fatalf("transcriber calls = %d, want 2", transcriber.calls)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for confirmed interrupt")
	}
}

func TestBargeInControllerCancellationStopsSubscription(t *testing.T) {
	candidates := make(chan audio.BargeCandidate)
	controller, err := NewBargeInController(
		fakeBargeCandidateSource{candidates: candidates},
		&sequenceBargeTranscriber{},
		fakeBargePolicy{confirm: func(string) bool { return true }},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := controller.Subscribe(ctx, 3)
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("event channel remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close after cancellation")
	}
}

func TestNewBargeInControllerValidatesDependencies(t *testing.T) {
	transcriber := &sequenceBargeTranscriber{}
	policy := fakeBargePolicy{confirm: func(string) bool { return false }}
	source := fakeBargeCandidateSource{candidates: make(chan audio.BargeCandidate)}
	if _, err := NewBargeInController(nil, transcriber, policy); err == nil {
		t.Fatal("nil source error = nil")
	}
	if _, err := NewBargeInController(source, nil, policy); err == nil {
		t.Fatal("nil transcriber error = nil")
	}
	if _, err := NewBargeInController(source, transcriber, nil); err == nil {
		t.Fatal("nil policy error = nil")
	}
}

type fakeBargeCandidateSource struct {
	candidates <-chan audio.BargeCandidate
}

func (f fakeBargeCandidateSource) BargeCandidates() <-chan audio.BargeCandidate {
	return f.candidates
}

type sequenceBargeTranscriber struct {
	texts []string
	calls int
	err   error
}

func (t *sequenceBargeTranscriber) Transcribe(context.Context, audio.Buffer) (string, error) {
	if t.err != nil {
		return "", t.err
	}
	if t.calls >= len(t.texts) {
		t.calls++
		return "", nil
	}
	text := t.texts[t.calls]
	t.calls++
	return text, nil
}

type fakeBargePolicy struct {
	confirm func(string) bool
}

func (p fakeBargePolicy) Confirm(text string) bool {
	if p.confirm == nil {
		return false
	}
	return p.confirm(text)
}

var _ BargeCandidateSource = fakeBargeCandidateSource{}
var _ Transcriber = (*sequenceBargeTranscriber)(nil)
var _ BargePolicy = fakeBargePolicy{}
var _ = errors.Is
