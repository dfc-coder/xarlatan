package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dfc-coder/xarlatan/internal/application"
	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/barge"
	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/console"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
	"github.com/dfc-coder/xarlatan/internal/stt"
	"github.com/dfc-coder/xarlatan/internal/tools"
	"github.com/dfc-coder/xarlatan/internal/tts"
	sherpavad "github.com/dfc-coder/xarlatan/internal/vad/sherpa"
	"github.com/dfc-coder/xarlatan/internal/wake"
)

var (
	cfgPath         = flag.String("config", "config.yaml", "path to config.yaml")
	logLevel        = flag.String("log", "info", "log level: debug|info|warn|error")
	noTools         = flag.Bool("no-tools", false, "disable all tools")
	wakeEnabled     = flag.Bool("wake", true, "require wake word before each voice command")
	wakeWord        = flag.String("wake-word", "xarlatan", "primary wake word or phrase")
	wakeAliases     = flag.String("wake-aliases", "charlatan,charlatán", "comma-separated wake aliases")
	bargeEnabled    = flag.Bool("barge-in", true, "allow wake-qualified voice stop during playback")
	maxToolRounds   = flag.Int("max-tool-rounds", 4, "maximum tool rounds per user turn")
	maxHistoryBytes = flag.Int("max-history-bytes", 12_288, "maximum JSON bytes retained in conversation history")
	maxSummaryBytes = flag.Int("max-summary-bytes", 2_048, "maximum bytes retained in the untrusted memory summary")
	version         = flag.Bool("version", false, "print version and exit")
)

var buildVersion = "dev"

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
	vadRuntime, err := cfg.SileroVADConfig()
	if err != nil {
		return fmt.Errorf("vad config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	transcriber, err := stt.New(cfg.STT, cfg.Audio.SampleRate)
	if err != nil {
		return fmt.Errorf("stt: %w", err)
	}
	defer func() {
		if err := transcriber.Close(); err != nil {
			slog.Warn("stt close", "err", err)
		}
	}()

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

	session, err := conversation.New(memoryManager, agent)
	if err != nil {
		return fmt.Errorf("conversation session: %w", err)
	}

	voiceDetector, err := sherpavad.New(sherpavad.Config{
		Model:              vadRuntime.Model,
		Threshold:          vadRuntime.Threshold,
		MinSilenceDuration: vadRuntime.MinSilenceDuration,
		MinSpeechDuration:  vadRuntime.MinSpeechDuration,
		MaxSpeechDuration:  vadRuntime.MaxSpeechDuration,
		SampleRate:         vadRuntime.SampleRate,
		NumThreads:         vadRuntime.NumThreads,
		Provider:           vadRuntime.Provider,
		WindowSize:         vadRuntime.WindowSize,
		BufferSize:         vadRuntime.BufferSize,
	})
	if err != nil {
		return fmt.Errorf("vad: %w", err)
	}
	recorder, err := audio.NewContinuousRecorderWithDetector(cfg.Audio, voiceDetector)
	if err != nil {
		_ = voiceDetector.Close()
		return fmt.Errorf("continuous audio recorder: %w", err)
	}
	defer func() {
		if err := recorder.Close(); err != nil {
			slog.Warn("recorder close", "err", err)
		}
	}()

	synthesizer, err := tts.New(cfg.TTS, cfg.Audio.Device)
	if err != nil {
		return fmt.Errorf("tts: %w", err)
	}
	defer func() {
		if err := synthesizer.Close(); err != nil {
			slog.Warn("tts close", "err", err)
		}
	}()
	playback := audio.NewPlayback(cfg.Audio.Device, cfg.Audio.SampleRate, cfg.Audio.Channels)
	playback.UseContinuousCaptureGuard()
	player, err := audio.NewResponsePlayback(playback)
	if err != nil {
		return fmt.Errorf("response playback: %w", err)
	}
	defer func() {
		if err := player.Close(); err != nil {
			slog.Warn("playback close", "err", err)
		}
	}()
	status := console.NewStatusPrinter(os.Stderr)
	observer := newCaptureGateObserver(newConsoleObserver(status), recorder)

	voiceCoordinator, err := application.NewCoordinator(application.Dependencies{
		Input:       recorder,
		Transcriber: transcriber,
		Responder:   session,
		Synthesizer: synthesizer,
		Player:      player,
		Observer:    observer,
		View:        newConsoleView(os.Stdout),
	})
	if err != nil {
		return fmt.Errorf("voice coordinator: %w", err)
	}

	wakeDetector, err := wake.NewPhraseDetector(*wakeWord, splitWakeAliases(*wakeAliases))
	if err != nil {
		return fmt.Errorf("wake detector: %w", err)
	}
	if *wakeEnabled {
		voiceCoordinator.SetWakeDetector(wakeDetector)
	}
	if *bargeEnabled {
		bargeController, err := application.NewBargeInController(
			recorder,
			transcriber,
			barge.NewExplicitStopPolicy(wakeDetector),
		)
		if err != nil {
			return fmt.Errorf("barge-in controller: %w", err)
		}
		voiceCoordinator.SetInterruptSource(bargeController)
	}
	return voiceCoordinator.Run(ctx)
}

func splitWakeAliases(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	aliases := make([]string, 0, len(parts))
	for _, part := range parts {
		if alias := strings.TrimSpace(part); alias != "" {
			aliases = append(aliases, alias)
		}
	}
	return aliases
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
