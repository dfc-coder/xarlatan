package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

// STTPartialMetricProvider exposes only metadata to the turn recorder.
type STTPartialMetricProvider interface {
	ConsumeFirstSTTPartial(turnID uint64) time.Duration
}

// TranscriptBridge observes the existing turn lifecycle, decodes a bounded
// partial snapshot while capture is still active, and wraps final STT. Partial
// text never becomes the return value of Transcribe and therefore cannot enter
// wake gating, memory or AgentRuntime.
type TranscriptBridge struct {
	source      PartialAudioSource
	transcriber Transcriber
	observer    TranscriptObserver

	decodeMu sync.Mutex
	mu       sync.Mutex
	turnID   uint64
	started  time.Time
	cancel   context.CancelFunc
	latency  time.Duration
}

// NewTranscriptBridge constructs the partial/final STT boundary.
func NewTranscriptBridge(source PartialAudioSource, transcriber Transcriber, observer TranscriptObserver) (*TranscriptBridge, error) {
	if source == nil {
		return nil, fmt.Errorf("partial audio source is nil")
	}
	if transcriber == nil {
		return nil, fmt.Errorf("transcriber is nil")
	}
	if observer == nil {
		observer = nopTranscriptObserver{}
	}
	return &TranscriptBridge{source: source, transcriber: transcriber, observer: observer}, nil
}

// OnEvent consumes metadata-only lifecycle events. Transcript data remains on
// the dedicated TranscriptObserver path.
func (b *TranscriptBridge) OnEvent(event Event) {
	if b == nil {
		return
	}
	switch event.State {
	case StateListening:
		b.startPreview(event.TurnID)
	case StateStopping:
		b.cancelPreview(event.TurnID)
	}
}

func (b *TranscriptBridge) startPreview(turnID uint64) {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.turnID = turnID
	b.started = time.Now()
	b.cancel = cancel
	b.latency = 0
	started := b.started
	b.mu.Unlock()

	go b.decodePreview(ctx, turnID, started)
}

func (b *TranscriptBridge) decodePreview(ctx context.Context, turnID uint64, started time.Time) {
	previews := b.source.PartialAudio()
	if previews == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case preview, ok := <-previews:
		if !ok || preview.Buffer.Empty() {
			return
		}
		b.decodeMu.Lock()
		defer b.decodeMu.Unlock()
		if ctx.Err() != nil || !b.isCurrentTurn(turnID) {
			return
		}
		text, err := b.transcriber.Transcribe(ctx, preview.Buffer)
		if err != nil || ctx.Err() != nil || !b.isCurrentTurn(turnID) {
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
		b.mu.Lock()
		if b.turnID != turnID {
			b.mu.Unlock()
			return
		}
		if b.latency == 0 {
			b.latency = latency
		}
		b.mu.Unlock()
		b.observer.OnTranscript(TranscriptEvent{TurnID: turnID, Kind: TranscriptPartial, Text: text})
	}
}

// Transcribe returns only the authoritative final transcript. The local decode
// mutex orders a preview that already started ahead of the final decode and
// prevents a late preview from racing behind the final event.
func (b *TranscriptBridge) Transcribe(ctx context.Context, buffer audio.Buffer) (string, error) {
	if b == nil || b.transcriber == nil {
		return "", fmt.Errorf("transcript bridge is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	turnID := b.currentTurn()
	b.decodeMu.Lock()
	text, err := b.transcriber.Transcribe(ctx, buffer)
	b.decodeMu.Unlock()
	if err != nil {
		b.cancelPreview(turnID)
		return "", err
	}
	text = strings.TrimSpace(text)
	b.cancelPreview(turnID)
	b.observer.OnTranscript(TranscriptEvent{TurnID: turnID, Kind: TranscriptFinal, Text: text})
	return text, nil
}

func (b *TranscriptBridge) currentTurn() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turnID
}

func (b *TranscriptBridge) isCurrentTurn(turnID uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turnID == turnID
}

func (b *TranscriptBridge) cancelPreview(turnID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.turnID != turnID {
		return
	}
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// ConsumeFirstSTTPartial returns and clears the bounded metadata for one turn.
func (b *TranscriptBridge) ConsumeFirstSTTPartial(turnID uint64) time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.turnID != turnID {
		return 0
	}
	latency := b.latency
	b.latency = 0
	return latency
}

var _ Transcriber = (*TranscriptBridge)(nil)
var _ Observer = (*TranscriptBridge)(nil)
var _ STTPartialMetricProvider = (*TranscriptBridge)(nil)
