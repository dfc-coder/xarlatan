// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"strings"
)

// Router chooses the next node from the current state.
type Router struct{}

// Name returns the node name used by the orchestrator.
func (Router) Name() string { return "router" }

// Run routes direct requests to response composition and planning-heavy
// requests to the planner.
func (Router) Run(_ context.Context, state State) (State, string, error) {
	next := routeNext(state.RouterContext())
	state.NextNode = next
	if next == "" {
		state.Done = true
	}
	return state, next, nil
}

func routeNext(ctx RouterContext) string {
	input := strings.ToLower(strings.TrimSpace(ctx.Input))
	intent := strings.ToLower(strings.TrimSpace(ctx.Intent))
	if input == "" {
		return ""
	}
	if intent == "clarify" || needsPlanning(input) {
		return "planner"
	}
	return "response_composer"
}

func needsPlanning(input string) bool {
	for _, cue := range []string{
		"busca",
		"buscar",
		"encuentra",
		"calcula",
		"abre",
		"llama",
		"consulta",
		"reserva",
		"crea",
		"haz",
		"quiero que",
		"necesito",
	} {
		if strings.Contains(input, cue) {
			return true
		}
	}
	return strings.Contains(input, "?")
}
