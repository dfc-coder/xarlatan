// Package orchestrator owns the assistant's LLM and tool execution loop.
package orchestrator

import (
	"log/slog"
	"sync"
	"time"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

// StopReason identifies why an agent turn stopped.
type StopReason string

const (
	StopDirectReply      StopReason = "direct_reply"
	StopRoundLimit       StopReason = "round_limit"
	StopModelError       StopReason = "model_error"
	StopCancelled        StopReason = "cancelled"
	StopDeadlineExceeded StopReason = "deadline_exceeded"
)

// ToolTrace records one ordered tool execution inside a model round.
type ToolTrace struct {
	CallID    string
	Tool      string
	Success   bool
	ErrorCode tools.ExecutionErrorCode
	Duration  time.Duration
}

// RoundTrace records one model call and the tools requested by its assistant message.
type RoundTrace struct {
	Number        int
	ModelDuration time.Duration
	Duration      time.Duration
	Tools         []ToolTrace
	StopReason    StopReason
	Error         string
}

// Trace is returned with every completed or partial agent turn.
type Trace struct {
	Rounds     []RoundTrace
	ToolRounds int
	StopReason StopReason
}

// RoundObserver receives immutable round events.
type RoundObserver interface {
	OnRound(round RoundTrace)
}

type nopRoundObserver struct{}

func (nopRoundObserver) OnRound(RoundTrace) {}

// SlogObserver writes round traces using slog.
type SlogObserver struct {
	Logger *slog.Logger
}

func (o SlogObserver) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// OnRound logs model and tool timing without message content or arguments.
func (o SlogObserver) OnRound(round RoundTrace) {
	logger := o.logger()
	logger.Debug(
		"agent round",
		"round", round.Number,
		"model_ms", round.ModelDuration.Milliseconds(),
		"duration_ms", round.Duration.Milliseconds(),
		"tool_calls", len(round.Tools),
		"stop_reason", round.StopReason,
		"error", round.Error,
	)
	for _, tool := range round.Tools {
		logger.Debug(
			"agent tool",
			"round", round.Number,
			"tool", tool.Tool,
			"call_id", tool.CallID,
			"ok", tool.Success,
			"error_code", tool.ErrorCode,
			"duration_ms", tool.Duration.Milliseconds(),
		)
	}
}

// MetricsSnapshot is a copy of runtime counters safe for assertions/debugging.
type MetricsSnapshot struct {
	Rounds       int
	ToolRounds   int
	ToolCalls    map[string]int
	ToolFailures map[string]int
	Stops        map[StopReason]int
}

// MetricsObserver tracks runtime counters.
type MetricsObserver struct {
	mu           sync.Mutex
	rounds       int
	toolRounds   int
	toolCalls    map[string]int
	toolFailures map[string]int
	stops        map[StopReason]int
}

// NewMetricsObserver creates an empty metrics collector.
func NewMetricsObserver() *MetricsObserver {
	return &MetricsObserver{
		toolCalls:    make(map[string]int),
		toolFailures: make(map[string]int),
		stops:        make(map[StopReason]int),
	}
}

// OnRound increments round, tool and stop counters.
func (m *MetricsObserver) OnRound(round RoundTrace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rounds++
	if len(round.Tools) > 0 {
		m.toolRounds++
	}
	for _, tool := range round.Tools {
		m.toolCalls[tool.Tool]++
		if !tool.Success {
			m.toolFailures[tool.Tool]++
		}
	}
	if round.StopReason != "" {
		m.stops[round.StopReason]++
	}
}

// Snapshot returns a defensive copy.
func (m *MetricsObserver) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MetricsSnapshot{
		Rounds:       m.rounds,
		ToolRounds:   m.toolRounds,
		ToolCalls:    cloneStringCounter(m.toolCalls),
		ToolFailures: cloneStringCounter(m.toolFailures),
		Stops:        cloneStopCounter(m.stops),
	}
}

func cloneStringCounter(counter map[string]int) map[string]int {
	out := make(map[string]int, len(counter))
	for key, value := range counter {
		out[key] = value
	}
	return out
}

func cloneStopCounter(counter map[StopReason]int) map[StopReason]int {
	out := make(map[StopReason]int, len(counter))
	for key, value := range counter {
		out[key] = value
	}
	return out
}
