package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

type blockingModel struct{}

func (blockingModel) Generate(
	ctx context.Context,
	_ []llm.Message,
	_ string,
	_ *tools.Registry,
) (string, string, []llm.Message, error) {
	<-ctx.Done()
	return "", "", nil, ctx.Err()
}

func TestNewAgentRuntimeRejectsInvalidConfig(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{{content: "unused"}}}

	if _, err := NewAgentRuntime(RuntimeConfig{}, model, nil, nil, nil); !IsRuntimeErrorCode(err, RuntimeInvalidRuntime) {
		t.Fatalf("zero max rounds error = %v, want %q", err, RuntimeInvalidRuntime)
	}
	if _, err := NewAgentRuntime(RuntimeConfig{MaxToolRounds: 1}, nil, nil, nil, nil); !IsRuntimeErrorCode(err, RuntimeInvalidRuntime) {
		t.Fatalf("nil model error = %v, want %q", err, RuntimeInvalidRuntime)
	}
}

func TestAgentReturnsModelErrorWithTrace(t *testing.T) {
	modelErr := errors.New("model unavailable")
	model := &scriptedModel{steps: []modelStep{{err: modelErr}}}
	runtime := mustRuntime(t, model, nil, 2, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "hola"})
	if !IsRuntimeErrorCode(err, RuntimeModelError) {
		t.Fatalf("Run() error = %v, want %q", err, RuntimeModelError)
	}
	if !errors.Is(err, modelErr) {
		t.Fatalf("Run() error = %v, want wrapped model error", err)
	}
	if got, want := result.Trace.StopReason, StopModelError; got != want {
		t.Fatalf("StopReason = %q, want %q", got, want)
	}
	if got, want := len(result.Trace.Rounds), 1; got != want {
		t.Fatalf("rounds = %d, want %d", got, want)
	}
	if result.Trace.Rounds[0].Error == "" {
		t.Fatal("round error is empty")
	}
}

func TestAgentHonorsDeadlineDuringModelGeneration(t *testing.T) {
	runtime, err := NewAgentRuntime(RuntimeConfig{MaxToolRounds: 1}, blockingModel{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAgentRuntime() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result, err := runtime.Run(ctx, Request{Input: "wait"})
	if !IsRuntimeErrorCode(err, RuntimeDeadlineExceeded) {
		t.Fatalf("Run() error = %v, want %q", err, RuntimeDeadlineExceeded)
	}
	if got, want := result.Trace.StopReason, StopModelError; got != want {
		t.Fatalf("trace StopReason = %q, want %q", got, want)
	}
}

func TestAgentDoesNotMutateInputHistory(t *testing.T) {
	history := []llm.Message{{
		Role:    "assistant",
		Content: "prior",
		ToolCalls: []tools.ToolCall{{
			ID: "old-call",
			Function: tools.CallFunction{
				Name:      "old-tool",
				Arguments: []byte(`{"value":"original"}`),
			},
		}},
	}}
	before := cloneTestMessages(history)
	model := &scriptedModel{steps: []modelStep{{content: "reply"}}}
	runtime := mustRuntime(t, model, nil, 1, nil)

	if _, err := runtime.Run(context.Background(), Request{Input: "new", History: history}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(history, before) {
		t.Fatalf("input history mutated: got %#v, want %#v", history, before)
	}
}

func TestAgentReturnsDeniedToolErrorToModel(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("denied-1", "danger")}},
		{content: "denied handled"},
	}}
	registry := tools.NewRegistry(tools.DefaultToolPolicy())
	danger := &recordingTool{name: "danger", result: tools.Result{Content: "must not run"}}
	if err := registry.Register(danger); !tools.IsToolDenied(err) {
		t.Fatalf("Register() error = %v, want policy denial", err)
	}
	runtime, err := NewAgentRuntime(
		RuntimeConfig{MaxToolRounds: 2},
		model,
		registry,
		tools.NewExecutor(registry),
		nil,
	)
	if err != nil {
		t.Fatalf("NewAgentRuntime() error = %v", err)
	}

	result, err := runtime.Run(context.Background(), Request{Input: "danger"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := danger.calls.Load(), int32(0); got != want {
		t.Fatalf("denied tool calls = %d, want %d", got, want)
	}
	if got, want := result.Trace.Rounds[0].Tools[0].ErrorCode, tools.ExecutionDeniedTool; got != want {
		t.Fatalf("ErrorCode = %q, want %q", got, want)
	}
	if got, want := result.Reply, "denied handled"; got != want {
		t.Fatalf("Reply = %q, want %q", got, want)
	}
}
