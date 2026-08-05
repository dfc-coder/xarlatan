// Package orchestrator runs the assistant through explicit nodes.
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

// State carries the data passed between nodes.
type State struct {
	Input         string
	Intent        string
	Summary       string
	RecentTurns   []string
	ToolCalls     []tools.ToolCall
	ToolResults   []string
	DraftResponse string
	FinalResponse string
	NextNode      string
	Done          bool
}

// Node processes a state and returns the next node name.
type Node interface {
	Name() string
	Run(ctx context.Context, state State) (State, string, error)
}

// Orchestrator executes a fixed set of nodes by name.
type Orchestrator struct {
	start               string
	nodes               map[string]Node
	obs                 Observer
	transitionOverrides map[string]string
}

// New builds an Orchestrator and registers nodes by their name.
func New(start string, nodes ...Node) *Orchestrator {
	return NewWithObserver(start, nil, nodes...)
}

// NewWithObserver builds an Orchestrator with optional observability hooks.
func NewWithObserver(start string, observer Observer, nodes ...Node) *Orchestrator {
	m := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		m[node.Name()] = node
	}
	if observer == nil {
		observer = nopObserver{}
	}
	return &Orchestrator{start: start, nodes: m, obs: observer}
}

// WithTransitionOverrides rewires transitions without changing node code.
//
// Keys must use "from->to" format (for example: "response_composer->finalizer").
func (o *Orchestrator) WithTransitionOverrides(overrides map[string]string) *Orchestrator {
	if len(overrides) == 0 {
		o.transitionOverrides = nil
		return o
	}

	cloned := make(map[string]string, len(overrides))
	for edge, next := range overrides {
		if edge == "" || next == "" {
			continue
		}
		cloned[edge] = next
	}
	o.transitionOverrides = cloned
	return o
}

// Run executes nodes until a node stops the flow or the context is cancelled.
func (o *Orchestrator) Run(ctx context.Context, state State) (State, error) {
	next := o.start
	for next != "" {
		if err := ctx.Err(); err != nil {
			return state, err
		}

		node, ok := o.nodes[next]
		if !ok {
			return state, fmt.Errorf("orchestrator: unknown node %q", next)
		}

		started := time.Now()
		o.obs.OnNodeStart(NodeStartEvent{Node: next})
		state.NextNode = next
		var err error
		state, next, err = node.Run(ctx, state)
		if err != nil {
			finishEvent := NodeFinishEvent{Node: node.Name(), NextNode: next, Done: state.Done, Duration: time.Since(started)}
			finishEvent.Error = err.Error()
			o.obs.OnNodeFinish(finishEvent)
			return state, err
		}
		next = o.applyTransitionOverride(node.Name(), next, state.Done)
		state.NextNode = next
		finishEvent := NodeFinishEvent{Node: node.Name(), NextNode: next, Done: state.Done, Duration: time.Since(started)}
		o.obs.OnNodeFinish(finishEvent)
		if state.Done {
			return state, nil
		}
	}

	return state, nil
}

func (o *Orchestrator) applyTransitionOverride(from, next string, done bool) string {
	if done || next == "" || len(o.transitionOverrides) == 0 {
		return next
	}
	if forced, ok := o.transitionOverrides[transitionKey(from, next)]; ok {
		return forced
	}
	return next
}
