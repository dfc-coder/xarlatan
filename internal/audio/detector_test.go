package audio

import (
	"context"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/vad"
)

func TestRecorderAcceptsInjectedDetectorAndPreservesPreRoll(t *testing.T) {
	detector := &fakeDetector{steps: []detectorStep{
		{},
		{},
		{speaking: true},
		{speaking: true, finish: true},
	}}
	recorder, err := NewRecorderWithDetector(config.AudioConfig{SampleRate: 16000, Channels: 1}, detector)
	if err != nil {
		t.Fatalf("NewRecorderWithDetector() error = %v", err)
	}

	var recording []float32
	for _, chunk := range [][]float32{{0.01}, {0.02}, {0.5}, {0.1}} {
		var done bool
		recording, done = recorder.processChunk(recording, chunk)
		if done {
			break
		}
	}

	want := []float32{0.01, 0.02, 0.5, 0.1}
	assertFloat32Slice(t, recording, want)
	if detector.processCalls != 4 {
		t.Fatalf("ProcessChunk calls = %d, want 4", detector.processCalls)
	}
}

func TestRecorderRejectsNilDetector(t *testing.T) {
	if _, err := NewRecorderWithDetector(config.AudioConfig{}, nil); err == nil {
		t.Fatal("NewRecorderWithDetector(nil) error = nil")
	}
}

func TestRecorderCloseDelegatesDetectorOnce(t *testing.T) {
	detector := &fakeDetector{}
	recorder, err := NewRecorderWithDetector(config.AudioConfig{}, detector)
	if err != nil {
		t.Fatalf("NewRecorderWithDetector() error = %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if detector.closeCalls != 1 {
		t.Fatalf("detector Close calls = %d, want 1", detector.closeCalls)
	}
}

type detectorStep struct {
	speaking bool
	finish   bool
}

type fakeDetector struct {
	steps        []detectorStep
	index        int
	processCalls int
	closeCalls   int
	speaking     bool
	handler      func(vad.Event)
}

func (f *fakeDetector) ProcessChunk([]float32) (bool, bool) {
	f.processCalls++
	if f.index >= len(f.steps) {
		return false, false
	}
	step := f.steps[f.index]
	f.index++
	f.speaking = step.speaking && !step.finish
	return step.speaking, step.finish
}

func (f *fakeDetector) Reset() { f.speaking = false }
func (f *fakeDetector) IsSpeaking() bool { return f.speaking }
func (f *fakeDetector) SetEventHandler(handler func(vad.Event)) { f.handler = handler }
func (f *fakeDetector) Close() error {
	f.closeCalls++
	return nil
}

var _ vad.Detector = (*fakeDetector)(nil)
var _ = context.Background
