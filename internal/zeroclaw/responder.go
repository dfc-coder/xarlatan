package zeroclaw

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

type promptTransport interface {
	RespondStream(context.Context, string, func(string) error) (Result, error)
}

// VoiceResponder adapts the ZeroClaw ACP session to Xarlatan's existing
// Coordinator responder boundary without adding local memory or agent logic.
type VoiceResponder struct {
	transport promptTransport
}

func NewVoiceResponder(transport promptTransport) (*VoiceResponder, error) {
	if transport == nil {
		return nil, errors.New("zeroclaw voice responder transport is nil")
	}
	return &VoiceResponder{transport: transport}, nil
}

func (r *VoiceResponder) Respond(ctx context.Context, text string) (conversation.Result, error) {
	return r.respond(ctx, text, nil)
}

func (r *VoiceResponder) RespondStream(ctx context.Context, text string, onDelta llm.ContentDelta) (conversation.Result, error) {
	var callback func(string) error
	if onDelta != nil {
		var observed atomic.Bool
		callback = func(delta string) error {
			if observed.CompareAndSwap(false, true) {
				slog.Info("zeroclaw response stream started")
			}
			return onDelta(delta)
		}
	}
	return r.respond(ctx, text, callback)
}

func (r *VoiceResponder) respond(ctx context.Context, text string, onDelta func(string) error) (conversation.Result, error) {
	if r == nil || r.transport == nil {
		return conversation.Result{}, errors.New("zeroclaw voice responder is not initialized")
	}
	result, err := r.transport.RespondStream(ctx, text, onDelta)
	if err != nil {
		return conversation.Result{}, err
	}
	return conversation.Result{Reply: strings.TrimSpace(result.Reply)}, nil
}
