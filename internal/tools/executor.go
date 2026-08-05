package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// ─── Wire types (OpenAI function-calling protocol) ────────────────────────────

// ToolCall is a single function invocation requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function CallFunction `json:"function"`
}

// CallFunction holds the name and JSON-encoded arguments.
type CallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolMessage is a "tool" role message appended after execution.
type ToolMessage struct {
	Role       string `json:"role"` // "tool"
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// ─── Executor ─────────────────────────────────────────────────────────────────

// Executor dispatches tool calls from the LLM to the registry.
type Executor struct {
	registry *Registry
}

// NewExecutor creates an Executor backed by the given registry.
func NewExecutor(r *Registry) *Executor {
	return &Executor{registry: r}
}

// RunAll executes every ToolCall concurrently and returns the ToolMessages
// to be appended to the conversation, along with a human-readable summary.
func (e *Executor) RunAll(ctx context.Context, calls []ToolCall) ([]ToolMessage, string) {
	type work struct {
		idx int
		msg ToolMessage
		log string
	}

	results := make([]work, len(calls))
	// Run sequentially to keep things simple and predictable on low-end hardware.
	// Switch to goroutines if latency becomes a problem.
	for i, call := range calls {
		msg, logLine := e.run(ctx, call)
		results[i] = work{i, msg, logLine}
	}

	msgs := make([]ToolMessage, len(calls))
	var logLines []string
	for _, r := range results {
		msgs[r.idx] = r.msg
		logLines = append(logLines, r.log)
	}
	return msgs, strings.Join(logLines, "\n")
}

func (e *Executor) run(ctx context.Context, call ToolCall) (ToolMessage, string) {
	if e == nil || e.registry == nil {
		content := fmt.Sprintf("ERROR: unknown tool %q", call.Function.Name)
		return ToolMessage{Role: "tool", ToolCallID: call.ID, Content: content},
			fmt.Sprintf("✗ %s — not found", call.Function.Name)
	}

	tool, ok := e.registry.Get(call.Function.Name)
	if !ok {
		content := fmt.Sprintf("ERROR: unknown tool %q", call.Function.Name)
		slog.Warn("Unknown tool called", "name", call.Function.Name)
		return ToolMessage{Role: "tool", ToolCallID: call.ID, Content: content},
			fmt.Sprintf("✗ %s — not found", call.Function.Name)
	}

	slog.Debug("Executing tool", "name", call.Function.Name, "args", string(call.Function.Arguments))

	result := tool.Execute(ctx, call.Function.Arguments)

	prefix := "✓"
	if result.IsError {
		prefix = "✗"
		slog.Warn("Tool returned error", "name", call.Function.Name, "content", result.Content)
	} else {
		slog.Debug("Tool result", "name", call.Function.Name, "bytes", len(result.Content))
	}

	return ToolMessage{Role: "tool", ToolCallID: call.ID, Content: result.Content},
		fmt.Sprintf("%s %s", prefix, call.Function.Name)
}

// FormatToolCalls renders tool calls as a readable line for the terminal.
func FormatToolCalls(calls []ToolCall) string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Function.Name
	}
	return "🔧 " + strings.Join(names, " → ")
}
