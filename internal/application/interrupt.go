package application

import (
	"context"
	"errors"
)

// StateInterrupted marks a user-driven interruption of the active turn.
const StateInterrupted State = "interrupted"

// ErrorInterrupted is recoverable: it aborts only the active turn, not the
// long-lived Coordinator loop.
const ErrorInterrupted ErrorCode = "interrupted"

var errTurnInterrupted = errors.New("turn interrupted")

// InterruptEvent is metadata-only. TurnID prevents a late speech event from an
// older turn from cancelling the current one.
type InterruptEvent struct {
	TurnID uint64
	Reason string
}

// InterruptSource binds an external interruption detector to one active turn.
// Implementations must stop publishing when ctx is done. Physical microphone
// integration is intentionally separate from this lifecycle contract.
type InterruptSource interface {
	Subscribe(ctx context.Context, turnID uint64) <-chan InterruptEvent
}

// SetInterruptSource enables user-driven interruption. Passing nil restores the
// original non-interruptible behavior.
func (c *Coordinator) SetInterruptSource(source InterruptSource) {
	if c == nil {
		return
	}
	c.interrupts = source
}

func (c *Coordinator) watchInterrupts(
	ctx context.Context,
	turnID uint64,
	cancel context.CancelCauseFunc,
) {
	if c == nil || c.interrupts == nil || cancel == nil {
		return
	}
	events := c.interrupts.Subscribe(ctx, turnID)
	if events == nil {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.TurnID != turnID {
					continue
				}
				cancel(errTurnInterrupted)
				return
			}
		}
	}()
}

func interruptedContext(ctx context.Context) bool {
	return ctx != nil && errors.Is(context.Cause(ctx), errTurnInterrupted)
}

func interruptedApplicationError() error {
	return &Error{Code: ErrorInterrupted, Recoverable: true, Err: errTurnInterrupted}
}
