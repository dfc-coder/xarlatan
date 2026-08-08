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

func TestMainConstructsVoicePipelineAroundResponseRuntime(t *testing.T) {
	source := readMainSource(t)
	for _, required := range []string{
		"newResponseRuntime(ctx, cfg)",
		"cfg.SileroVADConfig()",
		"sherpavad.New(",
		"audio.NewContinuousRecorderWithDetector(",
		"recorder.Close()",
		"audio.NewPlayback(",
		"playback.UseContinuousCaptureGuard()",
		"audio.NewResponsePlayback(playback)",
		"newCaptureGateObserver(",
		"wake.NewPhraseDetector(",
		"voiceCoordinator.SetWakeDetector(",
		"barge.NewExplicitStopPolicy(wakeDetector)",
		"application.NewBargeInController(",
		"voiceCoordinator.SetInterruptSource(bargeController)",
		"barge-in",
		"player.Close()",
		"application.NewCoordinator(application.Dependencies{",
		"Responder:   responseRuntime.responder",
		"voiceCoordinator.Run(ctx)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("main.go missing composition primitive %q", required)
		}
	}
	for _, forbidden := range []string{
		"llm.NewServerManager(",
		"llm.NewClient(",
		"memory.New(",
		"orchestrator.NewAgentRuntime(",
		"conversation.New(",
		"buildRegistry(",
		"llmClient.Generate(",
		"agent.Run(",
		"executor.RunAll(",
		"RunAllDetailed(",
		"memoryManager.Prepare(",
		"memoryManager.Update(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("main.go contains agent-plane logic %q", forbidden)
		}
	}
}

func TestSplitWakeAliases(t *testing.T) {
	if got := splitWakeAliases(" charlatan, charlatán ,, xarlatan "); !reflect.DeepEqual(got, []string{"charlatan", "charlatán", "xarlatan"}) {
		t.Fatalf("splitWakeAliases() = %v", got)
	}
	if got := splitWakeAliases("   "); got != nil {
		t.Fatalf("splitWakeAliases(blank) = %v, want nil", got)
	}
}

func TestLegacyRuntimeOwnsLLMServerLifecycle(t *testing.T) {
	source := readCompositionSource(t, "legacy_agent_runtime.go")
	for _, required := range []string{
		"llm.NewServerManager(",
		"serverManager.Start(ctx)",
		"serverManager.WaitReady(ctx)",
		"serverManager.Stop(context.Background())",
		"llm.NewClient(",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("legacy runtime missing lifecycle primitive %q", required)
		}
	}
}

func TestLegacyRuntimeConstructsBoundedMemory(t *testing.T) {
	source := readCompositionSource(t, "legacy_agent_runtime.go")
	for _, required := range []string{
		"memory.New(",
		"memory.ExtractiveSummarizer{}",
		"maxHistoryBytes",
		"maxSummaryBytes",
		"orchestrator.NewAgentRuntime(",
		"conversation.New(memoryManager, agent)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("legacy runtime missing bounded memory primitive %q", required)
		}
	}
}

func TestMainIsCompositionRootOnly(t *testing.T) {
	source := readMainSource(t)
	for _, forbidden := range []string{
		"RecordUntilSilence(",
		".Transcribe(",
		".Respond(",
		".Synthesize(",
		".Speak(",
		".Play(",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("main.go contains voice-stage logic %q", forbidden)
		}
	}
}

func TestMainExitsOnlyAfterRunReturns(t *testing.T) {
	source := readMainSource(t)
	if !strings.Contains(source, "func run() error") {
		t.Fatal("main.go missing run() error boundary")
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
