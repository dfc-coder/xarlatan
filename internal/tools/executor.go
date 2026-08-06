package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ToolCall is a single function invocation requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function CallFunction `json:"function"`
}

// CallFunction holds the name and JSON-encoded arguments.
type CallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolMessage is a role=tool message appended after execution.
type ToolMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// ExecutionErrorCode classifies a tool execution without parsing log text.
type ExecutionErrorCode string

const (
	ExecutionUnknownTool ExecutionErrorCode = "unknown_tool"
	ExecutionDeniedTool  ExecutionErrorCode = "denied_tool"
	ExecutionToolError   ExecutionErrorCode = "tool_error"
	ExecutionCancelled   ExecutionErrorCode = "cancelled"
)

// ExecutionRecord preserves the call, protocol message, classification and timing.
type ExecutionRecord struct {
	Call      ToolCall
	Message   ToolMessage
	Log       string
	ErrorCode ExecutionErrorCode
	Duration  time.Duration
}

// Success reports whether the tool completed without a classified error.
func (r ExecutionRecord) Success() bool { return r.ErrorCode == "" }

// Executor dispatches tool calls from the LLM to the registry.
type Executor struct {
	registry *Registry
}

// NewExecutor creates an Executor backed by the given registry.
func NewExecutor(r *Registry) *Executor {
	return &Executor{registry: r}
}

// RunAll executes every ToolCall sequentially and preserves the legacy return
// shape used by callers outside the agent runtime.
func (e *Executor) RunAll(ctx context.Context, calls []ToolCall) ([]ToolMessage, string) {
	records := e.RunAllDetailed(ctx, calls)
	messages := make([]ToolMessage, len(records))
	logs := make([]string, len(records))
	for i, record := range records {
		messages[i] = record.Message
		logs[i] = record.Log
	}
	return messages, strings.Join(logs, "\n")
}

// RunAllDetailed executes calls in request order and returns typed records.
// If the context is cancelled, no later call is started.
func (e *Executor) RunAllDetailed(ctx context.Context, calls []ToolCall) []ExecutionRecord {
	records := make([]ExecutionRecord, 0, len(calls))
	for _, call := range calls {
		if ctx.Err() != nil {
			break
		}
		record := e.runDetailed(ctx, call)
		records = append(records, record)
		if ctx.Err() != nil {
			break
		}
	}
	return records
}

func (e *Executor) runDetailed(ctx context.Context, call ToolCall) ExecutionRecord {
	started := time.Now()
	record := ExecutionRecord{Call: call}
	defer func() { record.Duration = time.Since(started) }()

	if e == nil || e.registry == nil {
		content := fmt.Sprintf("ERROR: unknown tool %q", call.Function.Name)
		record.Message = ToolMessage{Role: "tool", ToolCallID: call.ID, Content: content}
		record.Log = fmt.Sprintf("✗ %s — not found", call.Function.Name)
		record.ErrorCode = ExecutionUnknownTool
		return record
	}

	if e.registry.IsDenied(call.Function.Name) {
		content := fmt.Sprintf("ERROR: tool %q denied by policy", call.Function.Name)
		slog.Warn("Tool denied by policy", "name", call.Function.Name)
		record.Message = ToolMessage{Role: "tool", ToolCallID: call.ID, Content: content}
		record.Log = fmt.Sprintf("✗ %s — denied", call.Function.Name)
		record.ErrorCode = ExecutionDeniedTool
		return record
	}

	tool, ok := e.registry.Get(call.Function.Name)
	if !ok {
		content := fmt.Sprintf("ERROR: unknown tool %q", call.Function.Name)
		slog.Warn("Unknown tool called", "name", call.Function.Name)
		record.Message = ToolMessage{Role: "tool", ToolCallID: call.ID, Content: content}
		record.Log = fmt.Sprintf("✗ %s — not found", call.Function.Name)
		record.ErrorCode = ExecutionUnknownTool
		return record
	}

	slog.Debug("Executing tool", "name", call.Function.Name, "args", string(call.Function.Arguments))
	result := tool.Execute(ctx, call.Function.Arguments)
	record.Message = ToolMessage{Role: "tool", ToolCallID: call.ID, Content: result.Content}

	if ctx.Err() != nil {
		record.ErrorCode = ExecutionCancelled
		record.Log = fmt.Sprintf("✗ %s — cancelled", call.Function.Name)
		return record
	}
	if result.IsError {
		record.ErrorCode = ExecutionToolError
		record.Log = fmt.Sprintf("✗ %s", call.Function.Name)
		slog.Warn("Tool returned error", "name", call.Function.Name, "content", result.Content)
		return record
	}

	record.Log = fmt.Sprintf("✓ %s", call.Function.Name)
	slog.Debug("Tool result", "name", call.Function.Name, "bytes", len(result.Content))
	return record
}

// FormatToolCalls renders tool calls as a readable line for the terminal.
func FormatToolCalls(calls []ToolCall) string {
	names := make([]string, len(calls))
	for i, call := range calls {
		names[i] = call.Function.Name
	}
	return "🔧 " + strings.Join(names, " → ")
}
