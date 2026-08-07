package vad

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSileroDetectorEmitsSingleSpeechLifecycle(t *testing.T) {
	backend := &fakeSileroBackend{steps: []sileroStep{
		{},
		{speech: true},
		{speech: true},
		{}, // trailing audio remains part of the active utterance
		{complete: true},
	}}
	detector := newSileroWithBackend(backend)
	var events []EventType
	detector.SetEventHandler(func(event Event) { events = append(events, event.Type) })

	if active, done := detector.ProcessChunk([]float32{0}); active || done {
		t.Fatalf("silence = (%v,%v), want false,false", active, done)
	}
	if active, done := detector.ProcessChunk([]float32{0.5}); !active || done {
		t.Fatalf("speech start = (%v,%v), want true,false", active, done)
	}
	if active, done := detector.ProcessChunk([]float32{0.5}); !active || done {
		t.Fatalf("continued speech = (%v,%v), want true,false", active, done)
	}
	if active, done := detector.ProcessChunk([]float32{0}); !active || done {
		t.Fatalf("trailing audio = (%v,%v), want true,false", active, done)
	}
	if active, done := detector.ProcessChunk([]float32{0}); !active || !done {
		t.Fatalf("completed segment = (%v,%v), want true,true", active, done)
	}

	want := []EventType{EventSpeechStarted, EventSpeechEnded}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if backend.popCount != 1 {
		t.Fatalf("Pop calls = %d, want 1", backend.popCount)
	}
	if detector.IsSpeaking() {
		t.Fatal("detector still speaking after completed segment")
	}
}

func TestSileroDetectorCompletedSegmentCannotMissStartEvent(t *testing.T) {
	backend := &fakeSileroBackend{steps: []sileroStep{{complete: true}}}
	detector := newSileroWithBackend(backend)
	var events []EventType
	detector.SetEventHandler(func(event Event) { events = append(events, event.Type) })

	active, done := detector.ProcessChunk([]float32{0.3})
	if !active || !done {
		t.Fatalf("completed segment = (%v,%v), want true,true", active, done)
	}
	want := []EventType{EventSpeechStarted, EventSpeechEnded}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSileroDetectorResetClearsNativeState(t *testing.T) {
	backend := &fakeSileroBackend{steps: []sileroStep{{speech: true}}}
	detector := newSileroWithBackend(backend)
	detector.ProcessChunk([]float32{0.5})
	if !detector.IsSpeaking() {
		t.Fatal("detector should be speaking before Reset")
	}

	detector.Reset()
	if detector.IsSpeaking() {
		t.Fatal("detector should be idle after Reset")
	}
	if backend.clearCount != 1 {
		t.Fatalf("Clear calls = %d, want 1", backend.clearCount)
	}
}

func TestSileroDetectorCloseIsIdempotent(t *testing.T) {
	backend := &fakeSileroBackend{}
	detector := newSileroWithBackend(backend)
	if err := detector.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := detector.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if backend.closeCount != 1 {
		t.Fatalf("Close calls = %d, want 1", backend.closeCount)
	}
	if active, done := detector.ProcessChunk([]float32{0.5}); active || done {
		t.Fatalf("closed detector ProcessChunk = (%v,%v), want false,false", active, done)
	}
}

func TestSileroDetectorIgnoresEmptyChunk(t *testing.T) {
	backend := &fakeSileroBackend{steps: []sileroStep{{speech: true}}}
	detector := newSileroWithBackend(backend)
	if active, done := detector.ProcessChunk(nil); active || done {
		t.Fatalf("empty chunk = (%v,%v), want false,false", active, done)
	}
	if backend.acceptCount != 0 {
		t.Fatalf("AcceptWaveform calls = %d, want 0", backend.acceptCount)
	}
}

func TestSileroConfigValidation(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "silero_vad.onnx")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	valid := SileroConfig{
		Model:              model,
		Threshold:          0.5,
		MinSilenceDuration: 800 * time.Millisecond,
		MinSpeechDuration:  250 * time.Millisecond,
		MaxSpeechDuration:  30 * time.Second,
		SampleRate:         16000,
		NumThreads:         1,
		Provider:           "cpu",
		WindowSize:         512,
		BufferSize:         35 * time.Second,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*SileroConfig)
	}{
		{name: "missing model", mutate: func(c *SileroConfig) { c.Model = filepath.Join(dir, "missing.onnx") }},
		{name: "sample rate", mutate: func(c *SileroConfig) { c.SampleRate = 8000 }},
		{name: "threshold", mutate: func(c *SileroConfig) { c.Threshold = 0 }},
		{name: "silence", mutate: func(c *SileroConfig) { c.MinSilenceDuration = 0 }},
		{name: "threads", mutate: func(c *SileroConfig) { c.NumThreads = 0 }},
		{name: "buffer", mutate: func(c *SileroConfig) { c.BufferSize = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)
			if err := cfg.validate(); err == nil {
				t.Fatal("validate() error = nil")
			}
		})
	}
}

type sileroStep struct {
	speech   bool
	complete bool
}

type fakeSileroBackend struct {
	steps      []sileroStep
	current    sileroStep
	index      int
	acceptCount int
	popCount   int
	clearCount int
	closeCount int
}

func (f *fakeSileroBackend) AcceptWaveform([]float32) {
	f.acceptCount++
	if f.index < len(f.steps) {
		f.current = f.steps[f.index]
		f.index++
	} else {
		f.current = sileroStep{}
	}
}

func (f *fakeSileroBackend) IsSpeech() bool { return f.current.speech }
func (f *fakeSileroBackend) IsEmpty() bool  { return !f.current.complete }
func (f *fakeSileroBackend) Pop() {
	f.popCount++
	f.current.complete = false
}
func (f *fakeSileroBackend) Clear() {
	f.clearCount++
	f.current = sileroStep{}
}
func (f *fakeSileroBackend) Close() { f.closeCount++ }
