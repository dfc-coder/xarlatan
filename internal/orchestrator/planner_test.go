package orchestrator

import (
	"context"
	"testing"
)

func TestPlanner_RunSetsIntentAndNextNode(t *testing.T) {
	tests := []struct {
		name       string
		state      State
		wantIntent string
		wantNext   string
	}{
		{
			name:       "direct answer",
			state:      State{Input: "cuanto es 2+2"},
			wantIntent: "answer_direct",
			wantNext:   "response_composer",
		},
		{
			name:       "clarification needed",
			state:      State{Input: "hazlo"},
			wantIntent: "clarify",
			wantNext:   "response_composer",
		},
	}

	planner := Planner{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, next, err := planner.Run(context.Background(), tc.state)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := state.Intent; got != tc.wantIntent {
				t.Fatalf("Intent = %q, want %q", got, tc.wantIntent)
			}
			if got := next; got != tc.wantNext {
				t.Fatalf("next = %q, want %q", got, tc.wantNext)
			}
			if got := state.NextNode; got != tc.wantNext {
				t.Fatalf("state.NextNode = %q, want %q", got, tc.wantNext)
			}
		})
	}
}
