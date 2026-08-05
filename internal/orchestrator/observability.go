// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// NodeStartEvent marks the beginning of a node execution.
type NodeStartEvent struct {
	Node string
}

// NodeFinishEvent marks the end of a node execution.
type NodeFinishEvent struct {
	Node     string
	NextNode string
	Done     bool
	Duration time.Duration
	Error    string
}

// ToolCallEvent traces a tool execution done by ToolExecutor.
type ToolCallEvent struct {
	Node     string
	Tool     string
	CallID   string
	Duration time.Duration
	Success  bool
	Error    string
}

// Observer receives execution events emitted by the orchestrator.
type Observer interface {
	OnNodeStart(event NodeStartEvent)
	OnNodeFinish(event NodeFinishEvent)
	OnToolCall(event ToolCallEvent)
}

type nopObserver struct{}

func (nopObserver) OnNodeStart(NodeStartEvent)   {}
func (nopObserver) OnNodeFinish(NodeFinishEvent) {}
func (nopObserver) OnToolCall(ToolCallEvent)     {}

// SlogObserver writes orchestrator traces using slog.
type SlogObserver struct {
	Logger *slog.Logger
}

func (o SlogObserver) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// OnNodeStart logs the beginning of a node execution.
func (o SlogObserver) OnNodeStart(event NodeStartEvent) {
	o.logger().Debug("orchestrator node start", "node", event.Node)
}

// OnNodeFinish logs node completion and transition details.
func (o SlogObserver) OnNodeFinish(event NodeFinishEvent) {
	args := []any{
		"node", event.Node,
		"next", event.NextNode,
		"done", event.Done,
		"duration_ms", event.Duration.Milliseconds(),
	}
	if event.Error != "" {
		args = append(args, "error", event.Error)
	}
	o.logger().Debug("orchestrator node finish", args...)
}

// OnToolCall logs tool call traces.
func (o SlogObserver) OnToolCall(event ToolCallEvent) {
	args := []any{
		"node", event.Node,
		"tool", event.Tool,
		"call_id", event.CallID,
		"ok", event.Success,
		"duration_ms", event.Duration.Milliseconds(),
	}
	if event.Error != "" {
		args = append(args, "error", event.Error)
	}
	o.logger().Debug("orchestrator tool call", args...)
}

// MetricsSnapshot captures counters for transitions and tool executions.
type MetricsSnapshot struct {
	NodeRuns     map[string]int
	Transitions  map[string]int
	ToolCalls    map[string]int
	ToolFailures map[string]int
}

// MetricsObserver tracks node and tool execution counters.
type MetricsObserver struct {
	mu       sync.Mutex
	nodeRuns map[string]int
	links    map[string]int
	tools    map[string]int
	failures map[string]int
}

// NewMetricsObserver creates an empty metrics collector.
func NewMetricsObserver() *MetricsObserver {
	return &MetricsObserver{
		nodeRuns: make(map[string]int),
		links:    make(map[string]int),
		tools:    make(map[string]int),
		failures: make(map[string]int),
	}
}

// OnNodeStart is intentionally empty; counters are finalized on finish.
func (m *MetricsObserver) OnNodeStart(NodeStartEvent) {}

// OnNodeFinish increments per-node and transition counters.
func (m *MetricsObserver) OnNodeFinish(event NodeFinishEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeRuns[event.Node]++
	m.links[transitionKey(event.Node, event.NextNode)]++
}

// OnToolCall increments tool counters and failures.
func (m *MetricsObserver) OnToolCall(event ToolCallEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools[event.Tool]++
	if !event.Success {
		m.failures[event.Tool]++
	}
}

// Snapshot returns a copy safe for assertions/debugging.
func (m *MetricsObserver) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MetricsSnapshot{
		NodeRuns:     cloneCounter(m.nodeRuns),
		Transitions:  cloneCounter(m.links),
		ToolCalls:    cloneCounter(m.tools),
		ToolFailures: cloneCounter(m.failures),
	}
}

func transitionKey(from, to string) string {
	return fmt.Sprintf("%s->%s", from, to)
}

func cloneCounter(counter map[string]int) map[string]int {
	out := make(map[string]int, len(counter))
	for k, v := range counter {
		out[k] = v
	}
	return out
}
