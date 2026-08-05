package orchestrator

import (
	"context"
	"reflect"
	"testing"
)

type stubNode struct {
	name string
	next string
	run  func(State) State
}

func (n stubNode) Name() string { return n.name }

func (n stubNode) Run(_ context.Context, state State) (State, string, error) {
	if n.run != nil {
		state = n.run(state)
	}
	return state, n.next, nil
}

func TestRun_ExecutesNodesInOrderAndStops(t *testing.T) {
	steps := make([]string, 0, 2)
	orch := New("intake",
		stubNode{
			name: "intake",
			next: "finalizer",
			run: func(state State) State {
				state.Input = "hello"
				steps = append(steps, "intake")
				return state
			},
		},
		stubNode{
			name: "finalizer",
			next: "",
			run: func(state State) State {
				state.FinalResponse = "done"
				state.Done = true
				steps = append(steps, "finalizer")
				return state
			},
		},
	)

	state, err := orch.Run(context.Background(), State{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := state.Input, "hello"; got != want {
		t.Fatalf("Input = %q, want %q", got, want)
	}
	if got, want := state.FinalResponse, "done"; got != want {
		t.Fatalf("FinalResponse = %q, want %q", got, want)
	}
	if !state.Done {
		t.Fatalf("Done = false, want true")
	}
	if got, want := len(steps), 2; got != want {
		t.Fatalf("len(steps) = %d, want %d", got, want)
	}
	if steps[0] != "intake" || steps[1] != "finalizer" {
		t.Fatalf("steps = %v, want [intake finalizer]", steps)
	}
}

func TestRun_TransitionOverridesMatchExactEdge(t *testing.T) {
	steps := make([]string, 0, 4)
	orch := New("router",
		stubNode{name: "router", next: "response_composer", run: func(state State) State {
			steps = append(steps, "router")
			return state
		}},
		stubNode{name: "response_composer", next: "tool_executor", run: func(state State) State {
			steps = append(steps, "response_composer")
			return state
		}},
		stubNode{name: "tool_executor", next: "finalizer", run: func(state State) State {
			steps = append(steps, "tool_executor")
			return state
		}},
		stubNode{name: "memory_update", next: "finalizer", run: func(state State) State {
			steps = append(steps, "memory_update")
			return state
		}},
		stubNode{name: "finalizer", next: "", run: func(state State) State {
			steps = append(steps, "finalizer")
			state.Done = true
			return state
		}},
	).WithTransitionOverrides(map[string]string{
		"response_composer->finalizer": "memory_update",
	})

	_, err := orch.Run(context.Background(), State{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := steps, []string{"router", "response_composer", "tool_executor", "finalizer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
}
