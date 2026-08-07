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

const (
	chunkDurationMS   = 80
	preRollChunkCount = 2
	pcmBytesPerSample = 2
)

// Recorder captures audio from the default ALSA microphone.
type Recorder struct {
	cfg     config.AudioConfig
	vad     vad.Detector
	preRoll *preRollBuffer
}

// NewRecorder preserves the energy-detector constructor for calibration,
// focused tests and compatibility. Production composition uses
// NewRecorderWithDetector with Silero.
func NewRecorder(cfg config.AudioConfig) *Recorder {
	releaseThreshold := cfg.SilenceThreshold * 0.8
	detector := vad.New(cfg.SilenceThreshold, releaseThreshold, cfg.SilenceDuration(), cfg.SampleRate)
	recorder, _ := NewRecorderWithDetector(cfg, detector)
	return recorder
}

// NewRecorderWithDetector creates a Recorder against the VAD boundary so the
// audio package does not depend on sherpa-onnx or another concrete detector.
func NewRecorderWithDetector(cfg config.AudioConfig, detector vad.Detector) (*Recorder, error) {
	if detector == nil {
		return nil, fmt.Errorf("voice activity detector is nil")
	}
	detector.SetEventHandler(logVADEvent)
	return &Recorder{cfg: cfg, vad: detector, preRoll: newPreRollBuffer(preRollChunkCount)}, nil
}

func logVADEvent(event vad.Event) {
	switch event.Type {
	case vad.EventSpeechStarted:
		slog.Debug("VAD: speech started")
	case vad.EventSilenceStart:
		slog.Debug("VAD: silence started")
	case vad.EventSilenceProgress:
		slog.Debug("VAD: silence accumulating", "ms", event.SilenceMS)
	case vad.EventSpeechEnded:
		slog.Debug("VAD: speech ended", "silence_ms", event.SilenceMS)
	}
}

// Close releases detector resources. It is safe for the energy detector and
// required for native Silero VAD.
func (r *Recorder) Close() error {
	if r == nil || r.vad == nil {
		return nil
	}
	return r.vad.Close()
}

// Next implements the application voice-input boundary.
func (r *Recorder) Next(ctx context.Context) (Buffer, error) {
	samples, err := r.RecordUntilSilence(ctx)
	if err != nil {
		return Buffer{}, err
	}
	return Buffer{Samples: samples, SampleRate: r.cfg.SampleRate, Channels: r.cfg.Channels}, nil
}

// RecordUntilSilence records audio until silence is detected or ctx is cancelled.
// Returns PCM samples as float32 in the range [-1, 1] at the configured sample rate.
func (r *Recorder) RecordUntilSilence(ctx context.Context) ([]float32, error) {
	if r == nil {
		return nil, fmt.Errorf("recorder is nil")
	}
	if r.vad == nil {
		return nil, fmt.Errorf("voice activity detector is nil")
	}
	if _, err := pcmChunkBytes(r.cfg.SampleRate, r.cfg.Channels); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	slog.Debug("Waiting for speech…")
	args := []string{
		"-D", r.cfg.Device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", r.cfg.SampleRate),
		"-c", fmt.Sprintf("%d", r.cfg.Channels),
		"-t", "raw",
		"--buffer-size=2048",
		"-q",
		"-",
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
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	samples, err := r.recordFromReader(ctx, stdout, time.Now().Add(r.cfg.MaxDuration()))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return samples, nil
}

func (r *Recorder) recordFromReader(ctx context.Context, stdout io.Reader, deadline time.Time) ([]float32, error) {
	bytesPerChunk, err := pcmChunkBytes(r.cfg.SampleRate, r.cfg.Channels)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

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
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		n, readErr := io.ReadFull(stdout, buf)
		completeBytes := n - n%(pcmBytesPerSample*r.cfg.Channels)
		shouldFinish := false
		if completeBytes > 0 {
			chunk := pcmToFloat32(buf[:completeBytes])
			recording, shouldFinish = r.processChunk(recording, chunk)
		}
		if shouldFinish {
			break
		}
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			return nil, fmt.Errorf("reading audio: %w", readErr)
		}
	}

	if len(recording) < r.cfg.SampleRate*r.cfg.Channels/4 {
		return nil, nil
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

func pcmChunkBytes(sampleRate, channels int) (int, error) {
	if sampleRate <= 0 {
		return 0, fmt.Errorf("audio sample rate must be greater than zero")
	}
	if channels != 1 {
		return 0, fmt.Errorf("voice capture requires mono audio")
	}
	samplesPerChannel := (sampleRate*chunkDurationMS + 999) / 1000
	return samplesPerChannel * channels * pcmBytesPerSample, nil
}

// pcmToFloat32 converts raw S16_LE bytes to normalized float32.
func pcmToFloat32(raw []byte) []float32 {
	n := len(raw) / pcmBytesPerSample
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(raw[i*pcmBytesPerSample:]))
		out[i] = float32(s) / 32768.0
	}
	return out
}
