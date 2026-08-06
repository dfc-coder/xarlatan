package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

type spyObserver struct {
	mu       sync.Mutex
	starts   []NodeStartEvent
	finishes []NodeFinishEvent
	tools    []ToolCallEvent
}

func (o *spyObserver) OnNodeStart(event NodeStartEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.starts = append(o.starts, event)
}

func (o *spyObserver) OnNodeFinish(event NodeFinishEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finishes = append(o.finishes, event)
}

func (o *spyObserver) OnToolCall(event ToolCallEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tools = append(o.tools, event)
}

func (o *spyObserver) finishNodes() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	nodes := make([]string, len(o.finishes))
	for i, event := range o.finishes {
		nodes[i] = event.Node
	}
	return nodes
}

func TestSlogObserver_LogsNodeStartAndFinish(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	orch := NewWithObserver("router", SlogObserver{Logger: logger}, Router{}, ResponseComposer{}, Finalizer{})
	_, err := orch.Run(context.Background(), State{Input: "dime la hora"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "orchestrator node start") {
		t.Fatalf("logs missing node start event: %s", out)
	}
	if !strings.Contains(out, "orchestrator node finish") {
		t.Fatalf("logs missing node finish event: %s", out)
	}
	if !strings.Contains(out, "node=router") {
		t.Fatalf("logs missing router node: %s", out)
	}
}

func TestMetricsObserver_CountsTransitionsAndToolCalls(t *testing.T) {
	metrics := NewMetricsObserver()
	r := tools.NewRegistry(tools.AllowAllToolPolicy())
	r.Register(echoTool{})
	orch := NewWithObserver(
		"router",
		metrics,
		Router{},
		ResponseComposer{},
		ToolExecutor{Executor: tools.NewExecutor(r), Observer: metrics},
		Finalizer{},
	)
	_, err := orch.Run(context.Background(), State{
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
	snap := metrics.Snapshot()
	if got, want := snap.NodeRuns["router"], 1; got != want {
		t.Fatalf("router runs = %d, want %d", got, want)
	}
	if got, want := snap.NodeRuns["tool_executor"], 1; got != want {
		t.Fatalf("tool_executor runs = %d, want %d", got, want)
	}
	if got, want := snap.Transitions["router->response_composer"], 1; got != want {
		t.Fatalf("transition router->response_composer = %d, want %d", got, want)
	}
	if got, want := snap.Transitions["response_composer->tool_executor"], 1; got != want {
		t.Fatalf("transition response_composer->tool_executor = %d, want %d", got, want)
	}
	if got, want := snap.ToolCalls["echo"], 1; got != want {
		t.Fatalf("echo tool calls = %d, want %d", got, want)
	}
	if got, want := snap.ToolFailures["echo"], 0; got != want {
		t.Fatalf("echo tool failures = %d, want %d", got, want)
	}
}

func TestFlow_DebugTraceShowsFullRoute(t *testing.T) {
	observer := &spyObserver{}
	r := tools.NewRegistry(tools.AllowAllToolPolicy())
	r.Register(echoTool{})
	orch := NewWithObserver(
		"router",
		observer,
		Router{},
		ResponseComposer{},
		ToolExecutor{Executor: tools.NewExecutor(r), Observer: observer},
		Finalizer{},
	)
	state, err := orch.Run(context.Background(), State{
		Input:   "dime algo",
		Summary: "resumen previo",
		ToolCalls: []tools.ToolCall{{
			ID:   "call-42",
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
	if !state.Done {
		t.Fatal("Done = false, want true")
	}
	if got, want := observer.finishNodes(), []string{"router", "response_composer", "tool_executor", "response_composer", "finalizer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("finish node trace = %v, want %v", got, want)
	}
	if got, want := len(observer.tools), 1; got != want {
		t.Fatalf("tool trace count = %d, want %d", got, want)
	}
	if got, want := observer.tools[0].CallID, "call-42"; got != want {
		t.Fatalf("tool trace call id = %q, want %q", got, want)
	}
}
