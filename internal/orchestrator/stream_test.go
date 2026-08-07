package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

type streamingScriptedModel struct {
	*scriptedModel
	deltas      []string
	streamCalls atomic.Int32
}

func (m *streamingScriptedModel) GenerateStream(
	ctx context.Context,
	history []llm.Message,
	userText string,
	registry *tools.Registry,
	onDelta llm.ContentDelta,
) (string, string, []llm.Message, error) {
	m.streamCalls.Add(1)
	for _, delta := range m.deltas {
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return "", "", nil, err
			}
		}
	}
	return m.scriptedModel.Generate(ctx, history, userText, registry)
}

func TestAgentRunStreamPublishesDirectReplyDeltas(t *testing.T) {
	model := &streamingScriptedModel{
		scriptedModel: &scriptedModel{steps: []modelStep{{content: "Hola, mundo."}}},
		deltas:        []string{"Hola", ", mundo."},
	}
	runtime := mustRuntime(t, model, nil, 3, nil)
	var deltas []string

	result, err := runtime.RunStream(context.Background(), Request{Input: "saluda"}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if result.Reply != "Hola, mundo." {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if !reflect.DeepEqual(deltas, []string{"Hola", ", mundo."}) {
		t.Fatalf("deltas = %v", deltas)
	}
	if got := model.streamCalls.Load(); got != 1 {
		t.Fatalf("stream calls = %d, want 1", got)
	}
}

func TestAgentRunStreamFallsBackForNonStreamingGenerator(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{{content: "buffered"}}}
	runtime := mustRuntime(t, model, nil, 3, nil)
	called := false

	result, err := runtime.RunStream(context.Background(), Request{Input: "hola"}, func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if result.Reply != "buffered" || called {
		t.Fatalf("result=%q callback=%v", result.Reply, called)
	}
}

func TestAgentRunStreamSuppressesStreamingWhenToolsAreExposed(t *testing.T) {
	model := &streamingScriptedModel{
		scriptedModel: &scriptedModel{steps: []modelStep{
			{calls: []tools.ToolCall{testCall("call-1", "echo")}},
			{content: "respuesta final"},
		}},
		deltas: []string{"no debe salir"},
	}
	echo := &recordingTool{name: "echo", result: tools.Result{Content: "ok"}}
	runtime := mustRuntime(t, model, []tools.Tool{echo}, 2, nil)
	called := false

	result, err := runtime.RunStream(context.Background(), Request{Input: "usa echo"}, func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if result.Reply != "respuesta final" {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if called {
		t.Fatal("stream callback invoked while tools were exposed")
	}
	if got := model.streamCalls.Load(); got != 0 {
		t.Fatalf("GenerateStream calls = %d, want 0", got)
	}
	if got := model.callCount(); got != 2 {
		t.Fatalf("Generate calls = %d, want 2", got)
	}
}

func TestAgentRunStreamPropagatesSinkFailureAsModelError(t *testing.T) {
	model := &streamingScriptedModel{
		scriptedModel: &scriptedModel{steps: []modelStep{{content: "unused"}}},
		deltas:        []string{"delta"},
	}
	runtime := mustRuntime(t, model, nil, 2, nil)
	sinkErr := errors.New("sink failed")

	_, err := runtime.RunStream(context.Background(), Request{Input: "hola"}, func(string) error {
		return sinkErr
	})
	if !IsRuntimeErrorCode(err, RuntimeModelError) || !errors.Is(err, sinkErr) {
		t.Fatalf("RunStream() error = %v", err)
	}
}
