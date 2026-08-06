// Package orchestrator owns the assistant's LLM and tool execution loop.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dfc-coder/xarlatan/internal/llm"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

// Generator is the minimal LLM contract required by AgentRuntime.
type Generator interface {
	Generate(
		ctx context.Context,
		history []llm.Message,
		userText string,
		registry *tools.Registry,
	) (reply string, toolLog string, nextHistory []llm.Message, err error)
}

// RuntimeConfig bounds the agent loop.
type RuntimeConfig struct {
	MaxToolRounds int
}

// Request starts one agent turn from an existing conversation history.
type Request struct {
	Input   string
	History []llm.Message
}

// Result is the completed or partial state of an agent turn.
type Result struct {
	Reply   string
	History []llm.Message
	Trace   Trace
}

// RuntimeErrorCode classifies terminal runtime failures.
type RuntimeErrorCode string

const (
	RuntimeInvalidRuntime   RuntimeErrorCode = "invalid_runtime"
	RuntimeModelError       RuntimeErrorCode = "model_error"
	RuntimeRoundLimit       RuntimeErrorCode = "round_limit"
	RuntimeCancelled        RuntimeErrorCode = "cancelled"
	RuntimeDeadlineExceeded RuntimeErrorCode = "deadline_exceeded"
)

// RuntimeError preserves a stable code, round and wrapped cause.
type RuntimeError struct {
	Code  RuntimeErrorCode
	Round int
	Err   error
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "agent runtime error"
	}
	if e.Err == nil {
		return fmt.Sprintf("agent runtime %s at round %d", e.Code, e.Round)
	}
	return fmt.Sprintf("agent runtime %s at round %d: %v", e.Code, e.Round, e.Err)
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsRuntimeErrorCode reports whether err contains the requested stable code.
func IsRuntimeErrorCode(err error, code RuntimeErrorCode) bool {
	var runtimeErr *RuntimeError
	return errors.As(err, &runtimeErr) && runtimeErr.Code == code
}

// AgentRuntime is the single owner of LLM and tool rounds.
type AgentRuntime struct {
	config   RuntimeConfig
	model    Generator
	registry *tools.Registry
	executor *tools.Executor
	observer RoundObserver
}

// NewAgentRuntime validates and constructs the production agent loop.
func NewAgentRuntime(
	config RuntimeConfig,
	model Generator,
	registry *tools.Registry,
	executor *tools.Executor,
	observer RoundObserver,
) (*AgentRuntime, error) {
	if config.MaxToolRounds <= 0 {
		return nil, &RuntimeError{
			Code: RuntimeInvalidRuntime,
			Err:  fmt.Errorf("max tool rounds must be greater than zero"),
		}
	}
	if model == nil {
		return nil, &RuntimeError{Code: RuntimeInvalidRuntime, Err: fmt.Errorf("generator is nil")}
	}
	if executor == nil {
		executor = tools.NewExecutor(registry)
	}
	if observer == nil {
		observer = nopRoundObserver{}
	}
	return &AgentRuntime{
		config:   config,
		model:    model,
		registry: registry,
		executor: executor,
		observer: observer,
	}, nil
}

// Run executes model and tool rounds until a direct reply, cancellation, model
// failure or configured tool-round limit is reached.
func (r *AgentRuntime) Run(ctx context.Context, request Request) (Result, error) {
	result := Result{History: cloneMessages(request.History)}
	if r == nil || r.model == nil || r.config.MaxToolRounds <= 0 {
		err := &RuntimeError{Code: RuntimeInvalidRuntime, Err: fmt.Errorf("runtime is not initialized")}
		return result, err
	}

	input := request.Input
	toolRounds := 0
	for roundNumber := 1; ; roundNumber++ {
		if err := ctx.Err(); err != nil {
			return r.stopForContext(result, roundNumber, err)
		}

		roundStarted := time.Now()
		modelStarted := time.Now()
		reply, _, nextHistory, err := r.model.Generate(ctx, result.History, input, r.registry)
		round := RoundTrace{
			Number:        roundNumber,
			ModelDuration: positiveDuration(time.Since(modelStarted)),
		}
		if err != nil {
			round.Error = err.Error()
			round.StopReason = StopModelError
			round.Duration = positiveDuration(time.Since(roundStarted))
			result.Trace.Rounds = append(result.Trace.Rounds, round)
			result.Trace.StopReason = round.StopReason
			result.Trace.ToolRounds = toolRounds
			r.observer.OnRound(round)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, runtimeErrorFromContext(roundNumber, ctxErr)
			}
			return result, &RuntimeError{Code: RuntimeModelError, Round: roundNumber, Err: err}
		}

		result.History = cloneMessages(nextHistory)
		calls := pendingToolCalls(result.History)
		if len(calls) == 0 {
			result.Reply = strings.TrimSpace(reply)
			round.StopReason = StopDirectReply
			round.Duration = positiveDuration(time.Since(roundStarted))
			result.Trace.Rounds = append(result.Trace.Rounds, round)
			result.Trace.StopReason = round.StopReason
			result.Trace.ToolRounds = toolRounds
			r.observer.OnRound(round)
			return result, nil
		}

		if toolRounds >= r.config.MaxToolRounds {
			round.StopReason = StopRoundLimit
			round.Duration = positiveDuration(time.Since(roundStarted))
			result.Trace.Rounds = append(result.Trace.Rounds, round)
			result.Trace.StopReason = round.StopReason
			result.Trace.ToolRounds = toolRounds
			r.observer.OnRound(round)
			return result, &RuntimeError{
				Code:  RuntimeRoundLimit,
				Round: roundNumber,
				Err:   fmt.Errorf("maximum of %d tool rounds reached", r.config.MaxToolRounds),
			}
		}

		records := r.executor.RunAllDetailed(ctx, calls)
		round.Tools = make([]ToolTrace, len(records))
		for i, record := range records {
			round.Tools[i] = ToolTrace{
				CallID:    record.Call.ID,
				Tool:      record.Call.Function.Name,
				Success:   record.Success(),
				ErrorCode: record.ErrorCode,
				Duration:  positiveDuration(record.Duration),
			}
			result.History = append(result.History, llm.Message{
				Role:       record.Message.Role,
				ToolCallID: record.Message.ToolCallID,
				Content:    record.Message.Content,
			})
		}
		toolRounds++
		round.Duration = positiveDuration(time.Since(roundStarted))
		result.Trace.Rounds = append(result.Trace.Rounds, round)
		result.Trace.ToolRounds = toolRounds
		r.observer.OnRound(round)

		if err := ctx.Err(); err != nil {
			result.Trace.StopReason = stopReasonFromContext(err)
			return result, runtimeErrorFromContext(roundNumber, err)
		}
		input = ""
	}
}

func (r *AgentRuntime) stopForContext(result Result, round int, err error) (Result, error) {
	result.Trace.StopReason = stopReasonFromContext(err)
	return result, runtimeErrorFromContext(round, err)
}

func runtimeErrorFromContext(round int, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &RuntimeError{Code: RuntimeDeadlineExceeded, Round: round, Err: context.DeadlineExceeded}
	}
	return &RuntimeError{Code: RuntimeCancelled, Round: round, Err: context.Canceled}
}

func stopReasonFromContext(err error) StopReason {
	if errors.Is(err, context.DeadlineExceeded) {
		return StopDeadlineExceeded
	}
	return StopCancelled
}

func pendingToolCalls(history []llm.Message) []tools.ToolCall {
	if len(history) == 0 {
		return nil
	}
	last := history[len(history)-1]
	if last.Role != "assistant" || len(last.ToolCalls) == 0 {
		return nil
	}
	return cloneToolCalls(last.ToolCalls)
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

func positiveDuration(duration time.Duration) time.Duration {
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}
