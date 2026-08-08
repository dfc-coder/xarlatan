package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAgentModeToLegacyNative(t *testing.T) {
	t.Setenv("ASSISTANT_CONFIG", "")
	path := writeTestConfig(t, "audio: {}")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Agent.Mode != "legacy_native" {
		t.Fatalf("Agent.Mode = %q, want legacy_native", cfg.Agent.Mode)
	}
}

func TestValidateVoiceGatewayDoesNotRequireLocalAgentStack(t *testing.T) {
	cfg := operationalConfig(t)
	binary := filepath.Join(t.TempDir(), "zeroclaw")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write zeroclaw: %v", err)
	}
	cfg.Agent = AgentConfig{
		Mode: "voice_gateway",
		ZeroClaw: ZeroClawConfig{
			Binary:     binary,
			AgentAlias: "xarlatan",
		},
	}
	cfg.LLM = LLMConfig{}
	cfg.Tools.WebSearch.Provider = "not-a-local-provider"
	cfg.Tools.Filesystem = FilesystemConfig{Enabled: true, AllowMutations: true}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v; voice_gateway must not validate local LLM/tools", err)
	}
}

func TestValidateVoiceGatewayRequiresZeroClawExecutable(t *testing.T) {
	cfg := operationalConfig(t)
	cfg.Agent = AgentConfig{Mode: "voice_gateway", ZeroClaw: ZeroClawConfig{Binary: filepath.Join(t.TempDir(), "missing")}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.zeroclaw.binary") {
		t.Fatalf("Validate() error = %v, want agent.zeroclaw.binary error", err)
	}
}

func TestValidateRejectsUnknownAgentMode(t *testing.T) {
	cfg := operationalConfig(t)
	cfg.Agent.Mode = "other"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.mode") {
		t.Fatalf("Validate() error = %v, want agent.mode error", err)
	}
}
