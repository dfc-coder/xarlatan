package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVoiceGatewayRuntimeIsolatedFromLocalAgentStack(t *testing.T) {
	source := readCompositionSource(t, "voice_gateway_runtime.go")
	for _, required := range []string{
		"acp.StartRuntime(",
		"runtime.BudgetedResponder(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("voice gateway runtime missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"internal/zeroclaw",
		"zeroclaw.StartRuntime(",
		"llm.NewServerManager(",
		"llm.NewClient(",
		"memory.New(",
		"tools.NewExecutor(",
		"orchestrator.NewAgentRuntime(",
		"conversation.New(",
		"buildRegistry(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("voice gateway runtime contains local-agent primitive %q", forbidden)
		}
	}
}

func TestResponseRuntimeSelectsVoiceGatewayOrLegacyNative(t *testing.T) {
	source := readCompositionSource(t, "response_runtime.go")
	for _, required := range []string{
		`case "voice_gateway":`,
		"newVoiceGatewayRuntime(",
		`case "legacy_native":`,
		"newLegacyAgentRuntime(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("response runtime missing %q", required)
		}
	}
}

func TestMainUsesResponseRuntimeBoundary(t *testing.T) {
	source := readMainSource(t)
	if !strings.Contains(source, "newResponseRuntime(") {
		t.Fatal("main.go must select the agent plane through newResponseRuntime")
	}
}

func readCompositionSource(t *testing.T, name string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}
