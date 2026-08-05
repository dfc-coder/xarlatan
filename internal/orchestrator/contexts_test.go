package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestStateContextProjection_MapsExpectedFields(t *testing.T) {
	state := State{
		Input:         "entrada",
		Intent:        "clarify",
		Summary:       "resumen",
		ToolCalls:     []tools.ToolCall{{ID: "call-1", Function: tools.CallFunction{Name: "echo", Arguments: json.RawMessage(`{"text":"hola"}`)}}},
		ToolResults:   []string{"ok"},
		DraftResponse: "borrador",
		FinalResponse: "final",
	}

	routerCtx := state.RouterContext()
	if got, want := routerCtx.Input, "entrada"; got != want {
		t.Fatalf("RouterContext.Input = %q, want %q", got, want)
	}
	if got, want := routerCtx.Intent, "clarify"; got != want {
		t.Fatalf("RouterContext.Intent = %q, want %q", got, want)
	}

	plannerCtx := state.PlannerContext()
	if got, want := plannerCtx.Input, "entrada"; got != want {
		t.Fatalf("PlannerContext.Input = %q, want %q", got, want)
	}

	composerCtx := state.ResponseComposerContext()
	if got, want := composerCtx.Summary, "resumen"; got != want {
		t.Fatalf("ResponseComposerContext.Summary = %q, want %q", got, want)
	}
	if got, want := composerCtx.DraftResponse, "borrador"; got != want {
		t.Fatalf("ResponseComposerContext.DraftResponse = %q, want %q", got, want)
	}

	finalCtx := state.FinalizerContext()
	if got, want := finalCtx.FinalResponse, "final"; got != want {
		t.Fatalf("FinalizerContext.FinalResponse = %q, want %q", got, want)
	}
}

func TestStateContextProjection_ClonesSliceFields(t *testing.T) {
	state := State{
		ToolCalls: []tools.ToolCall{{
			ID: "call-1",
			Function: tools.CallFunction{
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hola"}`),
			},
		}},
		ToolResults: []string{"result-1"},
	}

	toolCtx := state.ToolExecutorContext()
	composerCtx := state.ResponseComposerContext()

	toolCtx.ToolCalls[0].Function.Arguments[0] = '['
	composerCtx.ToolResults[0] = "changed"

	if got, want := string(state.ToolCalls[0].Function.Arguments), `{"text":"hola"}`; got != want {
		t.Fatalf("state ToolCalls args mutated = %q, want %q", got, want)
	}
	if got, want := state.ToolResults[0], "result-1"; got != want {
		t.Fatalf("state ToolResults mutated = %q, want %q", got, want)
	}
}
