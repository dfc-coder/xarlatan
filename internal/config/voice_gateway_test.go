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
	configureVoiceGatewayFixture(t, &cfg)
	cfg.LLM = LLMConfig{}
	cfg.Tools.WebSearch.Provider = "not-a-local-provider"
	cfg.Tools.Filesystem = FilesystemConfig{Enabled: true, AllowMutations: true}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v; voice_gateway must not validate local LLM/tools", err)
	}
}

func TestValidateVoiceGatewayRequiresACPExecutableAndWorkspace(t *testing.T) {
	cfg := operationalConfig(t)
	root := t.TempDir()
	cfg.Agent = AgentConfig{Mode: "voice_gateway", ACP: ACPConfig{
		Binary: filepath.Join(root, "missing"), Args: []string{"acp"}, CWD: root,
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.acp.binary") {
		t.Fatalf("Validate() error = %v, want agent.acp.binary error", err)
	}

	binary := filepath.Join(root, "agent-acp")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.Agent.ACP.Binary = binary
	cfg.Agent.ACP.CWD = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.acp.cwd is required") {
		t.Fatalf("Validate() error = %v, want required cwd", err)
	}
	cfg.Agent.ACP.CWD = "relative"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Validate() error = %v, want absolute cwd", err)
	}
	cfg.Agent.ACP.CWD = root
	cfg.Agent.ACP.Args = []string{"acp", " "}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.acp.args[1]") {
		t.Fatalf("Validate() error = %v, want blank arg error", err)
	}
}

func TestLoadRejectsLegacyZeroClawSchema(t *testing.T) {
	path := writeTestConfig(t, `
audio: {}
agent:
  mode: voice_gateway
  zeroclaw:
    binary: /usr/local/bin/zeroclaw
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field zeroclaw not found") {
		t.Fatalf("Load() error = %v, want strict legacy-schema rejection", err)
	}
}

func TestValidateRejectsUnknownAgentMode(t *testing.T) {
	cfg := operationalConfig(t)
	cfg.Agent.Mode = "other"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "agent.mode") {
		t.Fatalf("Validate() error = %v, want agent.mode error", err)
	}
}
