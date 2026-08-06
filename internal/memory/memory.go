// Package memory maintains bounded, role-safe conversation history.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

const summaryPrefix = "[memory-summary]\n"

// ErrorCode classifies stable memory failures.
type ErrorCode string

const (
	ErrorInvalidConfig  ErrorCode = "invalid_config"
	ErrorInvalidHistory ErrorCode = "invalid_history"
	ErrorBudgetExceeded ErrorCode = "budget_exceeded"
	ErrorSummarizer     ErrorCode = "summarizer_error"
	ErrorCancelled      ErrorCode = "cancelled"
)

// MemoryError preserves a stable code and wrapped cause without embedding
// conversation content in the error text.
type MemoryError struct {
	Code ErrorCode
	Err  error
}

func (e *MemoryError) Error() string {
	if e == nil {
		return "memory error"
	}
	if e.Err == nil {
		return "memory " + string(e.Code)
	}
	return fmt.Sprintf("memory %s: %v", e.Code, e.Err)
}

func (e *MemoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsErrorCode reports whether err contains the requested stable code.
func IsErrorCode(err error, code ErrorCode) bool {
	var memoryErr *MemoryError
	return errors.As(err, &memoryErr) && memoryErr.Code == code
}

// Config defines hard JSON-byte budgets for emitted history and summary text.
type Config struct {
	MaxHistoryBytes int
	MaxSummaryBytes int
}

// Summarizer replaces the previous summary after complete turns are evicted.
type Summarizer interface {
	Summarize(ctx context.Context, previous string, dropped []llm.Message, maxBytes int) (string, error)
}

// Snapshot is the immutable result of one memory update.
type Snapshot struct {
	History []llm.Message
	Summary string
	Trace   Trace
}

// Trace contains sizes and eviction counts without conversation content.
type Trace struct {
	InputMessages    int
	InputBytes       int
	OutputMessages   int
	OutputBytes      int
	DroppedTurns     int
	DroppedMessages  int
	SummaryGenerated bool
	SummaryBytes     int
}

// Manager owns the trusted system prompt, bounded summary and recent turns.
type Manager struct {
	mu           sync.RWMutex
	config       Config
	systemPrompt string
	summarizer   Summarizer
	summary      string
	turns        [][]llm.Message
}

// New validates configuration and constructs an empty memory state.
func New(config Config, systemPrompt string, summarizer Summarizer) (*Manager, error) {
	if config.MaxHistoryBytes <= 0 {
		return nil, memoryError(ErrorInvalidConfig, "max history bytes must be greater than zero")
	}
	if config.MaxSummaryBytes <= 0 {
		return nil, memoryError(ErrorInvalidConfig, "max summary bytes must be greater than zero")
	}
	if config.MaxSummaryBytes >= config.MaxHistoryBytes {
		return nil, memoryError(ErrorInvalidConfig, "max summary bytes must be less than max history bytes")
	}
	manager := &Manager{config: config, systemPrompt: systemPrompt, summarizer: summarizer}
	baseBytes, err := Measure(manager.buildHistory("", nil))
	if err != nil {
		return nil, &MemoryError{Code: ErrorInvalidConfig, Err: fmt.Errorf("measure system prompt: %w", err)}
	}
	if baseBytes > config.MaxHistoryBytes {
		return nil, &MemoryError{Code: ErrorBudgetExceeded, Err: fmt.Errorf("system prompt requires %d bytes, budget is %d", baseBytes, config.MaxHistoryBytes)}
	}
	if summarizer != nil {
		reserved := manager.buildHistory(strings.Repeat("x", config.MaxSummaryBytes), nil)
		reservedBytes, measureErr := Measure(reserved)
		if measureErr != nil {
			return nil, &MemoryError{Code: ErrorInvalidConfig, Err: fmt.Errorf("measure summary reserve: %w", measureErr)}
		}
		if reservedBytes > config.MaxHistoryBytes {
			return nil, memoryError(ErrorInvalidConfig, "history budget cannot contain configured summary reserve")
		}
	}
	return manager, nil
}

// History returns a defensive copy of the current bounded prompt history.
func (m *Manager) History() []llm.Message {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneMessages(m.buildHistory(m.summary, m.turns))
}

// Reset removes summary and turns while retaining configuration and system prompt.
func (m *Manager) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.summary = ""
	m.turns = nil
	m.mu.Unlock()
}

// Update validates completed history, evicts oldest complete turns and
// atomically publishes a bounded snapshot.
func (m *Manager) Update(ctx context.Context, history []llm.Message) (Snapshot, error) {
	if m == nil {
		return Snapshot{}, memoryError(ErrorInvalidConfig, "manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, contextMemoryError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	inputBytes, err := Measure(history)
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure input history: %w", err)}
	}
	conversation, err := m.conversationMessages(history)
	if err != nil {
		return Snapshot{}, err
	}
	turns, err := splitTurns(conversation)
	if err != nil {
		return Snapshot{}, err
	}

	previousSummary := m.summary
	kept := cloneTurns(turns)
	candidateBytes, err := Measure(m.buildHistory(previousSummary, kept))
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure candidate history: %w", err)}
	}

	var dropped [][]llm.Message
	if candidateBytes > m.config.MaxHistoryBytes {
		reservedSummary := ""
		if m.summarizer != nil {
			reservedSummary = strings.Repeat("x", m.config.MaxSummaryBytes)
		}
		for len(kept) > 0 {
			reservedBytes, measureErr := Measure(m.buildHistory(reservedSummary, kept))
			if measureErr != nil {
				return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure reserved history: %w", measureErr)}
			}
			if reservedBytes <= m.config.MaxHistoryBytes {
				break
			}
			dropped = append(dropped, kept[0])
			kept = kept[1:]
		}
	}

	newSummary := previousSummary
	if len(dropped) > 0 {
		newSummary = ""
		if m.summarizer != nil {
			if err := ctx.Err(); err != nil {
				return Snapshot{}, contextMemoryError(err)
			}
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

	output := m.buildHistory(newSummary, kept)
	outputBytes, err := Measure(output)
	if err != nil {
		return Snapshot{}, &MemoryError{Code: ErrorInvalidHistory, Err: fmt.Errorf("measure output history: %w", err)}
	}
	if outputBytes > m.config.MaxHistoryBytes {
		return Snapshot{}, &MemoryError{Code: ErrorBudgetExceeded, Err: fmt.Errorf("bounded history requires %d bytes, budget is %d", outputBytes, m.config.MaxHistoryBytes)}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, contextMemoryError(err)
	}

	m.summary = newSummary
	m.turns = cloneTurns(kept)
	trace := Trace{
		InputMessages:    len(history),
		InputBytes:       inputBytes,
		OutputMessages:   len(output),
		OutputBytes:      outputBytes,
		DroppedTurns:     len(dropped),
		DroppedMessages:  countTurnMessages(dropped),
		SummaryGenerated: len(dropped) > 0 && newSummary != "",
		SummaryBytes:     len([]byte(newSummary)),
	}
	return Snapshot{History: cloneMessages(output), Summary: newSummary, Trace: trace}, nil
}

// Measure returns the exact JSON payload size of a message slice.
func Measure(messages []llm.Message) (int, error) {
	payload, err := json.Marshal(messages)
	if err != nil {
		return 0, err
	}
	return len(payload), nil
}

// ExtractiveSummarizer is local and deterministic; it performs no LLM call.
type ExtractiveSummarizer struct{}

func (ExtractiveSummarizer) Summarize(ctx context.Context, previous string, dropped []llm.Message, maxBytes int) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(dropped)+1)
	if previous = strings.TrimSpace(previous); previous != "" {
		parts = append(parts, "previous: "+previous)
	}
	for _, message := range dropped {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		content := strings.TrimSpace(message.Content)
		if len(message.ToolCalls) > 0 {
			names := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				if name := strings.TrimSpace(call.Function.Name); name != "" {
					names = append(names, name)
				}
			}
			if len(names) > 0 {
				content = "tool calls: " + strings.Join(names, ", ")
			}
		}
		if content != "" {
			parts = append(parts, message.Role+": "+content)
		}
	}
	return truncateUTF8(strings.Join(parts, " | "), maxBytes), nil
}

func (m *Manager) conversationMessages(history []llm.Message) ([]llm.Message, error) {
	if len(history) == 0 {
		return nil, memoryError(ErrorInvalidHistory, "history is empty")
	}
	first := history[0]
	if first.Role != "system" || first.Content != m.systemPrompt || first.ToolCallID != "" || len(first.ToolCalls) > 0 {
		return nil, memoryError(ErrorInvalidHistory, "trusted system prompt is missing or changed")
	}
	start := 1
	if start < len(history) && m.summary != "" {
		expected := summaryMessage(m.summary)
		candidate := history[start]
		if candidate.Role == expected.Role && candidate.Content == expected.Content && candidate.ToolCallID == "" && len(candidate.ToolCalls) == 0 {
			start++
		}
	}
	conversation := cloneMessages(history[start:])
	for _, message := range conversation {
		if message.Role == "system" {
			return nil, memoryError(ErrorInvalidHistory, "conversation contains a system message")
		}
	}
	return conversation, nil
}

func splitTurns(messages []llm.Message) ([][]llm.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	turns := make([][]llm.Message, 0)
	var current []llm.Message
	pending := make(map[string]struct{})
	finish := func() error {
		if len(pending) > 0 {
			return memoryError(ErrorInvalidHistory, "tool call is missing a result")
		}
		if len(current) > 0 {
			turns = append(turns, cloneMessages(current))
			current = nil
		}
		return nil
	}

	for _, message := range messages {
		switch message.Role {
		case "user":
			if message.ToolCallID != "" || len(message.ToolCalls) > 0 {
				return nil, memoryError(ErrorInvalidHistory, "user message contains tool fields")
			}
			if len(current) > 0 {
				if err := finish(); err != nil {
					return nil, err
				}
			}
			current = append(current, cloneMessage(message))
		case "assistant":
			if len(current) == 0 {
				return nil, memoryError(ErrorInvalidHistory, "assistant message appears before user")
			}
			if len(pending) > 0 {
				return nil, memoryError(ErrorInvalidHistory, "assistant continued before all tool results")
			}
			if message.ToolCallID != "" {
				return nil, memoryError(ErrorInvalidHistory, "assistant message contains tool_call_id")
			}
			for _, call := range message.ToolCalls {
				id := strings.TrimSpace(call.ID)
				if id == "" {
					return nil, memoryError(ErrorInvalidHistory, "tool call ID is empty")
				}
				if _, exists := pending[id]; exists {
					return nil, memoryError(ErrorInvalidHistory, "tool call ID is duplicated")
				}
				pending[id] = struct{}{}
			}
			current = append(current, cloneMessage(message))
		case "tool":
			if len(current) == 0 {
				return nil, memoryError(ErrorInvalidHistory, "tool message appears before user")
			}
			if len(message.ToolCalls) > 0 {
				return nil, memoryError(ErrorInvalidHistory, "tool message contains tool calls")
			}
			id := strings.TrimSpace(message.ToolCallID)
			if id == "" {
				return nil, memoryError(ErrorInvalidHistory, "tool message has empty tool_call_id")
			}
			if _, exists := pending[id]; !exists {
				return nil, memoryError(ErrorInvalidHistory, "tool result has no matching call")
			}
			delete(pending, id)
			current = append(current, cloneMessage(message))
		default:
			return nil, memoryError(ErrorInvalidHistory, "conversation contains an unsupported role")
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return turns, nil
}

func (m *Manager) buildHistory(summary string, turns [][]llm.Message) []llm.Message {
	capacity := 1 + countTurnMessages(turns)
	if strings.TrimSpace(summary) != "" {
		capacity++
	}
	history := make([]llm.Message, 0, capacity)
	history = append(history, llm.Message{Role: "system", Content: m.systemPrompt})
	if strings.TrimSpace(summary) != "" {
		history = append(history, summaryMessage(summary))
	}
	for _, turn := range turns {
		history = append(history, cloneMessages(turn)...)
	}
	return history
}

func summaryMessage(summary string) llm.Message {
	return llm.Message{Role: "user", Content: summaryPrefix + summary}
}

func flattenTurns(turns [][]llm.Message) []llm.Message {
	messages := make([]llm.Message, 0, countTurnMessages(turns))
	for _, turn := range turns {
		messages = append(messages, cloneMessages(turn)...)
	}
	return messages
}

func countTurnMessages(turns [][]llm.Message) int {
	total := 0
	for _, turn := range turns {
		total += len(turn)
	}
	return total
}

func cloneTurns(turns [][]llm.Message) [][]llm.Message {
	if len(turns) == 0 {
		return nil
	}
	cloned := make([][]llm.Message, len(turns))
	for i, turn := range turns {
		cloned[i] = cloneMessages(turn)
	}
	return cloned
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]llm.Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message llm.Message) llm.Message {
	cloned := message
	cloned.ToolCalls = cloneToolCalls(message.ToolCalls)
	return cloned
}

func cloneToolCalls(calls []tools.ToolCall) []tools.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]tools.ToolCall, len(calls))
	for i, call := range calls {
		cloned[i] = call
		if len(call.Function.Arguments) > 0 {
			cloned[i].Function.Arguments = append([]byte(nil), call.Function.Arguments...)
		}
	}
	return cloned
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || value == "" {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	truncated := value[:maxBytes]
	for truncated != "" && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return strings.TrimSpace(truncated)
}

func memoryError(code ErrorCode, message string) error {
	return &MemoryError{Code: code, Err: errors.New(message)}
}

func contextMemoryError(err error) error {
	return &MemoryError{Code: ErrorCancelled, Err: err}
}
