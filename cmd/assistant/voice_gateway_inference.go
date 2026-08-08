package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/inference"
)

func newVoiceGatewayInference(ctx context.Context, cfg *config.Config) (*voiceInferenceRuntime, error) {
	sttWorker, err := inference.StartWorker(ctx, inference.WorkerConfig{
		Python:          cfg.Voice.Worker.Python,
		Script:          cfg.Voice.Worker.Script,
		Mode:            "stt",
		ModelDir:        cfg.Voice.STT.ModelDir,
		Language:        cfg.Voice.STT.Language,
		RequestedDevice: cfg.Voice.STT.Device,
		FallbackDevice:  cfg.Voice.STT.FallbackDevice,
		CacheDir:        cfg.Voice.STT.CacheDir,
		ShutdownTimeout: 2 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("OpenVINO STT worker: %w", err)
	}
	sttHealth, err := sttWorker.Health(ctx)
	if err != nil {
		_ = sttWorker.Close()
		return nil, fmt.Errorf("OpenVINO STT health: %w", err)
	}
	slog.Info("voice STT ready", "requested_device", sttHealth.RequestedDevice, "device", sttHealth.ResolvedDevice, "fallback", sttHealth.FallbackReason)

	ttsWorker, err := inference.StartWorker(ctx, inference.WorkerConfig{
		Python:          cfg.Voice.Worker.Python,
		Script:          cfg.Voice.Worker.Script,
		Mode:            "tts",
		ModelDir:        cfg.Voice.TTS.ModelDir,
		VoiceFile:       cfg.Voice.TTS.VoiceFile,
		Language:        cfg.Voice.TTS.Language,
		RequestedDevice: cfg.Voice.TTS.Device,
		FallbackDevice:  cfg.Voice.TTS.FallbackDevice,
		CacheDir:        cfg.Voice.TTS.CacheDir,
		ShutdownTimeout: 2 * time.Second,
	})
	if err != nil {
		_ = sttWorker.Close()
		return nil, fmt.Errorf("Kokoro TTS worker: %w", err)
	}
	ttsHealth, err := ttsWorker.Health(ctx)
	if err != nil {
		_ = ttsWorker.Close()
		_ = sttWorker.Close()
		return nil, fmt.Errorf("Kokoro TTS health: %w", err)
	}
	slog.Info("voice TTS ready", "requested_device", ttsHealth.RequestedDevice, "device", ttsHealth.ResolvedDevice, "fallback", ttsHealth.FallbackReason)

	return &voiceInferenceRuntime{
		transcriber: sttWorker,
		synthesizer: ttsWorker,
		close: func() error {
			return errors.Join(ttsWorker.Close(), sttWorker.Close())
		},
	}, nil
}
