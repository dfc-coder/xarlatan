// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import "context"

// MemoryUpdate is an optional extension point to compact/update memory.
type MemoryUpdate struct{}

// Name returns the node name used by the orchestrator.
func (MemoryUpdate) Name() string { return "memory_update" }

// Run forwards the flow to finalization.
func (MemoryUpdate) Run(_ context.Context, state State) (State, string, error) {
	state.NextNode = "finalizer"
	return state, state.NextNode, nil
}
