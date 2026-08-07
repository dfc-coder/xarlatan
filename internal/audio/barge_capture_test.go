package audio

import (
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestBargeCaptureRequiresWarmupAndRelativeEnergyPersistence(t *testing.T) {
	detector := &fakeDetector{steps: []detectorStep{
		{}, {}, {},
		{speaking: true}, {speaking: true},
		{speaking: true, finish: true},
	}}
	recorder, err := newContinuousRecorder(
		config.AudioConfig{SampleRate: 16000, Channels: 1},
		detector,
		ContinuousOptions{PreRollChunks: 2, QueueDepth: 2},
		&fakeContinuousFactory{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	state := newBargeCaptureState()

	chunks := [][]float32{
		constantChunk(1280, 0.01),
		constantChunk(1280, 0.01),
		constantChunk(1280, 0.01),
		constantChunk(1280, 0.08),
		constantChunk(1280, 0.09),
		constantChunk(1280, 0.05),
	}
	var candidate BargeCandidate
	var ready bool
	for _, chunk := range chunks {
		candidate, ready = recorder.processBargeChunk(state, chunk)
	}
	if !ready {
		t.Fatal("barge candidate was not produced")
	}
	if candidate.Buffer.Empty() || candidate.StartedAt.IsZero() {
		t.Fatalf("candidate = %+v", candidate)
	}
	if candidate.PeakRMS < 0.08 {
		t.Fatalf("PeakRMS = %f, want >= 0.08", candidate.PeakRMS)
	}
}

func TestBargeCaptureRejectsEchoLevelSpeech(t *testing.T) {
	detector := &fakeDetector{steps: []detectorStep{
		{speaking: true}, {speaking: true}, {speaking: true},
		{speaking: true}, {speaking: true}, {speaking: true, finish: true},
	}}
	recorder, err := newContinuousRecorder(
		config.AudioConfig{SampleRate: 16000, Channels: 1},
		detector,
		ContinuousOptions{PreRollChunks: 2, QueueDepth: 2},
		&fakeContinuousFactory{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	state := newBargeCaptureState()
	for i := 0; i < 6; i++ {
		if candidate, ready := recorder.processBargeChunk(state, constantChunk(1280, 0.012)); ready {
			t.Fatalf("echo-level candidate produced: %+v", candidate)
		}
	}
}

func TestBargeCandidateQueueIsBoundedAndFlushedOnResume(t *testing.T) {
	recorder := &ContinuousRecorder{bargeCandidates: make(chan BargeCandidate, 2)}
	for i := 0; i < 3; i++ {
		recorder.publishBargeCandidate(BargeCandidate{
			Buffer:    Buffer{Samples: []float32{float32(i + 1)}, SampleRate: 16000, Channels: 1},
			StartedAt: time.Now(),
		})
	}
	if got := len(recorder.bargeCandidates); got != 2 {
		t.Fatalf("candidate queue length = %d, want 2", got)
	}
	first := <-recorder.bargeCandidates
	if first.Buffer.Samples[0] != 2 {
		t.Fatalf("oldest retained candidate = %v, want 2", first.Buffer.Samples[0])
	}
	recorder.publishBargeCandidate(BargeCandidate{
		Buffer:    Buffer{Samples: []float32{4}, SampleRate: 16000, Channels: 1},
		StartedAt: time.Now(),
	})
	recorder.SetSuppressed(false)
	if got := len(recorder.bargeCandidates); got != 0 {
		t.Fatalf("candidate queue after resume = %d, want 0", got)
	}
}

func constantChunk(count int, value float32) []float32 {
	chunk := make([]float32, count)
	for i := range chunk {
		chunk[i] = value
	}
	return chunk
}
