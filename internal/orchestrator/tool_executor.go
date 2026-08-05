// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

// ToolExecutor runs pending tool calls and stores their results.
type ToolExecutor struct {
	Executor *tools.Executor
	Observer Observer
}

// Name returns the node name used by the orchestrator.
func (ToolExecutor) Name() string { return "tool_executor" }

// Run executes pending tool calls and forwards the flow to composition.
func (n ToolExecutor) Run(ctx context.Context, state State) (State, string, error) {
	nodeCtx := state.ToolExecutorContext()
	if len(nodeCtx.ToolCalls) == 0 {
		state.NextNode = "response_composer"
		return state, state.NextNode, nil
	}

	exec := n.Executor
	if exec == nil {
		exec = tools.NewExecutor(nil)
	}
	observer := n.Observer
	if observer == nil {
		observer = nopObserver{}
	}

	calls := nodeCtx.ToolCalls
	state.ToolCalls = nil
	state.ToolResults = state.ToolResults[:0]
	for _, call := range calls {
		started := time.Now()
		msgs, _ := exec.RunAll(ctx, []tools.ToolCall{call})
		content := "ERROR: tool execution produced no message"
		if len(msgs) > 0 {
			content = strings.TrimSpace(msgs[0].Content)
		}
		success := !strings.HasPrefix(content, "ERROR:")
		event := ToolCallEvent{
			Node:     n.Name(),
			Tool:     call.Function.Name,
			CallID:   call.ID,
			Duration: time.Since(started),
			Success:  success,
		}
		if !success {
			event.Error = content
		}
		observer.OnToolCall(event)

		msg := tools.ToolMessage{Content: content}
		if len(msgs) > 0 {
			msg = msgs[0]
		}
		state.ToolResults = append(state.ToolResults, strings.TrimSpace(msg.Content))
	}
	state.NextNode = "response_composer"
	return state, state.NextNode, nil
}
