package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/dfc-coder/xarlatan/internal/llm"
)

// Prepare compacts stored memory before a model call so the emitted history
// plus the prospective user message fits MaxHistoryBytes. The returned History
// does not include userText; the model adapter remains responsible for adding it.
func (m *Manager) Prepare(ctx context.Context, userText string) (Snapshot, error) {
	if m == nil {
		return Snapshot{}, memoryError(ErrorInvalidConfig, "manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, contextMemoryError(err)
	}

	prospective := llm.Message{Role: "user", Content: userText}
	m.mu.Lock()
	defer m.mu.Unlock()

	previousSummary := m.summary
	kept := cloneTurns(m.turns)
	inputHistory := m.buildHistory(previousSummary, kept)
	inputPrompt := append(cloneMessages(inputHistory), prospective)
	inputBytes, err := Measure(inputPrompt)
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure prospective prompt: %w", err)}
	}
	if inputBytes <= m.config.MaxHistoryBytes {
		trace := Trace{
			InputMessages:  len(inputPrompt),
			InputBytes:     inputBytes,
			OutputMessages: len(inputPrompt),
			OutputBytes:    inputBytes,
			SummaryBytes:   len([]byte(previousSummary)),
		}
		return Snapshot{History: cloneMessages(inputHistory), Summary: previousSummary, Trace: trace}, nil
	}

	var dropped [][]llm.Message
	useSummary := strings.TrimSpace(previousSummary) != "" || m.summarizer != nil
	reservedSummary := ""
	if useSummary {
		reservedSummary = strings.Repeat("x", m.config.MaxSummaryBytes)
	}

	for len(kept) > 0 {
		candidate := append(m.buildHistory(reservedSummary, kept), prospective)
		candidateBytes, measureErr := Measure(candidate)
		if measureErr != nil {
			return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure prepared prompt: %w", measureErr)}
		}
		if candidateBytes <= m.config.MaxHistoryBytes {
			break
		}
		dropped = append(dropped, kept[0])
		kept = kept[1:]
	}

	baseWithReserve := append(m.buildHistory(reservedSummary, kept), prospective)
	baseWithReserveBytes, err := Measure(baseWithReserve)
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure summary reserve: %w", err)}
	}
	if baseWithReserveBytes > m.config.MaxHistoryBytes {
		useSummary = false
		reservedSummary = ""
	}

	for len(kept) > 0 {
		candidate := append(m.buildHistory(reservedSummary, kept), prospective)
		candidateBytes, measureErr := Measure(candidate)
		if measureErr != nil {
			return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure prepared prompt: %w", measureErr)}
		}
		if candidateBytes <= m.config.MaxHistoryBytes {
			break
		}
		dropped = append(dropped, kept[0])
		kept = kept[1:]
	}

	newSummary := previousSummary
	if !useSummary {
		newSummary = ""
	} else if len(dropped) > 0 {
		newSummary = ""
		if m.summarizer != nil {
			newSummary, err = m.summarizer.Summarize(ctx, previousSummary, flattenTurns(dropped), m.config.MaxSummaryBytes)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return Snapshot{}, contextMemoryError(ctxErr)
				}
				return Snapshot{}, &MemoryError{Code: ErrorSummarizer, Err: err}
			}
			newSummary = truncateUTF8(strings.TrimSpace(newSummary), m.config.MaxSummaryBytes)
		}
	}

	history := m.buildHistory(newSummary, kept)
	outputPrompt := append(cloneMessages(history), prospective)
	outputBytes, err := Measure(outputPrompt)
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure prepared output: %w", err)}
	}
	if outputBytes > m.config.MaxHistoryBytes {
		return Snapshot{}, &MemoryError{
			Code: ErrorBudgetExceeded,
			Err:  fmt.Errorf("system prompt and current input require %d bytes, budget is %d", outputBytes, m.config.MaxHistoryBytes),
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, contextMemoryError(err)
	}

	m.summary = newSummary
	m.turns = cloneTurns(kept)
	trace := Trace{
		InputMessages:    len(inputPrompt),
		InputBytes:       inputBytes,
		OutputMessages:   len(outputPrompt),
		OutputBytes:      outputBytes,
		DroppedTurns:     len(dropped),
		DroppedMessages:  countTurnMessages(dropped),
		SummaryGenerated: len(dropped) > 0 && newSummary != "",
		SummaryBytes:     len([]byte(newSummary)),
	}
	return Snapshot{History: cloneMessages(history), Summary: newSummary, Trace: trace}, nil
}
