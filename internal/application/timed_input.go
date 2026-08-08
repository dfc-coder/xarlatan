package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

// TimedVoiceInput decorates a VoiceInput with an end-of-speech timestamp tied
// to the utterance returned by Next. The timestamp is metadata-only and is
// captured immediately after the underlying endpointed utterance is delivered.
type TimedVoiceInput struct {
	input VoiceInput
	mu    sync.Mutex
	ended time.Time
}

func NewTimedVoiceInput(input VoiceInput) (*TimedVoiceInput, error) {
	if input == nil {
		return nil, errors.New("timed voice input is nil")
	}
	return &TimedVoiceInput{input: input}, nil
}

func (i *TimedVoiceInput) Next(ctx context.Context) (audio.Buffer, error) {
	if i == nil || i.input == nil {
		return audio.Buffer{}, errors.New("timed voice input is not initialized")
	}
	buffer, err := i.input.Next(ctx)
	if err != nil {
		return buffer, err
	}
	if !buffer.Empty() {
		i.mu.Lock()
		i.ended = time.Now()
		i.mu.Unlock()
	}
	return buffer, nil
}

func (i *TimedVoiceInput) ConsumeEndOfSpeech() time.Time {
	if i == nil {
		return time.Time{}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	value := i.ended
	i.ended = time.Time{}
	return value
}

var _ VoiceInput = (*TimedVoiceInput)(nil)
var _ EndOfSpeechMetricProvider = (*TimedVoiceInput)(nil)
