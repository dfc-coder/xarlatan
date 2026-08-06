package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestMemoryPrepareBoundsNextPrompt(t *testing.T) {
	manager := newTestManager(t, Config{MaxHistoryBytes: 360, MaxSummaryBytes: 64}, nil)
	history := manager.History()
	for i := 0; i < 4; i++ {
		history = append(history,
			llm.Message{Role: "user", Content: "question " + strings.Repeat("x", 35)},
			llm.Message{Role: "assistant", Content: "answer " + strings.Repeat("y", 35)},
		)
	}
	if _, err := manager.Update(context.Background(), history); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	input := "current " + strings.Repeat("z", 90)
	snapshot, err := manager.Prepare(context.Background(), input)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	prompt := append(snapshot.History, llm.Message{Role: "user", Content: input})
	assertWithinBudget(t, prompt, 360)
	if snapshot.Trace.OutputBytes != measuredBytes(t, prompt) {
		t.Fatalf("OutputBytes = %d, want %d", snapshot.Trace.OutputBytes, measuredBytes(t, prompt))
	}
}

func TestMemoryPrepareRejectsInputLargerThanBudget(t *testing.T) {
	manager := newTestManager(t, Config{MaxHistoryBytes: 180, MaxSummaryBytes: 32}, nil)
	before := historyText(manager.History())

	_, err := manager.Prepare(context.Background(), strings.Repeat("x", 500))
	if !IsErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Prepare() error = %v, want %s", err, ErrorBudgetExceeded)
	}
	if after := historyText(manager.History()); after != before {
		t.Fatalf("state changed after rejected input: %q != %q", after, before)
	}
}

func TestMemoryPrepareHonorsCancellation(t *testing.T) {
	manager := newTestManager(t, Config{MaxHistoryBytes: 360, MaxSummaryBytes: 64}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := manager.Prepare(ctx, "question")
	if !IsErrorCode(err, ErrorCancelled) {
		t.Fatalf("Prepare() error = %v, want %s", err, ErrorCancelled)
	}
}
