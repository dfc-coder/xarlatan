package audio

import (
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestContinuousRecorderPublishesOneBoundedPartialPerActiveUtterance(t *testing.T) {
	detector := &fakeDetector{steps: []detectorStep{
		{speaking: true}, {speaking: true}, {speaking: true}, {speaking: true},
		{speaking: true}, {speaking: true}, {speaking: true}, {speaking: true},
		{speaking: true}, {speaking: true}, {speaking: true}, {speaking: true},
		{speaking: true}, {speaking: true, finish: true},
	}}
	recorder, err := newContinuousRecorder(
		config.AudioConfig{SampleRate: 1000, Channels: 1},
		detector,
		ContinuousOptions{PreRollChunks: 2, QueueDepth: 2},
		&fakeContinuousFactory{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.waiting.Store(true)

	var recording []float32
	partialSent := false
	for i := 0; i < 14; i++ {
		chunk := constantChunk(80, 0.1)
		var finish bool
		recording, finish = recorder.processChunk(recording, chunk)
		partialSent = recorder.maybePublishPartial(recording, partialSent)
		if finish {
			break
		}
	}
	if !partialSent {
		t.Fatal("partial was not published")
	}
	if got := len(recorder.partials); got != 1 {
		t.Fatalf("partial queue length = %d, want 1", got)
	}
	preview := <-recorder.partials
	if preview.Buffer.Empty() || preview.CapturedAt.IsZero() {
		t.Fatalf("preview = %+v", preview)
	}
	if len(preview.Buffer.Samples) < 1000 {
		t.Fatalf("partial samples = %d, want at least one second", len(preview.Buffer.Samples))
	}
	if recorder.maybePublishPartial(recording, partialSent) != true {
		t.Fatal("partialSent unexpectedly reset")
	}
	if len(recorder.partials) != 0 {
		t.Fatal("second preview emitted for same utterance")
	}
}

func TestContinuousRecorderDoesNotPublishPartialWithoutActiveNext(t *testing.T) {
	recorder := &ContinuousRecorder{
		cfg:      config.AudioConfig{SampleRate: 1000, Channels: 1},
		partials: make(chan PartialAudio, 1),
	}
	if recorder.maybePublishPartial(makeSamples(1200, 0.2), false) {
		t.Fatal("partial marked sent while no Next is active")
	}
	if len(recorder.partials) != 0 {
		t.Fatal("partial published while no Next is active")
	}
}

func TestPartialAudioQueueDropsOldSnapshotAndResetFlushes(t *testing.T) {
	recorder := &ContinuousRecorder{
		cfg:      config.AudioConfig{SampleRate: 1000, Channels: 1},
		partials: make(chan PartialAudio, 1),
	}
	recorder.waiting.Store(true)
	first := PartialAudio{Buffer: Buffer{Samples: []float32{1}, SampleRate: 1000, Channels: 1}, CapturedAt: time.Now()}
	second := PartialAudio{Buffer: Buffer{Samples: []float32{2}, SampleRate: 1000, Channels: 1}, CapturedAt: time.Now()}
	recorder.publishPartial(first)
	recorder.publishPartial(second)
	if got := len(recorder.partials); got != 1 {
		t.Fatalf("partial queue length = %d, want 1", got)
	}
	if got := (<-recorder.partials).Buffer.Samples[0]; got != 2 {
		t.Fatalf("retained partial = %v, want newest 2", got)
	}
	recorder.publishPartial(second)
	recorder.resetPartials()
	if len(recorder.partials) != 0 {
		t.Fatal("resetPartials did not flush queue")
	}
}
