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
	cfgPath       = flag.String("config", "config.yaml", "path to config.yaml")
	logLevel      = flag.String("log", "info", "log level: debug|info|warn|error")
	noTools       = flag.Bool("no-tools", false, "disable all tools")
	reset         = flag.Bool("reset", false, "reset conversation history on startup")
	maxToolRounds = flag.Int("max-tool-rounds", 4, "maximum tool rounds per user turn")
	version       = flag.Bool("version", false, "print version and exit")
)

const buildVersion = "0.2.0"

func main() {
	flag.Parse()
	if *version {
		fmt.Println("assistant v" + buildVersion)
		os.Exit(0)
	}
	setupLogger(*logLevel)
	if *maxToolRounds <= 0 {
		slog.Error("agent config", "err", "max-tool-rounds must be greater than zero")
		os.Exit(1)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("config validation", "err", err)
		os.Exit(1)
	}

	transcriber, err := stt.New(cfg.STT, cfg.Audio.SampleRate)
	if err != nil {
		slog.Error("stt", "err", err)
		os.Exit(1)
	}
	defer transcriber.Close()

	llmClient, err := llm.New(cfg.LLM)
	if err != nil {
		slog.Error("llm", "err", err)
		os.Exit(1)
	}
	defer llmClient.Close()

	summary := ""
	history := memory.Compose(cfg.LLM.SystemPrompt, summary, nil)
	var registry *tools.Registry
	var executor *tools.Executor

	if *reset {
		summary = ""
		history = memory.Compose(cfg.LLM.SystemPrompt, summary, nil)
	}
	if !*noTools {
		registry, err = buildRegistry(cfg)
		if err != nil {
			slog.Error("tools", "err", err)
			os.Exit(1)
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
		slog.Error("agent runtime", "err", err)
		os.Exit(1)
	}

	recorder := audio.NewRecorder(cfg.Audio)
	speaker, err := tts.New(cfg.TTS, cfg.Audio.Device)
	if err != nil {
		slog.Error("tts", "err", err)
		os.Exit(1)
	}
	defer speaker.Close()
	status := console.NewStatusPrinter(os.Stderr)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	status.Set("Escuchando")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		samples, err := recorder.RecordUntilSilence(ctx)
		if err != nil || len(samples) == 0 {
			if ctx.Err() != nil {
				return
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

		status.Set("Pensando")
		t0 = time.Now()
		turn, err := agent.Run(ctx, orchestrator.Request{Input: text, History: history})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("agent", "err", err)
			status.Set("Escuchando")
			continue
		}
		printAgentTrace(turn.Trace)

		snap := memory.Compact(turn.History, 20)
		slog.Debug(
			"memory compact",
			"input_messages", snap.Trace.InputMessages,
			"dropped_messages", snap.Trace.DroppedMessages,
			"window_messages", snap.Trace.WindowMessages,
			"summary_generated", snap.Trace.SummaryGenerated,
		)
		summary = memory.MergeSummary(summary, snap.Summary)
		history = memory.Compose(cfg.LLM.SystemPrompt, summary, snap.Window)
		slog.Debug("agent", "ms", time.Since(t0).Milliseconds(), "rounds", len(turn.Trace.Rounds))
		fmt.Printf("🤖  %s\n", turn.Reply)

		status.Set("Hablando")
		if err := speaker.Speak(ctx, turn.Reply); err != nil {
			slog.Warn("tts", "err", err)
		}
		status.Set("Escuchando")
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
