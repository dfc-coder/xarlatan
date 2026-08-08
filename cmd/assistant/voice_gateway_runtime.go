package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/zeroclaw"
)

func newVoiceGatewayRuntime(ctx context.Context, cfg *config.Config) (*responseRuntime, error) {
	runtime, err := zeroclaw.StartRuntime(ctx, zeroclaw.RuntimeConfig{
		Binary:          cfg.Agent.ZeroClaw.Binary,
		AgentAlias:      cfg.Agent.ZeroClaw.AgentAlias,
		CWD:             cfg.Agent.ZeroClaw.CWD,
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("zeroclaw runtime: %w", err)
	}
	responder, err := runtime.Responder()
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("zeroclaw responder: %w", err)
	}
	return &responseRuntime{
		responder: responder,
		close:     runtime.Close,
	}, nil
}
