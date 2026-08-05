// Package audio provides microphone capture and speaker playback using ALSA.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/vad"
)

const chunkDurationMS = 80 // process audio in 80 ms chunks
const preRollChunkCount = 2

// Recorder captures audio from the default ALSA microphone.
type Recorder struct {
	cfg     config.AudioConfig
	vad     *vad.VAD
	preRoll *preRollBuffer
}

// NewRecorder creates a new Recorder.
func NewRecorder(cfg config.AudioConfig) *Recorder {
	releaseThreshold := cfg.SilenceThreshold * 0.8
	v := vad.New(cfg.SilenceThreshold, releaseThreshold, cfg.SilenceDuration(), cfg.SampleRate)
	v.OnEvent = func(e vad.Event) {
		switch e.Type {
		case vad.EventVoiceDetected:
			slog.Debug("VAD: voice detected")
		case vad.EventSilenceStart:
			slog.Debug("VAD: silence started")
		case vad.EventSilenceProgress:
			slog.Debug("VAD: silence accumulating", "ms", e.SilenceMS)
		case vad.EventCutBySilence:
			slog.Debug("VAD: cut by silence", "ms", e.SilenceMS)
		}
	}
	return &Recorder{cfg: cfg, vad: v, preRoll: newPreRollBuffer(preRollChunkCount)}
}

// RecordUntilSilence records audio until silence is detected or ctx is cancelled.
// Returns PCM samples as float32 in the range [-1, 1] at the configured sample rate.
func (r *Recorder) RecordUntilSilence(ctx context.Context) ([]float32, error) {
	slog.Debug("Waiting for speech…")

	// arecord arguments: raw signed 16-bit little-endian PCM
	args := []string{
		"-D", r.cfg.Device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", r.cfg.SampleRate),
		"-c", fmt.Sprintf("%d", r.cfg.Channels),
		"-t", "raw",
		"--buffer-size=2048",
		"-q", // quiet — suppress ALSA messages to stderr
		"-",  // write to stdout
	}

	cmd := exec.CommandContext(ctx, "arecord", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("arecord pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("arecord start: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	return r.recordFromReader(ctx, stdout, time.Now().Add(r.cfg.MaxDuration()))
}

func (r *Recorder) recordFromReader(ctx context.Context, stdout io.Reader, deadline time.Time) ([]float32, error) {

	samplesPerChunk := r.cfg.SampleRate * chunkDurationMS / 1000
	bytesPerChunk := samplesPerChunk * 2 // int16 = 2 bytes

	buf := make([]byte, bytesPerChunk)
	var recording []float32

	r.vad.Reset()
	r.preRoll.reset()

	for {
		if time.Now().After(deadline) {
			slog.Warn("max recording duration reached")
			slog.Debug("VAD: cut by timeout")
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if _, err := io.ReadFull(stdout, buf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, fmt.Errorf("reading audio: %w", err)
		}

		chunk := pcmToFloat32(buf)
		var shouldFinish bool
		recording, shouldFinish = r.processChunk(recording, chunk)

		if shouldFinish {
			break
		}
	}

	if len(recording) < r.cfg.SampleRate/4 { // < 250 ms
		return nil, nil // too short — noise
	}

	return recording, nil
}

func (r *Recorder) processChunk(recording, chunk []float32) ([]float32, bool) {
	isSpeaking, shouldFinish := r.vad.ProcessChunk(chunk)

	if isSpeaking {
		if len(recording) == 0 {
			recording = r.preRoll.startRecording(chunk)
			r.preRoll.reset()
		} else {
			recording = append(recording, chunk...)
		}
	} else if len(recording) == 0 {
		r.preRoll.add(chunk)
	}

	return recording, shouldFinish
}

// pcmToFloat32 converts raw S16_LE bytes to normalised float32.
func pcmToFloat32(raw []byte) []float32 {
	n := len(raw) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(raw[i*2:]))
		out[i] = float32(s) / 32768.0
	}
	return out
}
