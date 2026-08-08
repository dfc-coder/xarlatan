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

	voiceInference, err := newVoiceInferenceRuntime(ctx, cfg)
	if err != nil {
		return fmt.Errorf("voice inference runtime: %w", err)
	}
	defer func() {
		if err := voiceInference.Close(); err != nil {
			slog.Warn("voice inference close", "err", err)
		}
	}()

	responseRuntime, err := newResponseRuntime(ctx, cfg)
	if err != nil {
		return fmt.Errorf("response runtime: %w", err)
	}
	defer func() {
		if err := responseRuntime.Close(); err != nil {
			slog.Warn("response runtime close", "err", err)
		}
	}()

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

	transcriptBridge, err := application.NewTranscriptBridge(
		recorder,
		voiceInference.transcriber,
		newConsoleTranscriptObserver(os.Stdout),
	)
	if err != nil {
		return fmt.Errorf("transcript bridge: %w", err)
	}

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
	observer := newFanoutObserver(
		newCaptureGateObserver(newConsoleObserver(status), recorder),
		transcriptBridge,
	)

	voiceCoordinator, err := application.NewCoordinator(application.Dependencies{
		Input:       recorder,
		Transcriber: transcriptBridge,
		Responder:   responseRuntime.responder,
		Synthesizer: voiceInference.synthesizer,
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
			voiceInference.transcriber,
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
