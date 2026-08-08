package main

import (
	"context"
	"fmt"

	"github.com/dfc-coder/xarlatan/internal/application"
	"github.com/dfc-coder/xarlatan/internal/config"
)

type voiceInferenceRuntime struct {
	transcriber application.Transcriber
	synthesizer application.Synthesizer
	close       func() error
}

func newVoiceInferenceRuntime(ctx context.Context, cfg *config.Config) (*voiceInferenceRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("voice inference config is nil")
	}
	switch cfg.Agent.Mode {
	case "voice_gateway":
		return newVoiceGatewayInference(ctx, cfg)
	case "legacy_native", "":
		return newLegacyVoiceInference(cfg)
	default:
		return nil, fmt.Errorf("unsupported agent mode %q", cfg.Agent.Mode)
	}
}

func (r *voiceInferenceRuntime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}
