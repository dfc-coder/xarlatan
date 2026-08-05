// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"strings"
)

// Planner classifies the user's request and prepares the next step.
type Planner struct{}

// Name returns the node name used by the orchestrator.
func (Planner) Name() string { return "planner" }

// Run sets a coarse intent and routes to response composition.
func (Planner) Run(_ context.Context, state State) (State, string, error) {
	state.Intent = classifyIntent(state.PlannerContext())
	state.NextNode = "response_composer"
	return state, state.NextNode, nil
}

func classifyIntent(ctx PlannerContext) string {
	input := strings.ToLower(strings.TrimSpace(ctx.Input))
	if input == "" {
		return "clarify"
	}
	for _, cue := range []string{"hazlo", "esto", "eso", "lo mismo", "algo", "cualquier cosa"} {
		if input == cue || strings.Contains(input, cue+" ") || strings.Contains(input, " "+cue) {
			return "clarify"
		}
	}
	if strings.Contains(input, "?") {
		return "answer_direct"
	}
	if len(input) <= 6 {
		return "clarify"
	}
	return "answer_direct"
}
