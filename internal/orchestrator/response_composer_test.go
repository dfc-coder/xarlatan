package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestResponseComposer_RunBuildsDraftAndRoutesToFinalizer(t *testing.T) {
	tests := []struct {
		name      string
		state     State
		wantDraft string
	}{
		{
			name:      "clarify intent",
			state:     State{Intent: "clarify", Input: "hazlo"},
			wantDraft: "Necesito un poco mas de contexto para responder bien.",
		},
		{
			name:      "tool results",
			state:     State{ToolResults: []string{"resultado 1", "resultado 2"}},
			wantDraft: "resultado 1\nresultado 2",
		},
		{
			name:      "input fallback",
			state:     State{Input: "dime la hora"},
			wantDraft: "Entendido: dime la hora",
		},
	}

	node := ResponseComposer{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, next, err := node.Run(context.Background(), tc.state)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := state.DraftResponse; got != tc.wantDraft {
				t.Fatalf("DraftResponse = %q, want %q", got, tc.wantDraft)
			}
			if got := next; got != "finalizer" {
				t.Fatalf("next = %q, want finalizer", got)
			}
			if got := state.NextNode; got != "finalizer" {
				t.Fatalf("state.NextNode = %q, want finalizer", got)
			}
		})
	}
}

func TestResponseComposer_RunRoutesToToolExecutorWhenToolsArePending(t *testing.T) {
	node := ResponseComposer{}
	state, next, err := node.Run(context.Background(), State{
		ToolCalls: []tools.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: tools.CallFunction{
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hola"}`),
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := next, "tool_executor"; got != want {
		t.Fatalf("next = %q, want %q", got, want)
	}
	if got, want := state.NextNode, "tool_executor"; got != want {
		t.Fatalf("state.NextNode = %q, want %q", got, want)
	}
}
