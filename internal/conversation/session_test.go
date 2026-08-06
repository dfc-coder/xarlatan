package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestSessionCallsAgentRuntimeOnce(t *testing.T) {
	manager := &fakeMemory{
		prepared: memory.Snapshot{History: []llm.Message{{Role: "system", Content: "trusted"}}},
		updated:  memory.Snapshot{History: []llm.Message{{Role: "system", Content: "trusted"}, {Role: "user", Content: "hello"}, {Role: "assistant", Content: "reply"}}},
	}
	agent := &fakeAgent{result: orchestrator.Result{
		Reply:   "reply",
		History: []llm.Message{{Role: "system", Content: "trusted"}, {Role: "user", Content: "hello"}, {Role: "assistant", Content: "reply"}},
	}}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := session.Respond(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if agent.calls != 1 {
		t.Fatalf("agent calls = %d, want 1", agent.calls)
	}
	if manager.prepareCalls != 1 || manager.updateCalls != 1 {
		t.Fatalf("memory calls = prepare %d update %d", manager.prepareCalls, manager.updateCalls)
	}
	if result.Reply != "reply" || !reflect.DeepEqual(result.History, manager.updated.History) {
		t.Fatalf("result = %+v", result)
	}
}

func TestSessionDoesNotCommitMemoryWhenAgentFails(t *testing.T) {
	manager := &fakeMemory{prepared: memory.Snapshot{History: []llm.Message{{Role: "system", Content: "trusted"}}}}
	agent := &fakeAgent{err: errors.New("model unavailable"), result: orchestrator.Result{
		History: []llm.Message{{Role: "system", Content: "trusted"}, {Role: "user", Content: "hello"}},
	}}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := session.Respond(context.Background(), "hello")
	if !IsErrorCode(err, ErrorAgent) {
		t.Fatalf("Respond() error = %v, want agent_failed", err)
	}
	if manager.updateCalls != 0 {
		t.Fatalf("memory update calls = %d, want 0", manager.updateCalls)
	}
	if len(result.History) != 0 {
		t.Fatalf("uncommitted history returned: %+v", result.History)
	}
}

func TestSessionDoesNotReturnUncommittedHistoryWhenUpdateFails(t *testing.T) {
	manager := &fakeMemory{
		prepared:  memory.Snapshot{History: []llm.Message{{Role: "system", Content: "trusted"}}},
		updateErr: errors.New("invalid history"),
	}
	agent := &fakeAgent{result: orchestrator.Result{
		Reply:   "reply",
		History: []llm.Message{{Role: "system", Content: "trusted"}, {Role: "user", Content: "hello"}, {Role: "assistant", Content: "reply"}},
	}}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := session.Respond(context.Background(), "hello")
	if !IsErrorCode(err, ErrorMemoryUpdate) {
		t.Fatalf("Respond() error = %v, want memory_update_failed", err)
	}
	if result.Reply != "" || len(result.History) != 0 {
		t.Fatalf("uncommitted result returned: %+v", result)
	}
}

func TestSessionStopsWhenPrepareFails(t *testing.T) {
	manager := &fakeMemory{prepareErr: errors.New("budget exceeded")}
	agent := &fakeAgent{}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = session.Respond(context.Background(), "hello")
	if !IsErrorCode(err, ErrorMemoryPrepare) {
		t.Fatalf("Respond() error = %v, want memory_prepare_failed", err)
	}
	if agent.calls != 0 || manager.updateCalls != 0 {
		t.Fatalf("unexpected downstream calls: agent=%d update=%d", agent.calls, manager.updateCalls)
	}
}

func TestSessionRejectsInvalidDependencies(t *testing.T) {
	if _, err := New(nil, &fakeAgent{}); !IsErrorCode(err, ErrorInvalidSession) {
		t.Fatalf("New(nil, agent) error = %v", err)
	}
	if _, err := New(&fakeMemory{}, nil); !IsErrorCode(err, ErrorInvalidSession) {
		t.Fatalf("New(memory, nil) error = %v", err)
	}
	var session *Session
	if _, err := session.Respond(context.Background(), "hello"); !IsErrorCode(err, ErrorInvalidSession) {
		t.Fatalf("nil session error = %v", err)
	}
}

func TestSessionRespectsContextAndBlankInput(t *testing.T) {
	manager := &fakeMemory{}
	agent := &fakeAgent{}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.Respond(ctx, "hello"); !IsErrorCode(err, ErrorCancelled) {
		t.Fatalf("cancelled Respond() error = %v", err)
	}

	if _, err := session.Respond(context.Background(), "   "); err != nil {
		t.Fatalf("blank Respond() error = %v", err)
	}
	if manager.prepareCalls != 0 || agent.calls != 0 {
		t.Fatalf("blank input invoked dependencies: prepare=%d agent=%d", manager.prepareCalls, agent.calls)
	}
}

func TestSessionReturnsDefensiveCommittedHistory(t *testing.T) {
	arguments := json.RawMessage(`{"query":"safe"}`)
	committed := []llm.Message{
		{Role: "system", Content: "trusted"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", ToolCalls: []tools.ToolCall{{
			ID: "call-1",
			Function: tools.CallFunction{
				Name:      "lookup",
				Arguments: arguments,
			},
		}},
		{Role: "tool", ToolCallID: "call-1", Content: "result"},
		{Role: "assistant", Content: "reply"},
	}
	manager := &fakeMemory{
		prepared: memory.Snapshot{History: committed[:1]},
		updated:  memory.Snapshot{History: committed},
	}
	agent := &fakeAgent{result: orchestrator.Result{Reply: "reply", History: committed}}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := session.Respond(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	result.History[2].ToolCalls[0].Function.Arguments[2] = 'X'
	if string(manager.updated.History[2].ToolCalls[0].Function.Arguments) != `{"query":"safe"}` {
		t.Fatal("returned history aliases committed memory")
	}
}

func TestConversationErrorUnwrapsCause(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{Code: ErrorAgent, Err: cause}
	if !errors.Is(err, cause) || err.Error() == "" {
		t.Fatalf("error does not preserve cause: %v", err)
	}
}

type fakeMemory struct {
	prepared     memory.Snapshot
	updated      memory.Snapshot
	prepareErr   error
	updateErr    error
	prepareCalls int
	updateCalls  int
}

func (m *fakeMemory) Prepare(context.Context, string) (memory.Snapshot, error) {
	m.prepareCalls++
	return m.prepared, m.prepareErr
}

func (m *fakeMemory) Update(context.Context, []llm.Message) (memory.Snapshot, error) {
	m.updateCalls++
	return m.updated, m.updateErr
}

type fakeAgent struct {
	calls  int
	result orchestrator.Result
	err    error
}

func (a *fakeAgent) Run(_ context.Context, _ orchestrator.Request) (orchestrator.Result, error) {
	a.calls++
	return a.result, a.err
}
