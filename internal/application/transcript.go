package application

import (
	"context"
	"strings"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

// TranscriptKind distinguishes non-authoritative preview text from the final
// transcript that may enter wake gating, memory and AgentRuntime.
type TranscriptKind string

const (
	TranscriptPartial TranscriptKind = "partial"
	TranscriptFinal   TranscriptKind = "final"
)

// TranscriptEvent is data-plane because it contains conversation text. It must
// never be sent through the metadata-only Observer event channel.
type TranscriptEvent struct {
	TurnID uint64
	Kind   TranscriptKind
	Text   string
}

// TranscriptObserver receives text-bearing STT events separately from metrics.
type TranscriptObserver interface {
	OnTranscript(TranscriptEvent)
}

type nopTranscriptObserver struct{}

func (nopTranscriptObserver) OnTranscript(TranscriptEvent) {}

// PartialAudioSource publishes at most one bounded preview snapshot for an
// active utterance in the WI-11C2 beta implementation.
type PartialAudioSource interface {
	PartialAudio() <-chan audio.PartialAudio
}

type partialSTTResult struct {
	turnID  uint64
	latency time.Duration
	text    string
}

// SetTranscriptObserver configures the text-bearing data-plane sink. Nil means
// no external sink while the internal partial/final lifecycle remains intact.
func (c *Coordinator) SetTranscriptObserver(observer TranscriptObserver) {
	if c == nil {
		return
	}
	if observer == nil {
		observer = nopTranscriptObserver{}
	}
	c.transcripts = observer
}

func (c *Coordinator) startPartialSTT(ctx context.Context, turnID uint64, started time.Time) <-chan partialSTTResult {
	if c == nil {
		return nil
	}
	source, ok := c.dependencies.Input.(PartialAudioSource)
	if !ok {
		return nil
	}
	previews := source.PartialAudio()
	if previews == nil {
		return nil
	}
	out := make(chan partialSTTResult, 1)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
			return
		case preview, ok := <-previews:
			if !ok || preview.Buffer.Empty() {
				return
			}
			text, err := c.dependencies.Transcriber.Transcribe(ctx, preview.Buffer)
			if err != nil || ctx.Err() != nil {
				return
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return
			}
			latency := time.Since(started)
			if latency <= 0 {
				latency = time.Nanosecond
			}
			event := TranscriptEvent{TurnID: turnID, Kind: TranscriptPartial, Text: text}
			c.transcriptObserver().OnTranscript(event)
			select {
			case out <- partialSTTResult{turnID: turnID, latency: latency, text: text}:
			default:
			}
		}
	}()
	return out
}

func (c *Coordinator) transcriptObserver() TranscriptObserver {
	if c == nil || c.transcripts == nil {
		return nopTranscriptObserver{}
	}
	return c.transcripts
}

func consumePartialSTT(result <-chan partialSTTResult, turnID uint64, recorder *turnRecorder) {
	if result == nil || recorder == nil {
		return
	}
	select {
	case partial, ok := <-result:
		if ok && partial.turnID == turnID && partial.text != "" {
			recorder.markFirstSTTPartial(partial.latency)
		}
	default:
	}
}
