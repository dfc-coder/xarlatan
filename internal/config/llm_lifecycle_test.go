package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validExternalLLMConfig() LLMConfig {
	return LLMConfig{
		Mode:              "external",
		Host:              "127.0.0.1",
		Port:              8080,
		ContextSize:       4096,
		Threads:           4,
		Temperature:       0.7,
		TopP:              0.9,
		MaxTokens:         64,
		StartupTimeoutMS:  1000,
		ShutdownTimeoutMS: 1000,
		HealthIntervalMS:  100,
	}
}

func TestLoadAppliesLLMLifecycleDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("llm:\n  host: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Mode != "managed" {
		t.Fatalf("llm.mode = %q, want managed", cfg.LLM.Mode)
	}
	if cfg.LLM.StartupTimeoutMS != defaultLLMStartupTimeoutMS {
		t.Fatalf("startup timeout = %d, want %d", cfg.LLM.StartupTimeoutMS, defaultLLMStartupTimeoutMS)
	}
	if cfg.LLM.ShutdownTimeoutMS != defaultLLMShutdownTimeoutMS {
		t.Fatalf("shutdown timeout = %d, want %d", cfg.LLM.ShutdownTimeoutMS, defaultLLMShutdownTimeoutMS)
	}
	if cfg.LLM.HealthIntervalMS != defaultLLMHealthIntervalMS {
		t.Fatalf("health interval = %d, want %d", cfg.LLM.HealthIntervalMS, defaultLLMHealthIntervalMS)
	}
}

func TestValidateLLMExternalModeDoesNotRequireLocalArtifacts(t *testing.T) {
	cfg := validExternalLLMConfig()
	cfg.Model = ""
	cfg.ServerBinary = ""
	if err := validateLLM(cfg); err != nil {
		t.Fatalf("validateLLM() error = %v", err)
	}
}

func TestValidateLLMManagedModeRequiresLocalArtifacts(t *testing.T) {
	cfg := validExternalLLMConfig()
	cfg.Mode = "managed"
	if err := validateLLM(cfg); err == nil || !strings.Contains(err.Error(), "llm.model") {
		t.Fatalf("validateLLM() error = %v, want missing model", err)
	}

	model := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	cfg.Model = model
	if err := validateLLM(cfg); err == nil || !strings.Contains(err.Error(), "llm.server_binary") {
		t.Fatalf("validateLLM() error = %v, want missing binary", err)
	}
}

func TestValidateLLMRejectsInvalidLifecycleSettings(t *testing.T) {
	for name, mutate := range map[string]func(*LLMConfig){
		"mode":             func(cfg *LLMConfig) { cfg.Mode = "automatic" },
		"startup timeout":  func(cfg *LLMConfig) { cfg.StartupTimeoutMS = 0 },
		"shutdown timeout": func(cfg *LLMConfig) { cfg.ShutdownTimeoutMS = 0 },
		"health interval":  func(cfg *LLMConfig) { cfg.HealthIntervalMS = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := validExternalLLMConfig()
			mutate(&cfg)
			if err := validateLLM(cfg); err == nil {
				t.Fatal("validateLLM() error = nil")
			}
		})
	}
}
