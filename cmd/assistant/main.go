package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/console"
	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
	"github.com/dfc-coder/xarlatan/internal/stt"
	"github.com/dfc-coder/xarlatan/internal/tools"
	"github.com/dfc-coder/xarlatan/internal/tts"
)

var (
	cfgPath         = flag.String("config", "config.yaml", "path to config.yaml")
	logLevel        = flag.String("log", "info", "log level: debug|info|warn|error")
	noTools         = flag.Bool("no-tools", false, "disable all tools")
	reset           = flag.Bool("reset", false, "reset conversation history on startup")
	maxToolRounds   = flag.Int("max-tool-rounds", 4, "maximum tool rounds per user turn")
	maxHistoryBytes = flag.Int("max-history-bytes", 12_288, "maximum JSON bytes retained in conversation history")
	maxSummaryBytes = flag.Int("max-summary-bytes", 2_048, "maximum bytes retained in the untrusted memory summary")
	version         = flag.Bool("version", false, "print version and exit")
)

const buildVersion = "0.2.0"

func main() {
	flag.Parse()
	if *version {
		fmt.Println("assistant v" + buildVersion)
		return
	}
	setupLogger(*logLevel)
	if err := run(); err != nil {
		slog.Error("assistant", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if *maxToolRounds <= 0 {
		return fmt.Errorf("max-tool-rounds must be greater than zero")
	}
	if *maxHistoryBytes <= 0 {
		return fmt.Errorf("max-history-bytes must be greater than zero")
	}
	if *maxSummaryBytes <= 0 {
		return fmt.Errorf("max-summary-bytes must be greater than zero")
	}
	if *maxSummaryBytes >= *maxHistoryBytes {
		return fmt.Errorf("max-summary-bytes must be less than max-history-bytes")
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	transcriber, err := stt.New(cfg.STT, cfg.Audio.SampleRate)
	if err != nil {
		return fmt.Errorf("stt: %w", err)
	}
	defer transcriber.Close()

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
		return fmt.Errorf("llm server manager: %w", err)
	}
	if err := serverManager.Start(ctx); err != nil {
		return fmt.Errorf("llm server start: %w", err)
	}
	defer func() {
		if err := serverManager.Stop(context.Background()); err != nil {
			slog.Warn("llm server stop", "err", err)
		}
	}()
	if err := serverManager.WaitReady(ctx); err != nil {
		return fmt.Errorf("llm server readiness: %w", err)
	}

	llmClient, err := llm.NewClient(llm.ClientConfig{
		BaseURL:     cfg.LLM.BaseURL(),
		Temperature: cfg.LLM.Temperature,
		TopP:        cfg.LLM.TopP,
		MaxTokens:   cfg.LLM.MaxTokens,
	})
	if err != nil {
		return fmt.Errorf("llm client: %w", err)
	}

	memoryManager, err := memory.New(
		memory.Config{MaxHistoryBytes: *maxHistoryBytes, MaxSummaryBytes: *maxSummaryBytes},
		cfg.LLM.SystemPrompt,
		memory.ExtractiveSummarizer{},
	)
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	if *reset {
		memoryManager.Reset()
	}
	history := memoryManager.History()

	var registry *tools.Registry
	var executor *tools.Executor
	if !*noTools {
		registry, err = buildRegistry(cfg)
		if err != nil {
			return fmt.Errorf("tools: %w", err)
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
		return fmt.Errorf("agent runtime: %w", err)
	}

	recorder := audio.NewRecorder(cfg.Audio)
	speaker, err := tts.New(cfg.TTS, cfg.Audio.Device)
	if err != nil {
		return fmt.Errorf("tts: %w", err)
	}
	defer speaker.Close()
	status := console.NewStatusPrinter(os.Stderr)

	status.Set("Escuchando")
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		samples, err := recorder.RecordUntilSilence(ctx)
		if err != nil || len(samples) == 0 {
			if ctx.Err() != nil {
				return nil
			}
			status.Set("Escuchando")
			continue
		}

		status.Set("Procesando STT")
		t0 := time.Now()
		text, err := transcriber.Transcribe(samples)
		if err != nil || text == "" {
			status.Set("Escuchando")
			continue
		}
		slog.Debug("stt", "ms", time.Since(t0).Milliseconds(), "text", text)
		fmt.Printf("\n👤  %s\n", text)

		prepared, err := memoryManager.Prepare(ctx, text)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("memory prepare", "err", err)
			status.Set("Escuchando")
			continue
		}
		history = prepared.History
		slog.Debug(
			"memory prepare",
			"input_bytes", prepared.Trace.InputBytes,
			"output_bytes", prepared.Trace.OutputBytes,
			"dropped_turns", prepared.Trace.DroppedTurns,
			"summary_generated", prepared.Trace.SummaryGenerated,
		)

		status.Set("Pensando")
		t0 = time.Now()
		turn, err := agent.Run(ctx, orchestrator.Request{Input: text, History: history})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("agent", "err", err)
			status.Set("Escuchando")
			continue
		}
		printAgentTrace(turn.Trace)

		snapshot, err := memoryManager.Update(ctx, turn.History)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("memory update: %w", err)
		}
		history = snapshot.History
		slog.Debug(
			"memory compact",
			"input_messages", snapshot.Trace.InputMessages,
			"input_bytes", snapshot.Trace.InputBytes,
			"output_messages", snapshot.Trace.OutputMessages,
			"output_bytes", snapshot.Trace.OutputBytes,
			"dropped_turns", snapshot.Trace.DroppedTurns,
			"dropped_messages", snapshot.Trace.DroppedMessages,
			"summary_generated", snapshot.Trace.SummaryGenerated,
			"summary_bytes", snapshot.Trace.SummaryBytes,
		)
		slog.Debug("agent", "ms", time.Since(t0).Milliseconds(), "rounds", len(turn.Trace.Rounds))
		fmt.Printf("🤖  %s\n", turn.Reply)

		status.Set("Hablando")
		if err := speaker.Speak(ctx, turn.Reply); err != nil {
			slog.Warn("tts", "err", err)
		}
		status.Set("Escuchando")
	}
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

func printAgentTrace(trace orchestrator.Trace) {
	for _, round := range trace.Rounds {
		for _, tool := range round.Tools {
			prefix := "✓"
			if !tool.Success {
				prefix = "✗"
			}
			fmt.Printf("   %s %s\n", prefix, tool.Tool)
		}
	}
}

// buildRegistry registers candidate tools through a configuration-derived policy.
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

func setupLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}
