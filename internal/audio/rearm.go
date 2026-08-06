package audio

import (
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"time"
)

const (
	defaultRearmCooldown  = 700 * time.Millisecond
	defaultStableSilence  = 560 * time.Millisecond
	defaultRearmThreshold = 0.012
)

type playbackGuard interface {
	Wait(context.Context) error
}

type playbackGuardFunc func(context.Context) error

func (f playbackGuardFunc) Wait(ctx context.Context) error {
	return f(ctx)
}

type microphoneRearmGuard struct {
	device        string
	sampleRate    int
	channels      int
	threshold     float64
	cooldown      time.Duration
	stableSilence time.Duration
}

func newMicrophoneRearmGuard(device string, sampleRate, channels int) *microphoneRearmGuard {
	return &microphoneRearmGuard{
		device:        device,
		sampleRate:    sampleRate,
		channels:      channels,
		threshold:     defaultRearmThreshold,
		cooldown:      defaultRearmCooldown,
		stableSilence: defaultStableSilence,
	}
}

// Wait keeps the assistant half-duplex after playback. It first allows speaker
// and device buffers to drain, then discards microphone input until ambient
// energy remains below the release threshold for a stable window.
func (g *microphoneRearmGuard) Wait(ctx context.Context) error {
	if g == nil {
		return fmt.Errorf("microphone rearm guard is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := sleepContext(ctx, g.cooldown); err != nil {
		return err
	}
	if _, err := pcmChunkBytes(g.sampleRate, g.channels); err != nil {
		return err
	}

	args := []string{
		"-D", g.device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", g.sampleRate),
		"-c", fmt.Sprintf("%d", g.channels),
		"-t", "raw",
		"--buffer-size=2048",
		"-q",
		"-",
	}
	cmd := exec.CommandContext(ctx, "arecord", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("rearm arecord pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rearm arecord start: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	if err := g.waitForStableSilence(ctx, stdout); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("waiting for stable microphone silence: %w", err)
	}
	return nil
}

func (g *microphoneRearmGuard) waitForStableSilence(ctx context.Context, reader io.Reader) error {
	bytesPerChunk, err := pcmChunkBytes(g.sampleRate, g.channels)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		return fmt.Errorf("rearm audio reader is nil")
	}

	chunkDuration := chunkDurationMS * time.Millisecond
	stableChunks := int((g.stableSilence + chunkDuration - 1) / chunkDuration)
	if stableChunks < 1 {
		stableChunks = 1
	}
	threshold := g.threshold
	if threshold < 0 {
		threshold = 0
	}

	buf := make([]byte, bytesPerChunk)
	consecutiveSilence := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := io.ReadFull(reader, buf)
		completeBytes := n - n%(pcmBytesPerSample*g.channels)
		if completeBytes > 0 {
			if rmsFloat32(pcmToFloat32(buf[:completeBytes])) <= threshold {
				consecutiveSilence++
				if consecutiveSilence >= stableChunks {
					return nil
				}
			} else {
				consecutiveSilence = 0
			}
		}
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				return fmt.Errorf("audio ended before stable silence")
			}
			return readErr
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func rmsFloat32(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range samples {
		value := float64(sample)
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(samples)))
}
