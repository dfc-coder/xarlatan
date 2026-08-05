package memory

import (
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestCompact_TrimsToShortWindowAndSummarizesDroppedTurns(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "reply 1"},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "reply 2"},
		{Role: "user", Content: "turn 3"},
		{Role: "assistant", Content: "reply 3"},
	}

	snap := Compact(history, 1)
	if snap.Summary == "" {
		t.Fatalf("Summary is empty")
	}
	if got, want := snap.Trace.InputMessages, len(history); got != want {
		t.Fatalf("Trace.InputMessages = %d, want %d", got, want)
	}
	if got, want := snap.Trace.DroppedMessages, 4; got != want {
		t.Fatalf("Trace.DroppedMessages = %d, want %d", got, want)
	}
	if !snap.Trace.SummaryGenerated {
		t.Fatalf("Trace.SummaryGenerated = false, want true")
	}
	if got, want := len(snap.Window), 2; got != want {
		t.Fatalf("Window len = %d, want %d", got, want)
	}
	if got, want := snap.Window[0].Content, "turn 3"; got != want {
		t.Fatalf("Window[0] = %q, want %q", got, want)
	}
	if got, want := snap.Window[1].Content, "reply 3"; got != want {
		t.Fatalf("Window[1] = %q, want %q", got, want)
	}
}

func TestCompose_BuildsCompactHistory(t *testing.T) {
	history := Compose("system", "resumen previo", []llm.Message{{Role: "user", Content: "hola"}})
	if got, want := len(history), 3; got != want {
		t.Fatalf("len(history) = %d, want %d", got, want)
	}
	if got, want := history[0].Role, "system"; got != want {
		t.Fatalf("history[0].Role = %q, want %q", got, want)
	}
	if got, want := history[1].Role, "system"; got != want {
		t.Fatalf("history[1].Role = %q, want %q", got, want)
	}
	if got, want := history[1].Content, "Resumen de contexto: resumen previo"; got != want {
		t.Fatalf("history[1].Content = %q, want %q", got, want)
	}
}

func TestCompact_IgnoresEmbeddedSummarySystemMessage(t *testing.T) {
	history := Compose("system", "resumen previo", []llm.Message{
		{Role: "user", Content: "hola"},
		{Role: "assistant", Content: "buenas"},
	})

	snap := Compact(history, 1)
	if got, want := len(snap.Window), 2; got != want {
		t.Fatalf("Window len = %d, want %d", got, want)
	}
	if got, want := snap.Trace.DroppedMessages, 0; got != want {
		t.Fatalf("Trace.DroppedMessages = %d, want %d", got, want)
	}
	if snap.Trace.SummaryGenerated {
		t.Fatalf("Trace.SummaryGenerated = true, want false")
	}
	if got, want := snap.Window[0].Role, "user"; got != want {
		t.Fatalf("Window[0].Role = %q, want %q", got, want)
	}
	if got, want := snap.Window[1].Role, "assistant"; got != want {
		t.Fatalf("Window[1].Role = %q, want %q", got, want)
	}
}

func TestCompactAndCompose_DoesNotCarryFullHistoryForward(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "turn 1"},
		{Role: "assistant", Content: "reply 1"},
		{Role: "user", Content: "turn 2"},
		{Role: "assistant", Content: "reply 2"},
		{Role: "user", Content: "turn 3"},
		{Role: "assistant", Content: "reply 3"},
		{Role: "user", Content: "turn 4"},
		{Role: "assistant", Content: "reply 4"},
	}

	snap := Compact(history, 1)
	if got, want := len(snap.Window), 2; got != want {
		t.Fatalf("Window len = %d, want %d", got, want)
	}
	if got, want := snap.Window[0].Content, "turn 4"; got != want {
		t.Fatalf("Window[0] = %q, want %q", got, want)
	}
	if got, want := snap.Window[1].Content, "reply 4"; got != want {
		t.Fatalf("Window[1] = %q, want %q", got, want)
	}
	if snap.Summary == "" {
		t.Fatalf("Summary is empty")
	}
	if got, want := snap.Trace.WindowMessages, 2; got != want {
		t.Fatalf("Trace.WindowMessages = %d, want %d", got, want)
	}
	if !snap.Trace.SummaryGenerated {
		t.Fatalf("Trace.SummaryGenerated = false, want true")
	}

	compacted := Compose("system", snap.Summary, snap.Window)
	if got, want := len(compacted), 4; got != want {
		t.Fatalf("len(compacted) = %d, want %d", got, want)
	}
	if got, want := compacted[0].Role, "system"; got != want {
		t.Fatalf("compacted[0].Role = %q, want %q", got, want)
	}
	if got, want := compacted[1].Role, "system"; got != want {
		t.Fatalf("compacted[1].Role = %q, want %q", got, want)
	}
	if got, want := compacted[2].Content, "turn 4"; got != want {
		t.Fatalf("compacted[2] = %q, want %q", got, want)
	}
	if got, want := compacted[3].Content, "reply 4"; got != want {
		t.Fatalf("compacted[3] = %q, want %q", got, want)
	}
}

func TestCompact_TraceForEmptyHistory(t *testing.T) {
	snap := Compact(nil, 2)
	if got, want := snap.Trace.InputMessages, 0; got != want {
		t.Fatalf("Trace.InputMessages = %d, want %d", got, want)
	}
	if got, want := snap.Trace.WindowMessages, 0; got != want {
		t.Fatalf("Trace.WindowMessages = %d, want %d", got, want)
	}
	if snap.Trace.SummaryGenerated {
		t.Fatalf("Trace.SummaryGenerated = true, want false")
	}
}
