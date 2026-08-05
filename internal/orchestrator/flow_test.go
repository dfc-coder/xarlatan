package orchestrator

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

type recordingNode struct {
	node  Node
	steps *[]string
}

func (n recordingNode) Name() string {
	return n.node.Name()
}

func (n recordingNode) Run(ctx context.Context, state State) (State, string, error) {
	*n.steps = append(*n.steps, n.node.Name())
	return n.node.Run(ctx, state)
}

func TestFlow_TransitionsBetweenBasicNodes(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSteps  []string
		wantIntent string
	}{
		{
			name:       "direct path",
			input:      "dime la hora",
			wantSteps:  []string{"router", "response_composer", "finalizer"},
			wantIntent: "",
		},
		{
			name:       "planner path",
			input:      "busca un restaurante cercano",
			wantSteps:  []string{"router", "planner", "response_composer", "finalizer"},
			wantIntent: "answer_direct",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := make([]string, 0, 4)
			orch := New("router",
				recordingNode{node: Router{}, steps: &steps},
				recordingNode{node: Planner{}, steps: &steps},
				recordingNode{node: ResponseComposer{}, steps: &steps},
				recordingNode{node: Finalizer{}, steps: &steps},
			)

			state, err := orch.Run(context.Background(), State{Input: tc.input})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !reflect.DeepEqual(steps, tc.wantSteps) {
				t.Fatalf("steps = %v, want %v", steps, tc.wantSteps)
			}
			if !state.Done {
				t.Fatalf("Done = false, want true")
			}
			if state.FinalResponse == "" {
				t.Fatalf("FinalResponse is empty")
			}
			if got := state.Intent; got != tc.wantIntent {
				t.Fatalf("Intent = %q, want %q", got, tc.wantIntent)
			}
		})
	}
}

func TestFlow_SkipsToolExecutorWhenNoToolsArePending(t *testing.T) {
	steps := make([]string, 0, 5)
	orch := New("router",
		recordingNode{node: Router{}, steps: &steps},
		recordingNode{node: Planner{}, steps: &steps},
		recordingNode{node: ResponseComposer{}, steps: &steps},
		recordingNode{node: ToolExecutor{}, steps: &steps},
		recordingNode{node: Finalizer{}, steps: &steps},
	)

	state, err := orch.Run(context.Background(), State{Input: "dime la hora"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := steps, []string{"router", "response_composer", "finalizer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	if got, want := state.FinalResponse, "Entendido: dime la hora"; got != want {
		t.Fatalf("FinalResponse = %q, want %q", got, want)
	}
}

func TestFlow_RunsToolExecutorWhenToolsArePending(t *testing.T) {
	steps := make([]string, 0, 6)
	r := tools.NewRegistry()
	r.Register(echoTool{})
	orch := New("router",
		recordingNode{node: Router{}, steps: &steps},
		recordingNode{node: ResponseComposer{}, steps: &steps},
		recordingNode{node: ToolExecutor{Executor: tools.NewExecutor(r)}, steps: &steps},
		recordingNode{node: Finalizer{}, steps: &steps},
	)

	state, err := orch.Run(context.Background(), State{
		Input: "dime algo",
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
	if got, want := steps, []string{"router", "response_composer", "tool_executor", "response_composer", "finalizer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	if got, want := len(state.ToolResults), 1; got != want {
		t.Fatalf("len(ToolResults) = %d, want %d", got, want)
	}
	if got, want := state.FinalResponse, `{"text":"hola"}`; got != want {
		t.Fatalf("FinalResponse = %q, want %q", got, want)
	}
}

func TestFlow_AllowsInsertingNewNodeViaTransitionOverride(t *testing.T) {
	steps := make([]string, 0, 5)
	orch := New("router",
		recordingNode{node: Router{}, steps: &steps},
		recordingNode{node: ResponseComposer{}, steps: &steps},
		recordingNode{node: MemoryUpdate{}, steps: &steps},
		recordingNode{node: Finalizer{}, steps: &steps},
	).WithTransitionOverrides(map[string]string{
		"response_composer->finalizer": "memory_update",
	})

	state, err := orch.Run(context.Background(), State{Input: "dime la hora"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := steps, []string{"router", "response_composer", "memory_update", "finalizer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	if got, want := state.FinalResponse, "Entendido: dime la hora"; got != want {
		t.Fatalf("FinalResponse = %q, want %q", got, want)
	}
}
