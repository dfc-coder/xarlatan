package orchestrator

import (
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestMetricsObserverCountsRoundsToolsFailuresAndStops(t *testing.T) {
	observer := NewMetricsObserver()
	observer.OnRound(RoundTrace{
		Number: 1,
		Tools: []ToolTrace{
			{Tool: "alpha", CallID: "a-1", Success: true, Duration: time.Millisecond},
			{Tool: "beta", CallID: "b-1", Success: false, ErrorCode: tools.ExecutionToolError, Duration: time.Millisecond},
		},
	})
	observer.OnRound(RoundTrace{Number: 2, StopReason: StopDirectReply})

	snapshot := observer.Snapshot()
	if got, want := snapshot.Rounds, 2; got != want {
		t.Fatalf("Rounds = %d, want %d", got, want)
	}
	if got, want := snapshot.ToolRounds, 1; got != want {
		t.Fatalf("ToolRounds = %d, want %d", got, want)
	}
	if got, want := snapshot.ToolCalls["alpha"], 1; got != want {
		t.Fatalf("ToolCalls[alpha] = %d, want %d", got, want)
	}
	if got, want := snapshot.ToolCalls["beta"], 1; got != want {
		t.Fatalf("ToolCalls[beta] = %d, want %d", got, want)
	}
	if got, want := snapshot.ToolFailures["beta"], 1; got != want {
		t.Fatalf("ToolFailures[beta] = %d, want %d", got, want)
	}
	if got, want := snapshot.Stops[StopDirectReply], 1; got != want {
		t.Fatalf("Stops[direct_reply] = %d, want %d", got, want)
	}
}

func TestMetricsSnapshotIsDefensiveCopy(t *testing.T) {
	observer := NewMetricsObserver()
	observer.OnRound(RoundTrace{
		Number:     1,
		StopReason: StopRoundLimit,
		Tools: []ToolTrace{{
			Tool:      "alpha",
			CallID:    "a-1",
			Success:   false,
			ErrorCode: tools.ExecutionUnknownTool,
		}},
	})

	first := observer.Snapshot()
	first.ToolCalls["alpha"] = 99
	first.ToolFailures["alpha"] = 99
	first.Stops[StopRoundLimit] = 99

	second := observer.Snapshot()
	if got, want := second.ToolCalls["alpha"], 1; got != want {
		t.Fatalf("ToolCalls[alpha] = %d, want %d", got, want)
	}
	if got, want := second.ToolFailures["alpha"], 1; got != want {
		t.Fatalf("ToolFailures[alpha] = %d, want %d", got, want)
	}
	if got, want := second.Stops[StopRoundLimit], 1; got != want {
		t.Fatalf("Stops[round_limit] = %d, want %d", got, want)
	}
}
