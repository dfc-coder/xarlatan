package orchestrator

import (
	"context"
	"reflect"
	"testing"
)

func TestState_ContainsCoreFields(t *testing.T) {
	want := []string{
		"Input",
		"Intent",
		"Summary",
		"RecentTurns",
		"ToolCalls",
		"ToolResults",
		"DraftResponse",
		"FinalResponse",
		"NextNode",
		"Done",
	}

	typeOf := reflect.TypeOf(State{})
	for _, name := range want {
		if _, ok := typeOf.FieldByName(name); !ok {
			t.Fatalf("State missing field %q", name)
		}
	}
}

func TestState_ChangesThroughRealNodes(t *testing.T) {
	ctx := context.Background()
	initial := State{Input: "busca un restaurante cercano"}

	router := Router{}
	planner := Planner{}
	composer := ResponseComposer{}
	finalizer := Finalizer{}

	afterRouter, next, err := router.Run(ctx, initial)
	if err != nil {
		t.Fatalf("router.Run() error = %v", err)
	}
	if got, want := next, "planner"; got != want {
		t.Fatalf("router next = %q, want %q", got, want)
	}
	if got, want := afterRouter.NextNode, "planner"; got != want {
		t.Fatalf("router state.NextNode = %q, want %q", got, want)
	}
	if reflect.DeepEqual(initial, afterRouter) {
		t.Fatalf("router returned unchanged state")
	}

	afterPlanner, next, err := planner.Run(ctx, afterRouter)
	if err != nil {
		t.Fatalf("planner.Run() error = %v", err)
	}
	if got, want := afterPlanner.Intent, "answer_direct"; got != want {
		t.Fatalf("planner intent = %q, want %q", got, want)
	}
	if got, want := next, "response_composer"; got != want {
		t.Fatalf("planner next = %q, want %q", got, want)
	}
	if got, want := afterPlanner.NextNode, "response_composer"; got != want {
		t.Fatalf("planner state.NextNode = %q, want %q", got, want)
	}

	afterComposer, next, err := composer.Run(ctx, afterPlanner)
	if err != nil {
		t.Fatalf("composer.Run() error = %v", err)
	}
	if got, want := afterComposer.DraftResponse, "Entendido: busca un restaurante cercano"; got != want {
		t.Fatalf("composer draft = %q, want %q", got, want)
	}
	if got, want := next, "finalizer"; got != want {
		t.Fatalf("composer next = %q, want %q", got, want)
	}

	afterFinal, next, err := finalizer.Run(ctx, afterComposer)
	if err != nil {
		t.Fatalf("finalizer.Run() error = %v", err)
	}
	if got, want := afterFinal.FinalResponse, "Entendido: busca un restaurante cercano"; got != want {
		t.Fatalf("finalizer response = %q, want %q", got, want)
	}
	if !afterFinal.Done {
		t.Fatalf("finalizer Done = false, want true")
	}
	if got, want := next, ""; got != want {
		t.Fatalf("finalizer next = %q, want empty", got)
	}
}
