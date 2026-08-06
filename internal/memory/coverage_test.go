package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		config Config
		prompt string
		code   ErrorCode
	}{
		{name: "history zero", config: Config{MaxSummaryBytes: 1}, prompt: "system", code: ErrorInvalidConfig},
		{name: "summary zero", config: Config{MaxHistoryBytes: 10}, prompt: "system", code: ErrorInvalidConfig},
		{name: "summary equals history", config: Config{MaxHistoryBytes: 10, MaxSummaryBytes: 10}, prompt: "system", code: ErrorInvalidConfig},
		{name: "system exceeds budget", config: Config{MaxHistoryBytes: 8, MaxSummaryBytes: 1}, prompt: strings.Repeat("x", 40), code: ErrorBudgetExceeded},
		{name: "summary reserve exceeds budget", config: Config{MaxHistoryBytes: 60, MaxSummaryBytes: 40}, prompt: "system", code: ErrorInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var summarizer Summarizer
			if tc.name == "summary reserve exceeds budget" {
				summarizer = ExtractiveSummarizer{}
			}
			_, err := New(tc.config, tc.prompt, summarizer)
			if !IsErrorCode(err, tc.code) {
				t.Fatalf("New() error = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestManagerResetAndNilSafety(t *testing.T) {
	var nilManager *Manager
	if got := nilManager.History(); got != nil {
		t.Fatalf("nil History() = %v, want nil", got)
	}
	nilManager.Reset()
	if _, err := nilManager.Update(context.Background(), nil); !IsErrorCode(err, ErrorInvalidConfig) {
		t.Fatalf("nil Update() error = %v, want %s", err, ErrorInvalidConfig)
	}

	manager := newTestManager(t, Config{MaxHistoryBytes: 512, MaxSummaryBytes: 64}, nil)
	history := append(manager.History(),
		llm.Message{Role: "user", Content: "question"},
		llm.Message{Role: "assistant", Content: "answer"},
	)
	if _, err := manager.Update(context.Background(), history); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	manager.Reset()
	got := manager.History()
	if len(got) != 1 || got[0].Role != "system" {
		t.Fatalf("History() after Reset = %+v", got)
	}
}

func TestExtractiveSummarizerIsBoundedAndCancelable(t *testing.T) {
	summarizer := ExtractiveSummarizer{}
	dropped := []llm.Message{
		{Role: "user", Content: "find the file"},
		{Role: "assistant", ToolCalls: []tools.ToolCall{
			{ID: "call-1", Type: "function", Function: tools.CallFunction{Name: "fs_read", Arguments: json.RawMessage("{}")}},
		}},
		{Role: "tool", ToolCallID: "call-1", Content: strings.Repeat("result ", 20)},
	}
	result, err := summarizer.Summarize(context.Background(), "older context", dropped, 72)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if len([]byte(result)) > 72 {
		t.Fatalf("summary bytes = %d, want <= 72", len([]byte(result)))
	}
	if !strings.Contains(result, "previous") || !strings.Contains(result, "user") {
		t.Fatalf("summary = %q, want previous and dropped context", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := summarizer.Summarize(ctx, "", dropped, 72); err == nil {
		t.Fatal("Summarize() error = nil after cancellation")
	}
}

func TestMemoryRejectsMalformedHistories(t *testing.T) {
	call := tools.ToolCall{ID: "call-1", Type: "function", Function: tools.CallFunction{Name: "noop", Arguments: json.RawMessage("{}")}}
	cases := []struct {
		name         string
		conversation []llm.Message
	}{
		{name: "empty history", conversation: nil},
		{name: "changed system", conversation: []llm.Message{{Role: "system", Content: "changed"}}},
		{name: "assistant before user", conversation: []llm.Message{{Role: "assistant", Content: "answer"}}},
		{name: "unsupported role", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "developer", Content: "x"}}},
		{name: "user tool fields", conversation: []llm.Message{{Role: "user", Content: "q", ToolCallID: "bad"}}},
		{name: "assistant tool id", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCallID: "bad"}}},
		{name: "empty call id", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{{Function: call.Function}}}}},
		{name: "duplicate call id", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call, call}}}},
		{name: "missing tool result", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call}}}},
		{name: "assistant before results", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call}}, {Role: "assistant", Content: "too early"}}},
		{name: "tool before user", conversation: []llm.Message{{Role: "tool", ToolCallID: "call-1", Content: "x"}}},
		{name: "tool contains calls", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call}}, {Role: "tool", ToolCallID: "call-1", ToolCalls: []tools.ToolCall{call}}}},
		{name: "tool empty id", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call}}, {Role: "tool", Content: "x"}}},
		{name: "tool unmatched id", conversation: []llm.Message{{Role: "user", Content: "q"}, {Role: "assistant", ToolCalls: []tools.ToolCall{call}}, {Role: "tool", ToolCallID: "other", Content: "x"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager := newTestManager(t, Config{MaxHistoryBytes: 2048, MaxSummaryBytes: 128}, nil)
			history := tc.conversation
			if tc.name != "empty history" && (len(history) == 0 || history[0].Role != "system") {
				history = append(manager.History(), history...)
			}
			_, err := manager.Update(context.Background(), history)
			if !IsErrorCode(err, ErrorInvalidHistory) {
				t.Fatalf("Update() error = %v, want %s", err, ErrorInvalidHistory)
			}
		})
	}
}

func TestMemoryTruncatesOversizedUTF8Summary(t *testing.T) {
	summarizer := &scriptedSummarizer{outputs: []string{strings.Repeat("á", 100)}}
	manager := newTestManager(t, Config{MaxHistoryBytes: 320, MaxSummaryBytes: 31}, summarizer)
	history := append(manager.History(),
		llm.Message{Role: "user", Content: strings.Repeat("large ", 50)},
		llm.Message{Role: "assistant", Content: strings.Repeat("answer ", 50)},
	)
	snapshot, err := manager.Update(context.Background(), history)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len([]byte(snapshot.Summary)) > 31 {
		t.Fatalf("summary bytes = %d, want <= 31", len([]byte(snapshot.Summary)))
	}
	if !json.Valid([]byte(`"` + snapshot.Summary + `"`)) {
		t.Fatalf("summary is not valid UTF-8 JSON string: %q", snapshot.Summary)
	}
}
