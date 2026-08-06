package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
)

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	return cmd.CombinedOutput()
}

// Playback plays normalized mono audio through the ALSA speaker.
type Playback struct {
	device     string
	sampleRate int
	channels   int
	runner     commandRunner
}

// NewPlayback creates a Playback instance.
func NewPlayback(device string, sampleRate, channels int) *Playback {
	return &Playback{
		device:     device,
		sampleRate: sampleRate,
		channels:   channels,
		runner:     execRunner{},
	}
}

// Play plays one normalized audio buffer and respects context cancellation.
func (p *Playback) Play(ctx context.Context, buffer Buffer) error {
	if buffer.Empty() {
		return nil
	}
	if p == nil || p.runner == nil {
		return fmt.Errorf("playback is not initialized")
	}
	if err := buffer.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	args := playbackArgs(p.device, buffer.SampleRate, buffer.Channels)
	out, err := p.runner.Run(ctx, "aplay", args, bytes.NewReader(float32ToPCM16(buffer.Samples)))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		slog.Debug("aplay output", "bytes", len(out))
		return fmt.Errorf("aplay: %w", err)
	}
	return nil
}

// PlayRaw preserves the legacy raw-PCM entry point for callers outside the
// voice application. New code should use Play.
func (p *Playback) PlayRaw(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if p == nil || p.runner == nil {
		return fmt.Errorf("playback is not initialized")
	}
	out, err := p.runner.Run(context.Background(), "aplay", playbackArgs(p.device, p.sampleRate, p.channels), bytes.NewReader(pcm))
	if err != nil {
		slog.Debug("aplay output", "bytes", len(out))
		return fmt.Errorf("aplay: %w", err)
	}
	return nil
}

func playbackArgs(device string, sampleRate, channels int) []string {
	return []string{
		"-D", device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", sampleRate),
		"-c", fmt.Sprintf("%d", channels),
		"-t", "raw",
		"-q",
		"-",
	}
}

func float32ToPCM16(samples []float32) []byte {
	pcm := make([]byte, len(samples)*pcmBytesPerSample)
	for i, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		value := int16(sample * 32767)
		binary.LittleEndian.PutUint16(pcm[i*pcmBytesPerSample:], uint16(value))
	}
	return pcm
}
