package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echoes the input" }
func (echoTool) Schema() tools.ParameterSchema {
	return tools.NewSchema([]string{"text"}, map[string]tools.Property{
		"text": {Type: "string", Description: "text to echo"},
	})
}
func (echoTool) Execute(_ context.Context, args json.RawMessage) tools.Result {
	return tools.Result{Content: string(args)}
}

func TestToolExecutor_RunExecutesRealTool(t *testing.T) {
	r := tools.NewRegistry(tools.AllowAllToolPolicy())
	r.Register(echoTool{})
	observer := &spyObserver{}
	node := ToolExecutor{Executor: tools.NewExecutor(r), Observer: observer}

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
	if got, want := next, "response_composer"; got != want {
		t.Fatalf("next = %q, want %q", got, want)
	}
	if got := len(state.ToolCalls); got != 0 {
		t.Fatalf("ToolCalls len = %d, want 0", got)
	}
	if got, want := len(state.ToolResults), 1; got != want {
		t.Fatalf("ToolResults len = %d, want %d", got, want)
	}
	if got, want := state.ToolResults[0], `{"text":"hola"}`; got != want {
		t.Fatalf("ToolResults[0] = %q, want %q", got, want)
	}
	if got, want := state.NextNode, "response_composer"; got != want {
		t.Fatalf("NextNode = %q, want %q", got, want)
	}
	if got, want := len(observer.tools), 1; got != want {
		t.Fatalf("tool trace count = %d, want %d", got, want)
	}
	if !observer.tools[0].Success {
		t.Fatalf("tool trace success = false, want true")
	}
}

func TestToolExecutor_RunWithoutExecutorReturnsToolErrors(t *testing.T) {
	observer := &spyObserver{}
	node := ToolExecutor{Observer: observer}

	state, next, err := node.Run(context.Background(), State{
		ToolCalls: []tools.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: tools.CallFunction{
				Name:      "missing",
				Arguments: json.RawMessage(`{}`),
			},
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := next, "response_composer"; got != want {
		t.Fatalf("next = %q, want %q", got, want)
	}
	if got := len(state.ToolCalls); got != 0 {
		t.Fatalf("ToolCalls len = %d, want 0", got)
	}
	if got, want := len(state.ToolResults), 1; got != want {
		t.Fatalf("ToolResults len = %d, want %d", got, want)
	}
	if got, want := state.ToolResults[0], `ERROR: unknown tool "missing"`; got != want {
		t.Fatalf("ToolResults[0] = %q, want %q", got, want)
	}
	if got, want := len(observer.tools), 1; got != want {
		t.Fatalf("tool trace count = %d, want %d", got, want)
	}
	if observer.tools[0].Success {
		t.Fatalf("tool trace success = true, want false")
	}
	if got, want := observer.tools[0].Error, `ERROR: unknown tool "missing"`; got != want {
		t.Fatalf("tool trace error = %q, want %q", got, want)
	}
}
