package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestBuildRegistryDefaultExposesOnlyWebTools(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{WebSearch: config.WebSearchConfig{Provider: "duckduckgo"}}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestBuildRegistryReadOnlyFilesystem(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{
		Filesystem: config.FilesystemConfig{Enabled: true, Root: t.TempDir()},
		WebSearch:  config.WebSearchConfig{Provider: "duckduckgo"},
	}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"fs_read", "fs_list", "fs_stat", "web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestBuildRegistryAllowsMutationsOnlyWhenExplicit(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{
		Filesystem: config.FilesystemConfig{Enabled: true, Root: t.TempDir(), AllowMutations: true},
		WebSearch:  config.WebSearchConfig{Provider: "duckduckgo"},
	}}
	registry, err := buildRegistry(cfg)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	want := []string{"fs_read", "fs_write", "fs_list", "fs_delete", "fs_stat", "fs_mkdir", "web_search", "web_fetch"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestApplicationUsesOrchestratorOnly(t *testing.T) {
	source := readMainSource(t)
	for _, forbidden := range []string{
		"llmClient.Generate(",
		"executor.RunAll(",
		"RunAllDetailed(",
		"hasToolCalls(",
		"toLLMMessages(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("main.go contains second agent-loop primitive %q", forbidden)
		}
	}
	if got, want := strings.Count(source, "agent.Run("), 1; got != want {
		t.Fatalf("agent.Run calls = %d, want %d", got, want)
	}
	if !strings.Contains(source, "orchestrator.NewAgentRuntime(") {
		t.Fatal("main.go does not construct AgentRuntime")
	}
}

func TestApplicationOwnsLLMServerLifecycle(t *testing.T) {
	source := readMainSource(t)
	for _, required := range []string{
		"func run() error",
		"llm.NewServerManager(",
		"serverManager.Start(ctx)",
		"serverManager.WaitReady(ctx)",
		"serverManager.Stop(context.Background())",
		"llm.NewClient(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("main.go missing lifecycle primitive %q", required)
		}
	}
	for _, forbidden := range []string{
		"llm.New(",
		"llmClient.Close(",
		"llmClient.startServer(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("main.go contains implicit lifecycle primitive %q", forbidden)
		}
	}
	if got := strings.Count(source, "os.Exit("); got != 1 {
		t.Fatalf("os.Exit calls = %d, want 1 only after run returns", got)
	}
}

func readMainSource(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	mainPath := filepath.Join(filepath.Dir(currentFile), "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	return string(content)
}
