package audio

import (
	"context"
	"fmt"
)

// ResponsePlayback adapts the persistent Playback primitive to application
// response semantics. A complete buffered response is written and then drained;
// streaming callers can issue multiple Write calls followed by one Finish.
type ResponsePlayback struct {
	playback *Playback
}

func NewResponsePlayback(playback *Playback) (*ResponsePlayback, error) {
	if playback == nil {
		return nil, fmt.Errorf("playback is nil")
	}
	return &ResponsePlayback{playback: playback}, nil
}

func (p *ResponsePlayback) Play(ctx context.Context, buffer Buffer) error {
	if p == nil || p.playback == nil {
		return fmt.Errorf("response playback is not initialized")
	}
	if buffer.Empty() {
		return nil
	}
	if err := p.playback.Write(ctx, buffer); err != nil {
		return err
	}
	return p.playback.Finish(ctx)
}

func (p *ResponsePlayback) Write(ctx context.Context, buffer Buffer) error {
	if p == nil || p.playback == nil {
		return fmt.Errorf("response playback is not initialized")
	}
	return p.playback.Write(ctx, buffer)
}

func (p *ResponsePlayback) Finish(ctx context.Context) error {
	if p == nil || p.playback == nil {
		return fmt.Errorf("response playback is not initialized")
	}
	return p.playback.Finish(ctx)
}

func (p *ResponsePlayback) Stop() error {
	if p == nil || p.playback == nil {
		return nil
	}
	return p.playback.Stop()
}

func (p *ResponsePlayback) Close() error {
	if p == nil || p.playback == nil {
		return nil
	}
	return p.playback.Close()
}
