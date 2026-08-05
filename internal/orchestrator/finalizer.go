// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"strings"
)

// Finalizer closes the execution and leaves a final response.
type Finalizer struct{}

// Name returns the node name used by the orchestrator.
func (Finalizer) Name() string { return "finalizer" }

// Run copies the draft response to final response and stops the flow.
func (Finalizer) Run(_ context.Context, state State) (State, string, error) {
	ctx := state.FinalizerContext()
	if strings.TrimSpace(ctx.FinalResponse) == "" {
		state.FinalResponse = strings.TrimSpace(ctx.DraftResponse)
	}
	if state.FinalResponse == "" {
		state.FinalResponse = "Sin respuesta disponible."
	}
	state.Done = true
	state.NextNode = ""
	return state, "", nil
}
