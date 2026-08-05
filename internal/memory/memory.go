// Package memory compacts conversation context into a summary and short window.
package memory

import (
	"strings"

	"github.com/dfc-coder/xarlatan/internal/llm"
)

// Snapshot is the compacted conversation state used between turns.
type Snapshot struct {
	Summary string
	Window  []llm.Message
	Trace   Trace
}

// Trace carries compacting metadata for debugging and observability.
type Trace struct {
	InputMessages    int
	DroppedMessages  int
	WindowMessages   int
	SummaryGenerated bool
}

// Compact trims a conversation to a short window and returns a summary of the
// dropped turns.
func Compact(history []llm.Message, maxPairs int) Snapshot {
	if len(history) == 0 {
		return Snapshot{Trace: Trace{}}
	}
	if maxPairs < 0 {
		maxPairs = 0
	}
	keepPrefix := 1
	if len(history) > 1 && history[1].Role == "system" {
		keepPrefix = 2
	}
	keep := maxPairs * 2
	if len(history) <= keepPrefix+keep {
		window := cloneMessages(history[keepPrefix:])
		return Snapshot{
			Window: window,
			Trace: Trace{
				InputMessages:    len(history),
				DroppedMessages:  0,
				WindowMessages:   len(window),
				SummaryGenerated: false,
			},
		}
	}

	dropped := history[keepPrefix : len(history)-keep]
	summary := summarize(dropped)
	window := cloneMessages(history[len(history)-keep:])
	return Snapshot{
		Summary: summary,
		Window:  window,
		Trace: Trace{
			InputMessages:    len(history),
			DroppedMessages:  len(dropped),
			WindowMessages:   len(window),
			SummaryGenerated: strings.TrimSpace(summary) != "",
		},
	}
}

// Compose rebuilds the history from the system prompt, a compact summary, and
// the short window kept in memory.
func Compose(systemPrompt, summary string, window []llm.Message) []llm.Message {
	history := make([]llm.Message, 0, 2+len(window))
	history = append(history, llm.Message{Role: "system", Content: systemPrompt})
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		history = append(history, llm.Message{Role: "system", Content: "Resumen de contexto: " + trimmed})
	}
	history = append(history, cloneMessages(window)...)
	return history
}

// MergeSummary appends new information to an existing summary.
func MergeSummary(existing, added string) string {
	existing = strings.TrimSpace(existing)
	added = strings.TrimSpace(added)
	switch {
	case existing == "":
		return added
	case added == "":
		return existing
	default:
		return existing + "\n" + added
	}
}

func summarize(messages []llm.Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		parts = append(parts, msg.Role+": "+content)
	}
	return strings.Join(parts, " | ")
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]llm.Message, len(messages))
	copy(cloned, messages)
	return cloned
}
