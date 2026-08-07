package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSileroVADConfigInfersStandardModelLayout(t *testing.T) {
	t.Setenv("XARLATAN_VAD_MODEL", "")
	root := t.TempDir()
	encoder := filepath.Join(root, "models", "stt", "whisper", "encoder.onnx")
	vadModel := filepath.Join(root, "models", "vad", "silero_vad.onnx")
	for _, path := range []string{encoder, vadModel} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
			t.Fatalf("write model: %v", err)
		}
	}
	cfg := Config{Audio: AudioConfig{SampleRate: 16000, SilenceDurationMS: 1500, MaxDurationS: 30}, STT: STTConfig{Encoder: encoder}}

	resolved, err := cfg.SileroVADConfig()
	if err != nil {
		t.Fatalf("SileroVADConfig() error = %v", err)
	}
	if resolved.Model != vadModel {
		t.Fatalf("Model = %q, want %q", resolved.Model, vadModel)
	}
	if resolved.Threshold != 0.5 || resolved.WindowSize != 512 || resolved.Provider != "cpu" {
		t.Fatalf("unexpected defaults: %+v", resolved)
	}
	if resolved.MinSilenceDuration != 1500*time.Millisecond || resolved.MaxSpeechDuration != 30*time.Second {
		t.Fatalf("unexpected durations: %+v", resolved)
	}
}

func TestSileroVADConfigAllowsExplicitModelOverride(t *testing.T) {
	model := filepath.Join(t.TempDir(), "custom.onnx")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	t.Setenv("XARLATAN_VAD_MODEL", model)
	cfg := Config{Audio: AudioConfig{SampleRate: 16000, SilenceDurationMS: 500, MaxDurationS: 5}}

	resolved, err := cfg.SileroVADConfig()
	if err != nil {
		t.Fatalf("SileroVADConfig() error = %v", err)
	}
	if resolved.Model != model {
		t.Fatalf("Model = %q, want %q", resolved.Model, model)
	}
}

func TestSileroVADConfigRejectsMissingModelAndUnsupportedRate(t *testing.T) {
	t.Setenv("XARLATAN_VAD_MODEL", filepath.Join(t.TempDir(), "missing.onnx"))
	cfg := Config{Audio: AudioConfig{SampleRate: 16000, SilenceDurationMS: 500, MaxDurationS: 5}}
	if _, err := cfg.SileroVADConfig(); err == nil {
		t.Fatal("missing VAD model error = nil")
	}

	cfg.Audio.SampleRate = 8000
	if _, err := cfg.SileroVADConfig(); err == nil {
		t.Fatal("unsupported sample rate error = nil")
	}
}
