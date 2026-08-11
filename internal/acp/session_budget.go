package acp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// sessionHistoryBudgetTransport keeps an ACP process persistent while rotating
// only the logical ACP session after a bounded number of completed turns. This
// prevents agent runtimes such as NullClaw from accumulating unbounded prompt
// history behind a long-lived session ID.
type sessionHistoryBudgetTransport struct {
	client   *Client
	maxTurns int

	mu    sync.Mutex
	turns int
}

// BudgetedResponder returns the normal voice responder with a bounded logical
// ACP session history. A non-positive maxTurns preserves the existing unbounded
// session behavior.
func (r *Runtime) BudgetedResponder(maxTurns int) (*VoiceResponder, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("ACP runtime is not initialized")
	}
	if maxTurns <= 0 {
		return r.Responder()
	}
	return NewVoiceResponder(&sessionHistoryBudgetTransport{
		client:   r.client,
		maxTurns: maxTurns,
	})
}

func (t *sessionHistoryBudgetTransport) RespondStream(
	ctx context.Context,
	prompt string,
	onDelta func(string) error,
) (Result, error) {
	if t == nil || t.client == nil {
		return Result{}, errors.New("ACP session history budget transport is not initialized")
	}

	// The ACP client already rejects concurrent prompts. This mutex additionally
	// keeps session rotation and the following prompt atomic from the wrapper's
	// point of view.
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.maxTurns > 0 && t.turns >= t.maxTurns {
		previous, next, err := t.client.rotateSession(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("rotate ACP session: %w", err)
		}
		slog.Info(
			"ACP session history budget rotated",
			"previous_session", previous,
			"session", next,
			"completed_turns", t.turns,
			"max_turns", t.maxTurns,
		)
		t.turns = 0
	}

	result, err := t.client.RespondStream(ctx, prompt, onDelta)
	if err == nil {
		t.turns++
	}
	return result, err
}

// rotateSession creates a fresh logical ACP session without restarting the ACP
// subprocess. NullClaw maps the ACP session ID directly to `agent invoke
// --session`, so changing this ID is what actually resets its short-term agent
// history while leaving its durable memory/backend untouched.
func (c *Client) rotateSession(ctx context.Context) (string, string, error) {
	if c == nil {
		return "", "", errors.New("ACP client is nil")
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if c.isClosed() {
		return "", "", ErrClosed
	}
	if c.promptBusy.Load() {
		return "", "", ErrSessionBusy
	}

	id := c.requestID()
	if err := c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/new",
		"params": map[string]any{
			"cwd": c.cfg.CWD,
		},
	}); err != nil {
		return "", "", err
	}

	message, err := c.readResponse(ctx, id)
	if err != nil {
		return "", "", err
	}
	result, err := responseResult(message)
	if err != nil {
		return "", "", err
	}
	next, _ := result["sessionId"].(string)
	next = strings.TrimSpace(next)
	if next == "" {
		return "", "", errors.New("ACP session/new returned empty sessionId")
	}

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return "", "", ErrClosed
	}
	previous := c.session
	c.session = next
	return previous, next, nil
}
