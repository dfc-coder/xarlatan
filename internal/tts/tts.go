// Package tts provides text-to-speech via sherpa-onnx.
package tts

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/dfc-coder/xarlatan/internal/config"
)

// Speaker synthesises speech via sherpa-onnx and plays it immediately.
type Speaker struct {
	cfg      config.TTSConfig
	tts      *sherpa.OfflineTts
	audioDev string
}

// New creates a Speaker.
func New(cfg config.TTSConfig, audioDev string) (*Speaker, error) {
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

	tts := sherpa.NewOfflineTts(&ttsCfg)
	if tts == nil {
		return nil, fmt.Errorf("sherpa tts: nil")
	}

	return &Speaker{cfg: cfg, tts: tts, audioDev: audioDev}, nil
}

// Close releases model resources.
func (s *Speaker) Close() error {
	if s.tts != nil {
		sherpa.DeleteOfflineTts(s.tts)
		s.tts = nil
	}
	return nil
}

// Speak synthesises text and plays it through the speaker.
func (s *Speaker) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	audio, err := s.Synthesise(text)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "assistant-tts-*.wav")
	if err != nil {
		return fmt.Errorf("create temp wav: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("close temp wav: %w", err)
	}
	defer os.Remove(name)

	if !audio.Save(name) {
		return fmt.Errorf("saving synthesized wav")
	}

	cmd := exec.CommandContext(ctx, "aplay", "-D", s.audioDev, name)
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Debug("aplay output", "out", string(out))
		return fmt.Errorf("aplay: %w", err)
	}
	return nil
}

// Synthesise generates speech audio for the given text.
func (s *Speaker) Synthesise(text string) (*sherpa.GeneratedAudio, error) {
	genCfg := sherpa.GenerationConfig{
		SilenceScale: 0.2,
		Speed:        1.0,
		Sid:          s.cfg.SpeakerID,
	}

	slog.Debug("sherpa tts synthesise", "chars", len(text))
	audio := s.tts.GenerateWithConfig(text, &genCfg, nil)
	if audio == nil {
		return nil, fmt.Errorf("sherpa tts: nil generated audio")
	}
	return audio, nil
}
