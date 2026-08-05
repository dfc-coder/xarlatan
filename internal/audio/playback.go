package audio

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
)

// Playback plays raw PCM audio through the ALSA speaker.
type Playback struct {
	device     string
	sampleRate int
	channels   int
}

// NewPlayback creates a Playback instance.
func NewPlayback(device string, sampleRate, channels int) *Playback {
	return &Playback{
		device:     device,
		sampleRate: sampleRate,
		channels:   channels,
	}
}

// PlayRaw plays signed 16-bit LE raw PCM data.
func (p *Playback) PlayRaw(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}

	args := []string{
		"-D", p.device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", p.sampleRate),
		"-c", fmt.Sprintf("%d", p.channels),
		"-t", "raw",
		"-q",
		"-",
	}

	cmd := exec.Command("aplay", args...)
	cmd.Stdin = bytes.NewReader(pcm)

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Debug("aplay output", "out", string(out))
		return fmt.Errorf("aplay: %w", err)
	}
	return nil
}
