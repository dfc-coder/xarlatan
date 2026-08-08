package main

import (
	"errors"
	"fmt"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/stt"
	"github.com/dfc-coder/xarlatan/internal/tts"
)

func newLegacyVoiceInference(cfg *config.Config) (*voiceInferenceRuntime, error) {
	transcriber, err := stt.New(cfg.STT, cfg.Audio.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("stt: %w", err)
	}
	synthesizer, err := tts.New(cfg.TTS, cfg.Audio.Device)
	if err != nil {
		_ = transcriber.Close()
		return nil, fmt.Errorf("tts: %w", err)
	}
	return &voiceInferenceRuntime{
		transcriber: transcriber,
		synthesizer: synthesizer,
		close: func() error {
			return errors.Join(synthesizer.Close(), transcriber.Close())
		},
	}, nil
}
