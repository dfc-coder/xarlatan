package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestRouter_RunIgnoresIrrelevantFields(t *testing.T) {
	node := Router{}
	base := State{Input: "dime la hora", Intent: ""}
	noisy := State{
		Input:         "dime la hora",
		Intent:        "",
		Summary:       "no debe afectar",
		ToolResults:   []string{"ruido"},
		DraftResponse: "otro valor",
		FinalResponse: "final",
		NextNode:      "planner",
	}

	baseOut, baseNext, err := node.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("base Run() error = %v", err)
	}
	noisyOut, noisyNext, err := node.Run(context.Background(), noisy)
	if err != nil {
		t.Fatalf("noisy Run() error = %v", err)
	}

	if baseNext != noisyNext {
		t.Fatalf("next differs with irrelevant fields: base=%q noisy=%q", baseNext, noisyNext)
	}
	if baseOut.Done != noisyOut.Done {
		t.Fatalf("Done differs with irrelevant fields: base=%v noisy=%v", baseOut.Done, noisyOut.Done)
	}
}

func TestPlanner_RunIgnoresIrrelevantFields(t *testing.T) {
	node := Planner{}
	base := State{Input: "hazlo"}
	noisy := State{
		Input:         "hazlo",
		Intent:        "answer_direct",
		Summary:       "no debe afectar",
		ToolResults:   []string{"ruido"},
		DraftResponse: "otro valor",
		FinalResponse: "final",
	}

	baseOut, baseNext, err := node.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("base Run() error = %v", err)
	}
	noisyOut, noisyNext, err := node.Run(context.Background(), noisy)
	if err != nil {
		t.Fatalf("noisy Run() error = %v", err)
	}

	if baseOut.Intent != noisyOut.Intent {
		t.Fatalf("intent differs with irrelevant fields: base=%q noisy=%q", baseOut.Intent, noisyOut.Intent)
	}
	if baseNext != noisyNext {
		t.Fatalf("next differs with irrelevant fields: base=%q noisy=%q", baseNext, noisyNext)
	}
}

func TestResponseComposer_RunIgnoresIrrelevantFields(t *testing.T) {
	node := ResponseComposer{}
	base := State{Input: "dime la hora", Summary: "resumen"}
	noisy := State{
		Input:         "dime la hora",
		Summary:       "resumen",
		FinalResponse: "no debe afectar",
		Done:          true,
		NextNode:      "tool_executor",
	}

	baseOut, baseNext, err := node.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("base Run() error = %v", err)
	}
	noisyOut, noisyNext, err := node.Run(context.Background(), noisy)
	if err != nil {
		t.Fatalf("noisy Run() error = %v", err)
	}

	if baseOut.DraftResponse != noisyOut.DraftResponse {
		t.Fatalf("draft differs with irrelevant fields: base=%q noisy=%q", baseOut.DraftResponse, noisyOut.DraftResponse)
	}
	if baseNext != noisyNext {
		t.Fatalf("next differs with irrelevant fields: base=%q noisy=%q", baseNext, noisyNext)
	}
}

func TestToolExecutor_RunIgnoresIrrelevantFields(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(echoTool{})
	node := ToolExecutor{Executor: tools.NewExecutor(r)}
	call := tools.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: tools.CallFunction{
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"hola"}`),
		},
	}
	base := State{ToolCalls: []tools.ToolCall{call}}
	noisy := State{
		Input:         "no debe afectar",
		Summary:       "otro",
		ToolCalls:     []tools.ToolCall{call},
		DraftResponse: "previo",
		FinalResponse: "final",
	}

	baseOut, baseNext, err := node.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("base Run() error = %v", err)
	}
	noisyOut, noisyNext, err := node.Run(context.Background(), noisy)
	if err != nil {
		t.Fatalf("noisy Run() error = %v", err)
	}

	if got, want := len(baseOut.ToolResults), 1; got != want {
		t.Fatalf("base ToolResults len = %d, want %d", got, want)
	}
	if baseOut.ToolResults[0] != noisyOut.ToolResults[0] {
		t.Fatalf("tool result differs with irrelevant fields: base=%q noisy=%q", baseOut.ToolResults[0], noisyOut.ToolResults[0])
	}
	if baseNext != noisyNext {
		t.Fatalf("next differs with irrelevant fields: base=%q noisy=%q", baseNext, noisyNext)
	}
}

func TestFinalizer_RunIgnoresIrrelevantFields(t *testing.T) {
	node := Finalizer{}
	base := State{DraftResponse: "respuesta"}
	noisy := State{
		Input:         "no debe afectar",
		ToolResults:   []string{"ruido"},
		DraftResponse: "respuesta",
		NextNode:      "planner",
	}

	baseOut, baseNext, err := node.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("base Run() error = %v", err)
	}
	noisyOut, noisyNext, err := node.Run(context.Background(), noisy)
	if err != nil {
		t.Fatalf("noisy Run() error = %v", err)
	}

	if baseOut.FinalResponse != noisyOut.FinalResponse {
		t.Fatalf("final response differs with irrelevant fields: base=%q noisy=%q", baseOut.FinalResponse, noisyOut.FinalResponse)
	}
	if baseNext != noisyNext {
		t.Fatalf("next differs with irrelevant fields: base=%q noisy=%q", baseNext, noisyNext)
	}
}
