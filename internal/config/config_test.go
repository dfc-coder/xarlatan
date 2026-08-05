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

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
