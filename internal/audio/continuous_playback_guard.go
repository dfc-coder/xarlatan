package audio

import (
	"context"
	"time"
)

const continuousCaptureSettleDelay = 350 * time.Millisecond

// UseContinuousCaptureGuard replaces the legacy post-playback microphone probe
// with a bounded acoustic settling delay. Continuous capture remains open and
// suppressed during this interval, so no second arecord process is required.
func (p *Playback) UseContinuousCaptureGuard() {
	if p == nil {
		return
	}
	p.guard = settlingPlaybackGuard{delay: continuousCaptureSettleDelay}
}

type settlingPlaybackGuard struct {
	delay time.Duration
}

func (g settlingPlaybackGuard) Wait(ctx context.Context) error {
	ctx = nonNilAudioContext(ctx)
	if g.delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(g.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
