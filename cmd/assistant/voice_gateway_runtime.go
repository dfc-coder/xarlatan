package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dfc-coder/xarlatan/internal/acp"
	"github.com/dfc-coder/xarlatan/internal/config"
)

func newVoiceGatewayRuntime(ctx context.Context, cfg *config.Config) (*responseRuntime, error) {
	runtime, err := acp.StartRuntime(ctx, acp.RuntimeConfig{
		Binary:          cfg.Agent.ACP.Binary,
		Args:            append([]string(nil), cfg.Agent.ACP.Args...),
		CWD:             cfg.Agent.ACP.CWD,
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("ACP runtime: %w", err)
	}
	info := runtime.AgentInfo()
	if info.Name != "" || info.Title != "" || info.Version != "" {
		slog.Info("ACP agent ready", "name", info.Name, "title", info.Title, "version", info.Version)
	}
	responder, err := runtime.Responder()
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("ACP responder: %w", err)
	}
	return &responseRuntime{
		responder: responder,
		close:     runtime.Close,
	}, nil
}
