package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/console"
	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/stt"
	"github.com/dfc-coder/xarlatan/internal/tools"
	"github.com/dfc-coder/xarlatan/internal/tts"
)

var (
	cfgPath  = flag.String("config", "config.yaml", "path to config.yaml")
	logLevel = flag.String("log", "info", "log level: debug|info|warn|error")
	noTools  = flag.Bool("no-tools", false, "disable all tools")
	reset    = flag.Bool("reset", false, "reset conversation history on startup")
	version  = flag.Bool("version", false, "print version and exit")
)

const buildVersion = "0.2.0"

func main() {
	flag.Parse()
	if *version {
		fmt.Println("assistant v" + buildVersion)
		os.Exit(0)
	}
	setupLogger(*logLevel)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	transcriber, err := stt.New(cfg.STT, cfg.Audio.SampleRate)
	if err != nil {
		slog.Error("stt", "err", err)
		os.Exit(1)
	}
	defer transcriber.Close()

	llmClient, err := llm.New(cfg.LLM)
	if err != nil {
		slog.Error("llm", "err", err)
		os.Exit(1)
	}
	defer llmClient.Close()

	summary := ""
	history := memory.Compose(cfg.LLM.SystemPrompt, summary, nil)
	var registry *tools.Registry
	var executor *tools.Executor

	if *reset {
		summary = ""
		history = memory.Compose(cfg.LLM.SystemPrompt, summary, nil)
	}
	if !*noTools {
		registry = buildRegistry(cfg)
		executor = tools.NewExecutor(registry)
		slog.Info("tools enabled", "list", registry.Names())
	}

	recorder := audio.NewRecorder(cfg.Audio)
	speaker, err := tts.New(cfg.TTS, cfg.Audio.Device)
	if err != nil {
		slog.Error("tts", "err", err)
		os.Exit(1)
	}
	defer speaker.Close()
	status := console.NewStatusPrinter(os.Stderr)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	status.Set("Escuchando")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		samples, err := recorder.RecordUntilSilence(ctx)
		if err != nil || len(samples) == 0 {
			if ctx.Err() != nil {
				return
			}
			status.Set("Escuchando")
			continue
		}

		status.Set("Procesando STT")
		t0 := time.Now()
		text, err := transcriber.Transcribe(samples)
		if err != nil || text == "" {
			status.Set("Escuchando")
			continue
		}
		slog.Debug("stt", "ms", time.Since(t0).Milliseconds(), "text", text)
		fmt.Printf("\n👤  %s\n", text)

		status.Set("Pensando")
		t0 = time.Now()
		reply, toolLog, nextHistory, err := llmClient.Generate(ctx, history, text, registry)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("llm", "err", err)
			status.Set("Escuchando")
			continue
		}
		if toolLog != "" {
			fmt.Printf("   %s\n", toolLog)
		}
		if executor != nil && hasToolCalls(nextHistory) {
			calls := nextHistory[len(nextHistory)-1].ToolCalls
			toolMsgs, execLog := executor.RunAll(ctx, calls)
			if execLog != "" {
				fmt.Printf("   %s\n", execLog)
			}
			nextHistory = append(nextHistory, toLLMMessages(toolMsgs)...)
			reply, toolLog, nextHistory, err = llmClient.Generate(ctx, nextHistory, "", registry)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("llm", "err", err)
				status.Set("Escuchando")
				continue
			}
			if toolLog != "" {
				fmt.Printf("   %s\n", toolLog)
			}
		}

		snap := memory.Compact(nextHistory, 20)
		slog.Debug(
			"memory compact",
			"input_messages",
			snap.Trace.InputMessages,
			"dropped_messages",
			snap.Trace.DroppedMessages,
			"window_messages",
			snap.Trace.WindowMessages,
			"summary_generated",
			snap.Trace.SummaryGenerated,
		)
		summary = memory.MergeSummary(summary, snap.Summary)
		history = memory.Compose(cfg.LLM.SystemPrompt, summary, snap.Window)
		slog.Debug("llm", "ms", time.Since(t0).Milliseconds())
		fmt.Printf("🤖  %s\n", reply)

		status.Set("Hablando")
		if err := speaker.Speak(ctx, reply); err != nil {
			slog.Warn("tts", "err", err)
		}
		status.Set("Escuchando")
	}
}

func hasToolCalls(history []llm.Message) bool {
	if len(history) == 0 {
		return false
	}
	return len(history[len(history)-1].ToolCalls) > 0
}

func toLLMMessages(messages []tools.ToolMessage) []llm.Message {
	converted := make([]llm.Message, len(messages))
	for i, msg := range messages {
		converted[i] = llm.Message{
			Role:       msg.Role,
			ToolCallID: msg.ToolCallID,
			Content:    msg.Content,
		}
	}
	return converted
}

// buildRegistry registers all tools. One line per tool — easy to add/remove.
func buildRegistry(cfg *config.Config) *tools.Registry {
	r := tools.NewRegistry()
	fs := cfg.Tools.FSRoot
	// Filesystem
	r.Register(tools.FSRead{RootDir: fs})
	r.Register(tools.FSWrite{RootDir: fs})
	r.Register(tools.FSList{RootDir: fs})
	r.Register(tools.FSDelete{RootDir: fs})
	r.Register(tools.FSStat{RootDir: fs})
	r.Register(tools.FSMkdir{RootDir: fs})
	// Web — WebSearchConfig is shared between config and tools packages
	ws := cfg.Tools.WebSearch
	r.Register(&tools.WebSearch{Provider: ws.Provider, APIKey: ws.APIKey, BaseURL: ws.BaseURL})
	r.Register(tools.WebFetch{})
	return r
}

func setupLogger(level string) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}
