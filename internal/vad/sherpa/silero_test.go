package sherpa

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	corevad "github.com/dfc-coder/xarlatan/internal/vad"
)

func TestDetectorEmitsSingleSpeechLifecycle(t *testing.T) {
	backend := &fakeBackend{steps: []step{
		{},
		{speech: true},
		{speech: true},
		{},
		{complete: true},
	}}
	detector := newWithBackend(backend)
	var events []corevad.EventType
	detector.SetEventHandler(func(event corevad.Event) { events = append(events, event.Type) })

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

	want := []corevad.EventType{corevad.EventSpeechStarted, corevad.EventSpeechEnded}
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

func TestCompletedSegmentCannotMissStartEvent(t *testing.T) {
	backend := &fakeBackend{steps: []step{{complete: true}}}
	detector := newWithBackend(backend)
	var events []corevad.EventType
	detector.SetEventHandler(func(event corevad.Event) { events = append(events, event.Type) })

	active, done := detector.ProcessChunk([]float32{0.3})
	if !active || !done {
		t.Fatalf("completed segment = (%v,%v), want true,true", active, done)
	}
	want := []corevad.EventType{corevad.EventSpeechStarted, corevad.EventSpeechEnded}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestResetClearsNativeState(t *testing.T) {
	backend := &fakeBackend{steps: []step{{speech: true}}}
	detector := newWithBackend(backend)
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

func TestCloseIsIdempotent(t *testing.T) {
	backend := &fakeBackend{}
	detector := newWithBackend(backend)
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

func TestEmptyChunkIsIgnored(t *testing.T) {
	backend := &fakeBackend{steps: []step{{speech: true}}}
	detector := newWithBackend(backend)
	if active, done := detector.ProcessChunk(nil); active || done {
		t.Fatalf("empty chunk = (%v,%v), want false,false", active, done)
	}
	if backend.acceptCount != 0 {
		t.Fatalf("AcceptWaveform calls = %d, want 0", backend.acceptCount)
	}
}

func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "silero_vad.onnx")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	valid := Config{
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
		mutate func(*Config)
	}{
		{name: "missing model", mutate: func(c *Config) { c.Model = filepath.Join(dir, "missing.onnx") }},
		{name: "sample rate", mutate: func(c *Config) { c.SampleRate = 8000 }},
		{name: "threshold", mutate: func(c *Config) { c.Threshold = 0 }},
		{name: "silence", mutate: func(c *Config) { c.MinSilenceDuration = 0 }},
		{name: "speech duration", mutate: func(c *Config) { c.MinSpeechDuration = -time.Millisecond }},
		{name: "max speech", mutate: func(c *Config) { c.MaxSpeechDuration = 0 }},
		{name: "threads", mutate: func(c *Config) { c.NumThreads = 0 }},
		{name: "provider", mutate: func(c *Config) { c.Provider = "" }},
		{name: "window", mutate: func(c *Config) { c.WindowSize = 0 }},
		{name: "buffer", mutate: func(c *Config) { c.BufferSize = 0 }},
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

type step struct {
	speech   bool
	complete bool
}

type fakeBackend struct {
	steps       []step
	current     step
	index       int
	acceptCount int
	popCount    int
	clearCount  int
	closeCount  int
}

func (f *fakeBackend) AcceptWaveform([]float32) {
	f.acceptCount++
	if f.index < len(f.steps) {
		f.current = f.steps[f.index]
		f.index++
	} else {
		f.current = step{}
	}
}

func (f *fakeBackend) IsSpeech() bool { return f.current.speech }
func (f *fakeBackend) IsEmpty() bool  { return !f.current.complete }
func (f *fakeBackend) Pop() {
	f.popCount++
	f.current.complete = false
}
func (f *fakeBackend) Clear() {
	f.clearCount++
	f.current = step{}
}
func (f *fakeBackend) Close() { f.closeCount++ }
