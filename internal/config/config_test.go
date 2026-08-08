package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, `
llm:
  temperatur: 0.2
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown YAML field error")
	}
	if !strings.Contains(err.Error(), "temperatur") {
		t.Fatalf("Load() error = %q, want field name %q", err, "temperatur")
	}
}

func TestLoadPreservesExplicitZeroTemperature(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, `
llm:
  temperature: 0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Temperature != 0 {
		t.Fatalf("LLM.Temperature = %v, want explicit zero", cfg.LLM.Temperature)
	}
}

func TestLoadAppliesDefaultTemperatureWhenOmitted(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, "llm: {}")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Fatalf("LLM.Temperature = %v, want 0.7", cfg.LLM.Temperature)
	}
}

func TestLoadAppliesDefaultFilesystemLimits(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, "tools:\n  filesystem: {}")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tools.Filesystem.MaxReadBytes != 1<<20 {
		t.Fatalf("MaxReadBytes = %d, want %d", cfg.Tools.Filesystem.MaxReadBytes, 1<<20)
	}
	if cfg.Tools.Filesystem.MaxWriteBytes != 1<<20 {
		t.Fatalf("MaxWriteBytes = %d, want %d", cfg.Tools.Filesystem.MaxWriteBytes, 1<<20)
	}
}

func TestLoadPreservesExplicitFilesystemLimits(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, `
tools:
  filesystem:
    max_read_bytes: 4096
    max_write_bytes: 8192
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tools.Filesystem.MaxReadBytes != 4096 || cfg.Tools.Filesystem.MaxWriteBytes != 8192 {
		t.Fatalf("filesystem limits = %d/%d, want 4096/8192", cfg.Tools.Filesystem.MaxReadBytes, cfg.Tools.Filesystem.MaxWriteBytes)
	}
}

func TestValidateRejectsInvalidLLMPort(t *testing.T) {
	cfg := Config{
		Audio: AudioConfig{SampleRate: 16000, Channels: 1, SilenceDurationMS: 1, MaxDurationS: 1, Device: "default"},
		LLM: LLMConfig{
			Mode:              "external",
			Host:              "127.0.0.1",
			Port:              70000,
			ContextSize:       4096,
			Threads:           4,
			Temperature:       0.7,
			TopP:              0.9,
			MaxTokens:         512,
			StartupTimeoutMS:  1000,
			ShutdownTimeoutMS: 1000,
			HealthIntervalMS:  100,
		},
		Log: LogConfig{Level: "info"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "llm.port") {
		t.Fatalf("Validate() error = %v, want llm.port error", err)
	}
}

func TestValidateRejectsNonMonoAudio(t *testing.T) {
	cfg := Config{Audio: AudioConfig{SampleRate: 16000, Channels: 2}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "audio.channels") {
		t.Fatalf("Validate() error = %v, want audio.channels error", err)
	}
}

func TestValidateRequiresAbsoluteFSRootWhenFilesystemEnabled(t *testing.T) {
	cfg := structurallyValidConfig()
	cfg.Tools.Filesystem = FilesystemConfig{Enabled: true, Root: "relative/path", MaxReadBytes: 1, MaxWriteBytes: 1}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Validate() error = %v, want absolute root error", err)
	}
}

func TestValidateRejectsNonPositiveFilesystemLimits(t *testing.T) {
	cfg := operationalConfig(t)
	cfg.Tools.Filesystem.MaxReadBytes = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_read_bytes") {
		t.Fatalf("Validate() error = %v, want max_read_bytes error", err)
	}
	cfg = operationalConfig(t)
	cfg.Tools.Filesystem.MaxWriteBytes = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_write_bytes") {
		t.Fatalf("Validate() error = %v, want max_write_bytes error", err)
	}
}

func TestValidateRejectsMissingRuntimeFiles(t *testing.T) {
	cfg := structurallyValidConfig()
	cfg.STT.Encoder = filepath.Join(t.TempDir(), "missing.onnx")
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "stt.encoder") {
		t.Fatalf("Validate() error = %v, want stt.encoder error", err)
	}
}

func TestValidateAcceptsOperationalConfig(t *testing.T) {
	cfg := operationalConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownWebProvider(t *testing.T) {
	cfg := structurallyValidConfig()
	cfg.Tools.WebSearch.Provider = "unknown"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("Validate() error = %v, want provider error", err)
	}
}

func structurallyValidConfig() Config {
	return Config{
		Audio: AudioConfig{SampleRate: 16000, Channels: 1, SilenceThreshold: 0.015, SilenceDurationMS: 1500, MaxDurationS: 30, Device: "default"},
		LLM: LLMConfig{
			Mode:              "external",
			Host:              "127.0.0.1",
			Port:              8080,
			ContextSize:       4096,
			Threads:           4,
			Temperature:       0.7,
			TopP:              0.9,
			MaxTokens:         512,
			StartupTimeoutMS:  60_000,
			ShutdownTimeoutMS: 5_000,
			HealthIntervalMS:  250,
		},
		TTS: TTSConfig{LengthScale: 1, NoiseScale: 0.667, NoiseW: 0.8},
		Tools: ToolsConfig{
			Filesystem: FilesystemConfig{MaxReadBytes: 1 << 20, MaxWriteBytes: 1 << 20},
			WebSearch:  WebSearchConfig{Provider: "duckduckgo"},
		},
		Log: LogConfig{Level: "info"},
	}
}

func operationalConfig(t *testing.T) Config {
	t.Helper()
	cfg := structurallyValidConfig()
	dir := t.TempDir()
	file := func(name string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("test"), mode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	cfg.STT.Encoder = file("encoder.onnx", 0o600)
	cfg.STT.Decoder = file("decoder.onnx", 0o600)
	cfg.STT.Tokens = file("stt-tokens.txt", 0o600)
	cfg.LLM.Model = file("model.gguf", 0o600)
	cfg.LLM.ServerBinary = file("llama-server", 0o700)
	cfg.TTS.Model = file("tts.onnx", 0o600)
	cfg.TTS.Tokens = file("tts-tokens.txt", 0o600)
	cfg.TTS.DataDir = filepath.Join(dir, "espeak")
	if err := os.Mkdir(cfg.TTS.DataDir, 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	cfg.Tools.Filesystem = FilesystemConfig{Enabled: true, Root: dir, MaxReadBytes: 1 << 20, MaxWriteBytes: 1 << 20}
	return cfg
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
