package conversation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
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
	if err == nil {
		t.Fatal("Respond() error = nil, want failure")
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
	if err == nil {
		t.Fatal("Respond() error = nil, want update failure")
	}
	if result.Reply != "" || len(result.History) != 0 {
		t.Fatalf("uncommitted result returned: %+v", result)
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
