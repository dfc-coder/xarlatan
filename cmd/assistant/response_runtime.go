package main

import (
	"context"
	"fmt"

	"github.com/dfc-coder/xarlatan/internal/application"
	"github.com/dfc-coder/xarlatan/internal/config"
)

type responseRuntime struct {
	responder application.Responder
	close     func() error
}

func newResponseRuntime(ctx context.Context, cfg *config.Config) (*responseRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("response runtime config is nil")
	}
	switch cfg.Agent.Mode {
	case "voice_gateway":
		return newVoiceGatewayRuntime(ctx, cfg)
	case "legacy_native", "":
		return newLegacyAgentRuntime(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported agent mode %q", cfg.Agent.Mode)
	}
}

func (r *responseRuntime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}
