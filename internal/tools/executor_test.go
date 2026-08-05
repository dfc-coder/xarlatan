package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExecutor_RunAllWithoutRegistryReturnsErrors(t *testing.T) {
	e := NewExecutor(nil)
	msgs, log := e.RunAll(context.Background(), []ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: CallFunction{
			Name:      "missing",
			Arguments: json.RawMessage(`{}`),
		},
	}})

	if got, want := len(msgs), 1; got != want {
		t.Fatalf("len(msgs) = %d, want %d", got, want)
	}
	if got, want := msgs[0].Content, "ERROR: unknown tool \"missing\""; got != want {
		t.Fatalf("msg content = %q, want %q", got, want)
	}
	if got, want := log, "✗ missing — not found"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}
