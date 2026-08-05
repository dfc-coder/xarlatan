// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"strings"
)

// ResponseComposer builds a draft response from the current state.
type ResponseComposer struct{}

// Name returns the node name used by the orchestrator.
func (ResponseComposer) Name() string { return "response_composer" }

// Run composes a response draft and advances to finalization.
func (ResponseComposer) Run(_ context.Context, state State) (State, string, error) {
	ctx := state.ResponseComposerContext()
	if len(ctx.ToolCalls) > 0 && len(ctx.ToolResults) == 0 {
		state.NextNode = "tool_executor"
		return state, state.NextNode, nil
	}
	if strings.TrimSpace(ctx.DraftResponse) == "" {
		state.DraftResponse = composeDraftResponse(ctx)
	}
	state.NextNode = "finalizer"
	return state, state.NextNode, nil
}

func composeDraftResponse(ctx ResponseComposerContext) string {
	if ctx.Intent == "clarify" {
		return "Necesito un poco mas de contexto para responder bien."
	}
	if len(ctx.ToolResults) > 0 {
		return strings.TrimSpace(strings.Join(ctx.ToolResults, "\n"))
	}
	if msg := strings.TrimSpace(ctx.Input); msg != "" {
		return "Entendido: " + msg
	}
	if sum := strings.TrimSpace(ctx.Summary); sum != "" {
		return "Contexto actual: " + sum
	}
	return "Listo."
}
