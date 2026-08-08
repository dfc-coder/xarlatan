package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

func newLegacyAgentRuntime(ctx context.Context, cfg *config.Config) (_ *responseRuntime, err error) {
	if *maxToolRounds <= 0 {
		return nil, fmt.Errorf("max-tool-rounds must be greater than zero")
	}
	if *maxHistoryBytes <= 0 {
		return nil, fmt.Errorf("max-history-bytes must be greater than zero")
	}
	if *maxSummaryBytes <= 0 {
		return nil, fmt.Errorf("max-summary-bytes must be greater than zero")
	}
	if *maxSummaryBytes >= *maxHistoryBytes {
		return nil, fmt.Errorf("max-summary-bytes must be less than max-history-bytes")
	}

	serverManager, err := llm.NewServerManager(llm.ServerConfig{
		Mode:            llm.ServerMode(cfg.LLM.Mode),
		Binary:          cfg.LLM.ServerBinary,
		Args:            llamaServerArgs(cfg.LLM),
		HealthURL:       cfg.LLM.BaseURL() + "/health",
		StartupTimeout:  cfg.LLM.StartupTimeout(),
		ShutdownTimeout: cfg.LLM.ShutdownTimeout(),
		HealthInterval:  cfg.LLM.HealthInterval(),
	}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("llm server manager: %w", err)
	}
	if err := serverManager.Start(ctx); err != nil {
		return nil, fmt.Errorf("llm server start: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if stopErr := serverManager.Stop(context.Background()); stopErr != nil {
				slog.Warn("llm server stop after setup failure", "err", stopErr)
			}
		}
	}()
	if err := serverManager.WaitReady(ctx); err != nil {
		return nil, fmt.Errorf("llm server readiness: %w", err)
	}

	llmClient, err := llm.NewClient(llm.ClientConfig{
		BaseURL:     cfg.LLM.BaseURL(),
		Temperature: cfg.LLM.Temperature,
		TopP:        cfg.LLM.TopP,
		MaxTokens:   cfg.LLM.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("llm client: %w", err)
	}

	memoryManager, err := memory.New(
		memory.Config{MaxHistoryBytes: *maxHistoryBytes, MaxSummaryBytes: *maxSummaryBytes},
		cfg.LLM.SystemPrompt,
		memory.ExtractiveSummarizer{},
	)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}

	var registry *tools.Registry
	var executor *tools.Executor
	if !*noTools {
		registry, err = buildRegistry(cfg)
		if err != nil {
			return nil, fmt.Errorf("tools: %w", err)
		}
		executor = tools.NewExecutor(registry)
		slog.Info("tools enabled", "list", registry.Names())
	}

	agent, err := orchestrator.NewAgentRuntime(
		orchestrator.RuntimeConfig{MaxToolRounds: *maxToolRounds},
		llmClient,
		registry,
		executor,
		orchestrator.SlogObserver{},
	)
	if err != nil {
		return nil, fmt.Errorf("agent runtime: %w", err)
	}

	session, err := conversation.New(memoryManager, agent)
	if err != nil {
		return nil, fmt.Errorf("conversation session: %w", err)
	}

	committed = true
	return &responseRuntime{
		responder: session,
		close: func() error {
			return serverManager.Stop(context.Background())
		},
	}, nil
}

func llamaServerArgs(cfg config.LLMConfig) []string {
	return []string{
		"--model", cfg.Model,
		"--host", cfg.Host,
		"--port", fmt.Sprintf("%d", cfg.Port),
		"--ctx-size", fmt.Sprintf("%d", cfg.ContextSize),
		"--n-gpu-layers", fmt.Sprintf("%d", cfg.NGPULayers),
		"--threads", fmt.Sprintf("%d", cfg.Threads),
		"--log-disable",
	}
}

func buildRegistry(cfg *config.Config) (*tools.Registry, error) {
	policy := tools.DefaultToolPolicy()
	fsCfg := cfg.Tools.Filesystem
	var sandbox *tools.FilesystemSandbox
	if fsCfg.Enabled {
		policy = policy.WithAllowed("fs_read", "fs_list", "fs_stat")
		if fsCfg.AllowMutations {
			policy = policy.WithAllowed("fs_write", "fs_delete", "fs_mkdir")
		}
		var err error
		sandbox, err = tools.NewFilesystemSandbox(fsCfg.Root, fsCfg.MaxReadBytes, fsCfg.MaxWriteBytes)
		if err != nil {
			return nil, fmt.Errorf("constructing filesystem sandbox: %w", err)
		}
	}

	registry := tools.NewRegistry(policy)
	candidates := []tools.Tool{
		tools.FSRead{Sandbox: sandbox},
		tools.FSWrite{Sandbox: sandbox},
		tools.FSList{Sandbox: sandbox},
		tools.FSDelete{Sandbox: sandbox},
		tools.FSStat{Sandbox: sandbox},
		tools.FSMkdir{Sandbox: sandbox},
		&tools.WebSearch{
			Provider: cfg.Tools.WebSearch.Provider,
			APIKey:   cfg.Tools.WebSearch.APIKey,
			BaseURL:  cfg.Tools.WebSearch.BaseURL,
		},
		tools.WebFetch{},
	}
	for _, candidate := range candidates {
		if err := registry.Register(candidate); err != nil && !tools.IsToolDenied(err) {
			return nil, fmt.Errorf("registering tool %q: %w", candidate.Name(), err)
		}
	}
	return registry, nil
}
