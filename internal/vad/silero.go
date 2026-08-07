package vad

import (
	"fmt"
	"os"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// SileroConfig contains the runtime parameters needed by sherpa-onnx Silero
// VAD. The k2-fsa maintained model used by Xarlatan is 16 kHz only.
type SileroConfig struct {
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

func (c SileroConfig) validate() error {
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

type sileroBackend interface {
	AcceptWaveform([]float32)
	IsSpeech() bool
	IsEmpty() bool
	Pop()
	Clear()
	Close()
}

type sherpaSileroBackend struct {
	detector *sherpa.VoiceActivityDetector
}

func (b *sherpaSileroBackend) AcceptWaveform(samples []float32) { b.detector.AcceptWaveform(samples) }
func (b *sherpaSileroBackend) IsSpeech() bool                   { return b.detector.IsSpeech() }
func (b *sherpaSileroBackend) IsEmpty() bool                    { return b.detector.IsEmpty() }
func (b *sherpaSileroBackend) Pop()                             { b.detector.Pop() }
func (b *sherpaSileroBackend) Clear()                           { b.detector.Clear() }
func (b *sherpaSileroBackend) Close() {
	if b == nil || b.detector == nil {
		return
	}
	sherpa.DeleteVoiceActivityDetector(b.detector)
	b.detector = nil
}

// SileroDetector adapts sherpa-onnx VAD to Xarlatan's detector contract. It
// keeps a segment active until sherpa reports a completed speech segment so the
// capture layer retains the full trailing audio needed by STT.
type SileroDetector struct {
	mu       sync.Mutex
	backend  sileroBackend
	speaking bool
	closed   bool
	onEvent  func(Event)
}

// NewSilero constructs the native sherpa-onnx VAD. The caller owns Close.
func NewSilero(config SileroConfig) (*SileroDetector, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	modelConfig := sherpa.VadModelConfig{
		SileroVad: sherpa.SileroVadModelConfig{
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
	native := sherpa.NewVoiceActivityDetector(&modelConfig, float32(config.BufferSize.Seconds()))
	if native == nil {
		return nil, fmt.Errorf("creating sherpa-onnx Silero VAD")
	}
	return newSileroWithBackend(&sherpaSileroBackend{detector: native}), nil
}

func newSileroWithBackend(backend sileroBackend) *SileroDetector {
	return &SileroDetector{backend: backend}
}

// ProcessChunk accepts one chunk and returns the same capture-oriented contract
// as the legacy energy detector. Once speech begins, chunks remain part of the
// utterance until sherpa queues a completed segment.
func (s *SileroDetector) ProcessChunk(samples []float32) (bool, bool) {
	if s == nil || len(samples) == 0 {
		return false, false
	}

	var events []Event
	s.mu.Lock()
	if s.closed || s.backend == nil {
		s.mu.Unlock()
		return false, false
	}

	s.backend.AcceptWaveform(samples)
	detected := s.backend.IsSpeech()
	if detected && !s.speaking {
		s.speaking = true
		events = append(events, Event{Type: EventSpeechStarted})
	}

	finished := !s.backend.IsEmpty()
	if finished {
		// A completed segment is sufficient to end this voice turn. Consume one
		// segment; Recorder.Reset clears any remaining queue before the next turn.
		s.backend.Pop()
		if !s.speaking {
			events = append(events, Event{Type: EventSpeechStarted})
		}
		s.speaking = false
		events = append(events, Event{Type: EventSpeechEnded})
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

func (s *SileroDetector) Reset() {
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

func (s *SileroDetector) IsSpeaking() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.speaking
}

func (s *SileroDetector) SetEventHandler(handler func(Event)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onEvent = handler
	s.mu.Unlock()
}

// Close releases the native detector once and is safe to call repeatedly.
func (s *SileroDetector) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	backend := s.backend
	s.backend = nil
	s.speaking = false
	s.mu.Unlock()
	if backend != nil {
		backend.Close()
	}
	return nil
}

var _ Detector = (*SileroDetector)(nil)
