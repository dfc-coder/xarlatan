package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRearmWaitsForConsecutiveStableSilence(t *testing.T) {
	guard := &microphoneRearmGuard{
		sampleRate:    1000,
		channels:      1,
		threshold:     0.1,
		stableSilence: 240 * time.Millisecond,
	}

	var samples []float32
	appendChunk := func(value float32) {
		for i := 0; i < 80; i++ {
			samples = append(samples, value)
		}
	}
	appendChunk(0.5)
	appendChunk(0)
	appendChunk(0)
	appendChunk(0.5) // resets the partial silence window
	appendChunk(0)
	appendChunk(0)
	appendChunk(0)

	if err := guard.waitForStableSilence(context.Background(), bytes.NewReader(float32ToPCM16(samples))); err != nil {
		t.Fatalf("waitForStableSilence() error = %v", err)
	}
}

func TestRearmCooldownIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	guard := &microphoneRearmGuard{
		device:        "default",
		sampleRate:    16000,
		channels:      1,
		threshold:     0.1,
		cooldown:      time.Hour,
		stableSilence: time.Second,
	}

	if err := guard.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}

func TestPlaybackRunsGuardAfterAudioCompletes(t *testing.T) {
	calls := make([]string, 0, 2)
	player := NewPlayback("default", 16000, 1)
	player.runner = commandRunnerFunc(func(context.Context, string, []string, io.Reader) ([]byte, error) {
		calls = append(calls, "playback")
		return nil, nil
	})
	player.guard = playbackGuardFunc(func(context.Context) error {
		calls = append(calls, "guard")
		return nil
	})

	err := player.Play(context.Background(), Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != "playback" || calls[1] != "guard" {
		t.Fatalf("calls = %v, want [playback guard]", calls)
	}
}

func TestPlaybackDoesNotGuardAfterPlaybackFailure(t *testing.T) {
	guardCalled := false
	player := NewPlayback("default", 16000, 1)
	player.runner = commandRunnerFunc(func(context.Context, string, []string, io.Reader) ([]byte, error) {
		return nil, errors.New("device failed")
	})
	player.guard = playbackGuardFunc(func(context.Context) error {
		guardCalled = true
		return nil
	})

	if err := player.Play(context.Background(), Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1}); err == nil {
		t.Fatal("Play() error = nil, want failure")
	}
	if guardCalled {
		t.Fatal("guard ran after failed playback")
	}
}

type commandRunnerFunc func(context.Context, string, []string, io.Reader) ([]byte, error)

func (f commandRunnerFunc) Run(ctx context.Context, name string, args []string, stdin io.Reader) ([]byte, error) {
	return f(ctx, name, args, stdin)
}
