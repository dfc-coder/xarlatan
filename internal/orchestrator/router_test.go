package orchestrator

import (
	"context"
	"testing"
)

func TestRouter_RunSelectsNextNode(t *testing.T) {
	tests := []struct {
		name     string
		state    State
		want     string
		wantDone bool
	}{
		{
			name:     "direct response",
			state:    State{Input: "dime el estado del tiempo"},
			want:     "response_composer",
			wantDone: false,
		},
		{
			name:     "needs planning by cue",
			state:    State{Input: "busca una receta con esos ingredientes"},
			want:     "planner",
			wantDone: false,
		},
		{
			name:     "needs planning by clarify intent",
			state:    State{Input: "dime la hora", Intent: "clarify"},
			want:     "planner",
			wantDone: false,
		},
		{
			name:     "empty input ends flow",
			state:    State{Input: "   "},
			want:     "",
			wantDone: true,
		},
	}

	router := Router{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, next, err := router.Run(context.Background(), tc.state)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := next; got != tc.want {
				t.Fatalf("next = %q, want %q", got, tc.want)
			}
			if got := state.NextNode; got != tc.want {
				t.Fatalf("state.NextNode = %q, want %q", got, tc.want)
			}
			if got := state.Done; got != tc.wantDone {
				t.Fatalf("state.Done = %v, want %v", got, tc.wantDone)
			}
		})
	}
}
