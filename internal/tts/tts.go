// Package tts provides text-to-speech via sherpa-onnx.
package tts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/config"
)

// Synthesizer generates normalized mono speech buffers via sherpa-onnx.
type Synthesizer struct {
	cfg config.TTSConfig
	tts *sherpa.OfflineTts
}

// New creates an offline Synthesizer. The audioDev parameter is retained for
// source compatibility; playback ownership now belongs to audio.Playback.
func New(cfg config.TTSConfig, _ string) (*Synthesizer, error) {
	ttsCfg := sherpa.OfflineTtsConfig{}
	ttsCfg.Model.Vits.Model = cfg.Model
	ttsCfg.Model.Vits.Tokens = cfg.Tokens
	ttsCfg.Model.Vits.DataDir = cfg.DataDir
	ttsCfg.Model.Vits.NoiseScale = float32(cfg.NoiseScale)
	ttsCfg.Model.Vits.NoiseScaleW = float32(cfg.NoiseW)
	ttsCfg.Model.Vits.LengthScale = float32(cfg.LengthScale)
	ttsCfg.Model.NumThreads = 4
	ttsCfg.Model.Provider = "cpu"
	ttsCfg.Model.Debug = 0

	engine := sherpa.NewOfflineTts(&ttsCfg)
	if engine == nil {
		return nil, fmt.Errorf("sherpa tts: nil")
	}
	return &Synthesizer{cfg: cfg, tts: engine}, nil
}

// Close releases model resources.
func (s *Synthesizer) Close() error {
	if s != nil && s.tts != nil {
		sherpa.DeleteOfflineTts(s.tts)
		s.tts = nil
	}
	return nil
}

// Synthesize generates one normalized mono audio buffer. Offline generation
// cannot be preempted once CGo enters sherpa, so context is checked before and
// immediately after the generation boundary.
func (s *Synthesizer) Synthesize(ctx context.Context, text string) (audio.Buffer, error) {
	if s == nil || s.tts == nil {
		return audio.Buffer{}, fmt.Errorf("synthesizer is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return audio.Buffer{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return audio.Buffer{}, nil
	}

	generated, err := s.Synthesise(text)
	if err != nil {
		return audio.Buffer{}, err
	}
	if err := ctx.Err(); err != nil {
		return audio.Buffer{}, err
	}
	return audio.Buffer{
		Samples:    append([]float32(nil), generated.Samples...),
		SampleRate: generated.SampleRate,
		Channels:   1,
	}, nil
}

// Synthesise retains direct access to sherpa output for low-level callers.
func (s *Synthesizer) Synthesise(text string) (*sherpa.GeneratedAudio, error) {
	genCfg := sherpa.GenerationConfig{
		SilenceScale: 0.2,
		Speed:        1.0,
		Sid:          s.cfg.SpeakerID,
	}

	slog.Debug("sherpa tts synthesise", "chars", len(text))
	generated := s.tts.GenerateWithConfig(text, &genCfg, nil)
	if generated == nil {
		return nil, fmt.Errorf("sherpa tts: nil generated audio")
	}
	return generated, nil
}
