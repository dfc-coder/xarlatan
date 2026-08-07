package application

import (
	"context"
	"fmt"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

// BargeCandidateSource publishes bounded private PCM captured only while the
// assistant is speaking.
type BargeCandidateSource interface {
	BargeCandidates() <-chan audio.BargeCandidate
}

// BargePolicy performs deterministic confirmation on candidate transcripts.
type BargePolicy interface {
	Confirm(transcript string) bool
}

// BargeInController converts private candidate audio into metadata-only
// InterruptEvents. It deliberately confirms via STT before cancelling playback.
type BargeInController struct {
	source      BargeCandidateSource
	transcriber Transcriber
	policy      BargePolicy
}

func NewBargeInController(source BargeCandidateSource, transcriber Transcriber, policy BargePolicy) (*BargeInController, error) {
	if source == nil {
		return nil, fmt.Errorf("barge candidate source is nil")
	}
	if transcriber == nil {
		return nil, fmt.Errorf("barge transcriber is nil")
	}
	if policy == nil {
		return nil, fmt.Errorf("barge policy is nil")
	}
	return &BargeInController{source: source, transcriber: transcriber, policy: policy}, nil
}

// Subscribe implements InterruptSource. A subscription belongs to one active
// TurnID and stops consuming candidates as soon as that turn context ends.
func (b *BargeInController) Subscribe(ctx context.Context, turnID uint64) <-chan InterruptEvent {
	ctx = nonNilContext(ctx)
	out := make(chan InterruptEvent, 1)
	if b == nil || b.source == nil || b.transcriber == nil || b.policy == nil {
		close(out)
		return out
	}
	candidates := b.source.BargeCandidates()
	if candidates == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case candidate, ok := <-candidates:
				if !ok {
					return
				}
				if candidate.Buffer.Empty() || candidate.StartedAt.IsZero() {
					continue
				}
				transcript, err := b.transcriber.Transcribe(ctx, candidate.Buffer)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				if !b.policy.Confirm(transcript) {
					continue
				}
				latency := time.Since(candidate.StartedAt)
				if latency <= 0 {
					latency = time.Nanosecond
				}
				event := InterruptEvent{
					TurnID:  turnID,
					Reason:  "wake_stop",
					Latency: latency,
				}
				select {
				case <-ctx.Done():
					return
				case out <- event:
					return
				}
			}
		}
	}()
	return out
}

var _ InterruptSource = (*BargeInController)(nil)
