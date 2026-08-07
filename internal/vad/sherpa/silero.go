// Package sherpa contains the native sherpa-onnx VAD adapter. It depends on
// the stable parent vad contract; the core VAD package does not depend on CGo.
package sherpa

import (
	"fmt"
	"os"
	"sync"
	"time"

	corevad "github.com/dfc-coder/xarlatan/internal/vad"
	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Config contains the runtime parameters needed by sherpa-onnx Silero VAD.
type Config struct {
	Model              string
	Threshold          float32
	MinSilenceDuration time.Duration
	MinSpeechDuration  time.Duration
	MaxSpeechDuration  time.Duration
	SampleRate         int
	NumThreads         int
	Provider           string
	WindowSize         int
	BufferSize         time.Duration
}

func (c Config) validate() error {
	if c.Model == "" {
		return fmt.Errorf("silero model is required")
	}
	info, err := os.Stat(c.Model)
	if err != nil {
		return fmt.Errorf("silero model %q: %w", c.Model, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("silero model %q is not a regular file", c.Model)
	}
	if c.SampleRate != 16000 {
		return fmt.Errorf("silero model requires 16000 Hz, got %d", c.SampleRate)
	}
	if c.Threshold <= 0 || c.Threshold >= 1 {
		return fmt.Errorf("silero threshold must be greater than 0 and less than 1")
	}
	if c.MinSilenceDuration <= 0 {
		return fmt.Errorf("silero minimum silence duration must be greater than zero")
	}
	if c.MinSpeechDuration < 0 {
		return fmt.Errorf("silero minimum speech duration must not be negative")
	}
	if c.MaxSpeechDuration <= 0 {
		return fmt.Errorf("silero maximum speech duration must be greater than zero")
	}
	if c.NumThreads <= 0 {
		return fmt.Errorf("silero num threads must be greater than zero")
	}
	if c.Provider == "" {
		return fmt.Errorf("silero provider is required")
	}
	if c.WindowSize <= 0 {
		return fmt.Errorf("silero window size must be greater than zero")
	}
	if c.BufferSize <= 0 {
		return fmt.Errorf("silero buffer size must be greater than zero")
	}
	return nil
}

type backend interface {
	AcceptWaveform([]float32)
	IsSpeech() bool
	IsEmpty() bool
	Pop()
	Clear()
	Close()
}

type nativeBackend struct {
	detector *sherpaonnx.VoiceActivityDetector
}

func (b *nativeBackend) AcceptWaveform(samples []float32) { b.detector.AcceptWaveform(samples) }
func (b *nativeBackend) IsSpeech() bool                   { return b.detector.IsSpeech() }
func (b *nativeBackend) IsEmpty() bool                    { return b.detector.IsEmpty() }
func (b *nativeBackend) Pop()                             { b.detector.Pop() }
func (b *nativeBackend) Clear()                           { b.detector.Clear() }
func (b *nativeBackend) Close() {
	if b == nil || b.detector == nil {
		return
	}
	sherpaonnx.DeleteVoiceActivityDetector(b.detector)
	b.detector = nil
}

// Detector adapts sherpa-onnx VAD to Xarlatan's stable VAD contract.
type Detector struct {
	mu       sync.Mutex
	backend  backend
	speaking bool
	closed   bool
	onEvent  func(corevad.Event)
}

// New constructs the native sherpa-onnx Silero VAD. The caller owns Close.
func New(config Config) (*Detector, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	modelConfig := sherpaonnx.VadModelConfig{
		SileroVad: sherpaonnx.SileroVadModelConfig{
			Model:              config.Model,
			Threshold:          config.Threshold,
			MinSilenceDuration: float32(config.MinSilenceDuration.Seconds()),
			MinSpeechDuration:  float32(config.MinSpeechDuration.Seconds()),
			WindowSize:         config.WindowSize,
			MaxSpeechDuration:  float32(config.MaxSpeechDuration.Seconds()),
		},
		SampleRate: config.SampleRate,
		NumThreads: config.NumThreads,
		Provider:   config.Provider,
	}
	native := sherpaonnx.NewVoiceActivityDetector(&modelConfig, float32(config.BufferSize.Seconds()))
	if native == nil {
		return nil, fmt.Errorf("creating sherpa-onnx Silero VAD")
	}
	return newWithBackend(&nativeBackend{detector: native}), nil
}

func newWithBackend(value backend) *Detector {
	return &Detector{backend: value}
}

// ProcessChunk retains trailing chunks until sherpa queues a completed segment.
func (s *Detector) ProcessChunk(samples []float32) (bool, bool) {
	if s == nil || len(samples) == 0 {
		return false, false
	}

	var events []corevad.Event
	s.mu.Lock()
	if s.closed || s.backend == nil {
		s.mu.Unlock()
		return false, false
	}

	s.backend.AcceptWaveform(samples)
	if s.backend.IsSpeech() && !s.speaking {
		s.speaking = true
		events = append(events, corevad.Event{Type: corevad.EventSpeechStarted})
	}

	finished := !s.backend.IsEmpty()
	if finished {
		s.backend.Pop()
		if !s.speaking {
			events = append(events, corevad.Event{Type: corevad.EventSpeechStarted})
		}
		s.speaking = false
		events = append(events, corevad.Event{Type: corevad.EventSpeechEnded})
	}
	active := s.speaking || finished
	handler := s.onEvent
	s.mu.Unlock()

	if handler != nil {
		for _, event := range events {
			handler(event)
		}
	}
	return active, finished
}

func (s *Detector) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.backend == nil {
		return
	}
	s.backend.Clear()
	s.speaking = false
}

func (s *Detector) IsSpeaking() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.speaking
}

func (s *Detector) SetEventHandler(handler func(corevad.Event)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onEvent = handler
	s.mu.Unlock()
}

// Close releases the native detector once and is safe to call repeatedly.
func (s *Detector) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	value := s.backend
	s.backend = nil
	s.speaking = false
	s.mu.Unlock()
	if value != nil {
		value.Close()
	}
	return nil
}

var _ corevad.Detector = (*Detector)(nil)
