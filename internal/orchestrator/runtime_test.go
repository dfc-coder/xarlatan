package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

type modelStep struct {
	content string
	calls   []tools.ToolCall
	err     error
}

type scriptedModel struct {
	mu       sync.Mutex
	steps    []modelStep
	calls    int
	inputs   []string
	history  [][]llm.Message
}

func (m *scriptedModel) Generate(
	_ context.Context,
	history []llm.Message,
	userText string,
	_ *tools.Registry,
) (string, string, []llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.inputs = append(m.inputs, userText)
	m.history = append(m.history, cloneTestMessages(history))
	if len(m.steps) == 0 {
		return "", "", nil, errors.New("unexpected model call")
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	if step.err != nil {
		return "", "", nil, step.err
	}
	next := cloneTestMessages(history)
	if userText != "" {
		next = append(next, llm.Message{Role: "user", Content: userText})
	}
	next = append(next, llm.Message{Role: "assistant", Content: step.content, ToolCalls: cloneTestCalls(step.calls)})
	return step.content, tools.FormatToolCalls(step.calls), next, nil
}

func (m *scriptedModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type recordingTool struct {
	name    string
	result  tools.Result
	calls   atomic.Int32
	order   *[]string
	orderMu *sync.Mutex
	block   bool
}

func (t *recordingTool) Name() string { return t.name }
func (t *recordingTool) Description() string { return "test tool" }
func (t *recordingTool) Schema() tools.ParameterSchema {
	return tools.NewSchema(nil, map[string]tools.Property{})
}
func (t *recordingTool) Execute(ctx context.Context, _ json.RawMessage) tools.Result {
	t.calls.Add(1)
	if t.order != nil {
		t.orderMu.Lock()
		*t.order = append(*t.order, t.name)
		t.orderMu.Unlock()
	}
	if t.block {
		<-ctx.Done()
		return tools.Errorf("cancelled: %v", ctx.Err())
	}
	return t.result
}

type traceObserver struct {
	mu     sync.Mutex
	rounds []RoundTrace
}

func (o *traceObserver) OnRound(round RoundTrace) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rounds = append(o.rounds, round)
}

func (o *traceObserver) snapshot() []RoundTrace {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]RoundTrace, len(o.rounds))
	copy(out, o.rounds)
	return out
}

func TestAgentReturnsDirectReply(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{{content: "respuesta directa"}}}
	runtime := mustRuntime(t, model, nil, 3, nil)

	result, err := runtime.Run(context.Background(), Request{
		Input:   "hola",
		History: []llm.Message{{Role: "system", Content: "system"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := result.Reply, "respuesta directa"; got != want {
		t.Fatalf("Reply = %q, want %q", got, want)
	}
	if got, want := model.callCount(), 1; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if got, want := messageRoles(result.History), []string{"system", "user", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	if got, want := result.Trace.StopReason, StopDirectReply; got != want {
		t.Fatalf("StopReason = %q, want %q", got, want)
	}
	if got, want := len(result.Trace.Rounds), 1; got != want {
		t.Fatalf("rounds = %d, want %d", got, want)
	}
}

func TestAgentExecutesOneToolRound(t *testing.T) {
	call := testCall("call-1", "echo")
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{call}},
		{content: "respuesta final"},
	}}
	echo := &recordingTool{name: "echo", result: tools.Result{Content: "tool output"}}
	runtime := mustRuntime(t, model, []tools.Tool{echo}, 3, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "usa echo"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := echo.calls.Load(), int32(1); got != want {
		t.Fatalf("tool calls = %d, want %d", got, want)
	}
	if got, want := result.Reply, "respuesta final"; got != want {
		t.Fatalf("Reply = %q, want %q", got, want)
	}
	if got, want := messageRoles(result.History), []string{"user", "assistant", "tool", "assistant"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	if got, want := result.History[2].ToolCallID, "call-1"; got != want {
		t.Fatalf("tool_call_id = %q, want %q", got, want)
	}
	if got, want := len(result.Trace.Rounds), 2; got != want {
		t.Fatalf("rounds = %d, want %d", got, want)
	}
}

func TestAgentExecutesMultipleToolRounds(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("call-1", "first")}},
		{calls: []tools.ToolCall{testCall("call-2", "second")}},
		{content: "terminado"},
	}}
	first := &recordingTool{name: "first", result: tools.Result{Content: "one"}}
	second := &recordingTool{name: "second", result: tools.Result{Content: "two"}}
	runtime := mustRuntime(t, model, []tools.Tool{first, second}, 3, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "dos pasos"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := model.callCount(), 3; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if got, want := len(result.Trace.Rounds), 3; got != want {
		t.Fatalf("rounds = %d, want %d", got, want)
	}
	if got, want := result.Trace.ToolRounds, 2; got != want {
		t.Fatalf("tool rounds = %d, want %d", got, want)
	}
	if got, want := result.Reply, "terminado"; got != want {
		t.Fatalf("Reply = %q, want %q", got, want)
	}
}

func TestAgentStopsAtConfiguredRoundLimit(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("call-1", "echo")}},
		{calls: []tools.ToolCall{testCall("call-2", "echo")}},
	}}
	echo := &recordingTool{name: "echo", result: tools.Result{Content: "ok"}}
	runtime := mustRuntime(t, model, []tools.Tool{echo}, 1, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "loop"})
	if !IsRuntimeErrorCode(err, RuntimeRoundLimit) {
		t.Fatalf("Run() error = %v, want %q", err, RuntimeRoundLimit)
	}
	if got, want := echo.calls.Load(), int32(1); got != want {
		t.Fatalf("tool calls = %d, want %d", got, want)
	}
	if got, want := result.Trace.StopReason, StopRoundLimit; got != want {
		t.Fatalf("StopReason = %q, want %q", got, want)
	}
	if got, want := result.Trace.ToolRounds, 1; got != want {
		t.Fatalf("tool rounds = %d, want %d", got, want)
	}
}

func TestAgentPreservesToolCallOrdering(t *testing.T) {
	var order []string
	var orderMu sync.Mutex
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("call-a", "alpha"), testCall("call-b", "beta")}},
		{content: "ordered"},
	}}
	alpha := &recordingTool{name: "alpha", result: tools.Result{Content: "A"}, order: &order, orderMu: &orderMu}
	beta := &recordingTool{name: "beta", result: tools.Result{Content: "B"}, order: &order, orderMu: &orderMu}
	runtime := mustRuntime(t, model, []tools.Tool{alpha, beta}, 2, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "order"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := order, []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("execution order = %v, want %v", got, want)
	}
	toolMessages := messagesByRole(result.History, "tool")
	if got, want := []string{toolMessages[0].ToolCallID, toolMessages[1].ToolCallID}, []string{"call-a", "call-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool IDs = %v, want %v", got, want)
	}
	traces := result.Trace.Rounds[0].Tools
	if got, want := []string{traces[0].CallID, traces[1].CallID}, []string{"call-a", "call-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace IDs = %v, want %v", got, want)
	}
}

func TestAgentReturnsUnknownToolError(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("missing-1", "missing")}},
		{content: "me recuperé"},
	}}
	runtime := mustRuntime(t, model, nil, 2, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "unknown"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	toolMessages := messagesByRole(result.History, "tool")
	if got, want := len(toolMessages), 1; got != want {
		t.Fatalf("tool messages = %d, want %d", got, want)
	}
	if got, want := result.Trace.Rounds[0].Tools[0].ErrorCode, tools.ExecutionUnknownTool; got != want {
		t.Fatalf("ErrorCode = %q, want %q", got, want)
	}
	if got, want := result.Reply, "me recuperé"; got != want {
		t.Fatalf("Reply = %q, want %q", got, want)
	}
}

func TestAgentContinuesAfterRecoverableToolError(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("fail-1", "fail")}},
		{content: "respuesta después del error"},
	}}
	fail := &recordingTool{name: "fail", result: tools.Errorf("expected failure")}
	runtime := mustRuntime(t, model, []tools.Tool{fail}, 2, nil)

	result, err := runtime.Run(context.Background(), Request{Input: "recover"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := result.Trace.Rounds[0].Tools[0].ErrorCode, tools.ExecutionToolError; got != want {
		t.Fatalf("ErrorCode = %q, want %q", got, want)
	}
	if got, want := model.callCount(), 2; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
}

func TestAgentCancelsDuringToolExecution(t *testing.T) {
	model := &scriptedModel{steps: []modelStep{{calls: []tools.ToolCall{testCall("block-1", "block")}}}}
	block := &recordingTool{name: "block", block: true}
	runtime := mustRuntime(t, model, []tools.Tool{block}, 2, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	result, err := runtime.Run(ctx, Request{Input: "cancel"})
	if !IsRuntimeErrorCode(err, RuntimeCancelled) {
		t.Fatalf("Run() error = %v, want %q", err, RuntimeCancelled)
	}
	if got, want := model.callCount(), 1; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if got, want := result.Trace.StopReason, StopCancelled; got != want {
		t.Fatalf("StopReason = %q, want %q", got, want)
	}
}

func TestAgentEmitsTraceForEveryRound(t *testing.T) {
	observer := &traceObserver{}
	model := &scriptedModel{steps: []modelStep{
		{calls: []tools.ToolCall{testCall("call-1", "echo")}},
		{content: "done"},
	}}
	echo := &recordingTool{name: "echo", result: tools.Result{Content: "ok"}}
	runtime := mustRuntime(t, model, []tools.Tool{echo}, 2, observer)

	result, err := runtime.Run(context.Background(), Request{Input: "trace"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	observed := observer.snapshot()
	if got, want := len(observed), len(result.Trace.Rounds); got != want {
		t.Fatalf("observed rounds = %d, want %d", got, want)
	}
	for i, round := range observed {
		if got, want := round.Number, i+1; got != want {
			t.Fatalf("round[%d].Number = %d, want %d", i, got, want)
		}
		if round.Duration <= 0 {
			t.Fatalf("round[%d].Duration = %v, want positive", i, round.Duration)
		}
	}
}

func mustRuntime(t *testing.T, model Generator, candidates []tools.Tool, maxRounds int, observer RoundObserver) *AgentRuntime {
	t.Helper()
	registry := tools.NewRegistry(tools.AllowAllToolPolicy())
	for _, candidate := range candidates {
		if err := registry.Register(candidate); err != nil {
			t.Fatalf("register %q: %v", candidate.Name(), err)
		}
	}
	runtime, err := NewAgentRuntime(RuntimeConfig{MaxToolRounds: maxRounds}, model, registry, tools.NewExecutor(registry), observer)
	if err != nil {
		t.Fatalf("NewAgentRuntime() error = %v", err)
	}
	return runtime
}

func testCall(id, name string) tools.ToolCall {
	return tools.ToolCall{
		ID:   id,
		Type: "function",
		Function: tools.CallFunction{
			Name:      name,
			Arguments: json.RawMessage(`{}`),
		},
	}
}

func cloneTestMessages(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	for i, message := range messages {
		out[i] = message
		out[i].ToolCalls = cloneTestCalls(message.ToolCalls)
	}
	return out
}

func cloneTestCalls(calls []tools.ToolCall) []tools.ToolCall {
	out := make([]tools.ToolCall, len(calls))
	copy(out, calls)
	return out
}

func messageRoles(messages []llm.Message) []string {
	roles := make([]string, len(messages))
	for i, message := range messages {
		roles[i] = message.Role
	}
	return roles
}

func messagesByRole(messages []llm.Message, role string) []llm.Message {
	var out []llm.Message
	for _, message := range messages {
		if message.Role == role {
			out = append(out, message)
		}
	}
	return out
}
