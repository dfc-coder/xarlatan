// Package conversation coordinates bounded memory with the single AgentRuntime.
package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/memory"
	"github.com/dfc-coder/xarlatan/internal/orchestrator"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

// Memory is the bounded-memory contract required by Session.
type Memory interface {
	Prepare(context.Context, string) (memory.Snapshot, error)
	Update(context.Context, []llm.Message) (memory.Snapshot, error)
}

// Agent is the single conversational runtime contract required by Session.
type Agent interface {
	Run(context.Context, orchestrator.Request) (orchestrator.Result, error)
}

// ErrorCode classifies session failures without conversation content.
type ErrorCode string

const (
	ErrorInvalidSession ErrorCode = "invalid_session"
	ErrorMemoryPrepare  ErrorCode = "memory_prepare_failed"
	ErrorAgent          ErrorCode = "agent_failed"
	ErrorMemoryUpdate   ErrorCode = "memory_update_failed"
	ErrorCancelled      ErrorCode = "cancelled"
	ErrorDeadline       ErrorCode = "deadline_exceeded"
)

// Error preserves a stable code and wrapped cause.
type Error struct {
	Code ErrorCode
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "conversation error"
	}
	if e.Err == nil {
		return "conversation " + string(e.Code)
	}
	return fmt.Sprintf("conversation %s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsErrorCode reports whether err contains the requested stable code.
func IsErrorCode(err error, code ErrorCode) bool {
	var sessionErr *Error
	return errors.As(err, &sessionErr) && sessionErr.Code == code
}

// Result is published only after memory accepts the completed turn.
type Result struct {
	Reply       string
	History     []llm.Message
	AgentTrace  orchestrator.Trace
	MemoryTrace memory.Trace
}

// Session owns the memory -> agent -> memory transaction for one conversation.
type Session struct {
	memory Memory
	agent  Agent
}

// New validates and constructs a Session.
func New(manager Memory, agent Agent) (*Session, error) {
	if manager == nil {
		return nil, &Error{Code: ErrorInvalidSession, Err: fmt.Errorf("memory is nil")}
	}
	if agent == nil {
		return nil, &Error{Code: ErrorInvalidSession, Err: fmt.Errorf("agent is nil")}
	}
	return &Session{memory: manager, agent: agent}, nil
}

// Respond executes exactly one AgentRuntime turn and publishes only committed
// memory. Partial agent history is never returned on failure.
func (s *Session) Respond(ctx context.Context, text string) (Result, error) {
	if s == nil || s.memory == nil || s.agent == nil {
		return Result{}, &Error{Code: ErrorInvalidSession, Err: fmt.Errorf("session is not initialized")}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, contextError(err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Result{}, nil
	}

	prepared, err := s.memory.Prepare(ctx, text)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, contextError(ctxErr)
		}
		return Result{}, &Error{Code: ErrorMemoryPrepare, Err: err}
	}

	turn, err := s.agent.Run(ctx, orchestrator.Request{Input: text, History: prepared.History})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, contextError(ctxErr)
		}
		return Result{}, &Error{Code: ErrorAgent, Err: err}
	}

	committed, err := s.memory.Update(ctx, turn.History)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, contextError(ctxErr)
		}
		return Result{}, &Error{Code: ErrorMemoryUpdate, Err: err}
	}
	return Result{
		Reply:       strings.TrimSpace(turn.Reply),
		History:     cloneMessages(committed.History),
		AgentTrace:  turn.Trace,
		MemoryTrace: committed.Trace,
	}, nil
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrorDeadline, Err: context.DeadlineExceeded}
	}
	return &Error{Code: ErrorCancelled, Err: context.Canceled}
}

func cloneMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]llm.Message, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].ToolCalls = cloneToolCalls(message.ToolCalls)
	}
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
