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

type streamingFakeAgent struct {
	fakeAgent
	deltas      []string
	streamCalls int
	streamErr   error
}

func (a *streamingFakeAgent) RunStream(_ context.Context, _ orchestrator.Request, onDelta llm.ContentDelta) (orchestrator.Result, error) {
	a.streamCalls++
	for _, delta := range a.deltas {
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return orchestrator.Result{}, err
			}
		}
	}
	if a.streamErr != nil {
		return orchestrator.Result{}, a.streamErr
	}
	return a.result, a.err
}

func TestSessionRespondStreamForwardsDeltasAndCommitsFinalHistory(t *testing.T) {
	committed := []llm.Message{
		{Role: "system", Content: "trusted"},
		{Role: "user", Content: "hola"},
		{Role: "assistant", Content: "Primera. Segunda."},
	}
	manager := &fakeMemory{
		prepared: memory.Snapshot{History: committed[:1]},
		updated:  memory.Snapshot{History: committed},
	}
	agent := &streamingFakeAgent{
		fakeAgent: fakeAgent{result: orchestrator.Result{Reply: "Primera. Segunda.", History: committed}},
		deltas:    []string{"Primera. ", "Segunda."},
	}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var deltas []string
	result, err := session.RespondStream(context.Background(), "hola", func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if !reflect.DeepEqual(deltas, agent.deltas) {
		t.Fatalf("deltas = %v", deltas)
	}
	if agent.streamCalls != 1 || agent.calls != 0 {
		t.Fatalf("agent calls stream=%d buffered=%d", agent.streamCalls, agent.calls)
	}
	if manager.prepareCalls != 1 || manager.updateCalls != 1 {
		t.Fatalf("memory calls prepare=%d update=%d", manager.prepareCalls, manager.updateCalls)
	}
	if result.Reply != "Primera. Segunda." || !reflect.DeepEqual(result.History, committed) {
		t.Fatalf("result = %+v", result)
	}
}

func TestSessionRespondStreamDoesNotCommitWhenStreamFails(t *testing.T) {
	manager := &fakeMemory{prepared: memory.Snapshot{History: []llm.Message{{Role: "system", Content: "trusted"}}}}
	agent := &streamingFakeAgent{deltas: []string{"parcial"}, streamErr: errors.New("model stream failed")}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	seen := false
	_, err = session.RespondStream(context.Background(), "hola", func(string) error {
		seen = true
		return nil
	})
	if !IsErrorCode(err, ErrorAgent) {
		t.Fatalf("RespondStream() error = %v, want agent failure", err)
	}
	if !seen {
		t.Fatal("expected partial delta before stream failure")
	}
	if manager.updateCalls != 0 {
		t.Fatalf("memory update calls = %d, want 0", manager.updateCalls)
	}
}

func TestSessionRespondStreamFallsBackToBufferedAgent(t *testing.T) {
	committed := []llm.Message{{Role: "user", Content: "hola"}, {Role: "assistant", Content: "reply"}}
	manager := &fakeMemory{prepared: memory.Snapshot{}, updated: memory.Snapshot{History: committed}}
	agent := &fakeAgent{result: orchestrator.Result{Reply: "reply", History: committed}}
	session, err := New(manager, agent)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	called := false

	result, err := session.RespondStream(context.Background(), "hola", func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if called || agent.calls != 1 || result.Reply != "reply" {
		t.Fatalf("called=%v agent.calls=%d reply=%q", called, agent.calls, result.Reply)
	}
}
