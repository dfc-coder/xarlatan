// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import "github.com/dfc-coder/xarlatan/internal/tools"

// RouterContext includes only the fields needed by Router.
type RouterContext struct {
	Input  string
	Intent string
}

// PlannerContext includes only the fields needed by Planner.
type PlannerContext struct {
	Input string
}

// ToolExecutorContext includes only the fields needed by ToolExecutor.
type ToolExecutorContext struct {
	ToolCalls []tools.ToolCall
}

// ResponseComposerContext includes only the fields needed by ResponseComposer.
type ResponseComposerContext struct {
	Intent        string
	Input         string
	Summary       string
	ToolCalls     []tools.ToolCall
	ToolResults   []string
	DraftResponse string
}

// FinalizerContext includes only the fields needed by Finalizer.
type FinalizerContext struct {
	DraftResponse string
	FinalResponse string
}

// RouterContext projects State into RouterContext.
func (s State) RouterContext() RouterContext {
	return RouterContext{
		Input:  s.Input,
		Intent: s.Intent,
	}
}

// PlannerContext projects State into PlannerContext.
func (s State) PlannerContext() PlannerContext {
	return PlannerContext{Input: s.Input}
}

// ToolExecutorContext projects State into ToolExecutorContext.
func (s State) ToolExecutorContext() ToolExecutorContext {
	return ToolExecutorContext{ToolCalls: cloneToolCalls(s.ToolCalls)}
}

// ResponseComposerContext projects State into ResponseComposerContext.
func (s State) ResponseComposerContext() ResponseComposerContext {
	return ResponseComposerContext{
		Intent:        s.Intent,
		Input:         s.Input,
		Summary:       s.Summary,
		ToolCalls:     cloneToolCalls(s.ToolCalls),
		ToolResults:   cloneStrings(s.ToolResults),
		DraftResponse: s.DraftResponse,
	}
}

// FinalizerContext projects State into FinalizerContext.
func (s State) FinalizerContext() FinalizerContext {
	return FinalizerContext{
		DraftResponse: s.DraftResponse,
		FinalResponse: s.FinalResponse,
	}
}

func cloneToolCalls(calls []tools.ToolCall) []tools.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]tools.ToolCall, len(calls))
	for i, call := range calls {
		cloned[i] = call
		if len(call.Function.Arguments) == 0 {
			continue
		}
		args := make([]byte, len(call.Function.Arguments))
		copy(args, call.Function.Arguments)
		cloned[i].Function.Arguments = args
	}
	return cloned
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
