package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
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

type playbackProcess interface {
	Write([]byte) (int, error)
	Drain() error
	Stop() error
}

type playbackProcessFactory interface {
	Start(device string, sampleRate, channels int) (playbackProcess, error)
}

type execPlaybackProcessFactory struct{}

func (execPlaybackProcessFactory) Start(device string, sampleRate, channels int) (playbackProcess, error) {
	cmd := exec.Command("aplay", playbackArgs(device, sampleRate, channels)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("aplay stdin: %w", err)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("aplay start: %w", err)
	}
	process := &execPlaybackProcess{
		cmd:   cmd,
		stdin: stdin,
		done:  make(chan error, 1),
	}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

type execPlaybackProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	done      chan error
	inputOnce sync.Once
	killOnce  sync.Once
	waitOnce  sync.Once
	waitErr   error
	killErr   error
}

func (p *execPlaybackProcess) Write(data []byte) (int, error) {
	if p == nil || p.stdin == nil {
		return 0, fmt.Errorf("aplay process is not initialized")
	}
	return p.stdin.Write(data)
}

func (p *execPlaybackProcess) closeInput() {
	if p == nil {
		return
	}
	p.inputOnce.Do(func() {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
	})
}

func (p *execPlaybackProcess) wait() error {
	if p == nil {
		return nil
	}
	p.waitOnce.Do(func() {
		if p.done == nil {
			return
		}
		if err, ok := <-p.done; ok {
			p.waitErr = err
		}
	})
	return p.waitErr
}

// Drain closes stdin and waits for aplay to consume all queued PCM and exit.
func (p *execPlaybackProcess) Drain() error {
	if p == nil {
		return nil
	}
	p.closeInput()
	if err := p.wait(); err != nil {
		return fmt.Errorf("aplay drain: %w", err)
	}
	return nil
}

// Stop aborts playback immediately and waits for process collection.
func (p *execPlaybackProcess) Stop() error {
	if p == nil {
		return nil
	}
	p.closeInput()
	p.killOnce.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.killErr = err
			}
		}
	})
	_ = p.wait()
	return p.killErr
}

// Playback owns one reusable ALSA playback session for the duration of a
// response. Write appends chunks, Finish drains audible playback and runs the
// microphone rearm guard, and Stop provides the immediate barge-in primitive.
type Playback struct {
	device     string
	sampleRate int
	channels   int
	runner     commandRunner
	guard      playbackGuard
	factory    playbackProcessFactory

	writeMu sync.Mutex
	mu      sync.Mutex
	session playbackProcess
	rate    int
	ch      int
	closed  bool
}

// NewPlayback creates a half-duplex Playback instance. The underlying aplay
// process is started lazily and reused by all compatible Write calls until
// Finish, Stop, a format change or Close.
func NewPlayback(device string, sampleRate, channels int) *Playback {
	return &Playback{
		device:     device,
		sampleRate: sampleRate,
		channels:   channels,
		runner:     execRunner{},
		guard:      newMicrophoneRearmGuard(device, sampleRate, channels),
		factory:    execPlaybackProcessFactory{},
	}
}

// Play preserves the complete-response API by writing one buffer and then
// waiting for audible playback to drain before rearming the microphone.
func (p *Playback) Play(ctx context.Context, buffer Buffer) error {
	if buffer.Empty() {
		return nil
	}
	ctx = nonNilAudioContext(ctx)
	if err := p.Write(ctx, buffer); err != nil {
		return err
	}
	return p.Finish(ctx)
}

// Write appends PCM to the reusable playback process without draining or
// running the post-playback guard. Streaming callers issue one Write per chunk.
func (p *Playback) Write(ctx context.Context, buffer Buffer) error {
	if buffer.Empty() {
		return nil
	}
	if p == nil || p.factory == nil {
		return fmt.Errorf("playback is not initialized")
	}
	if err := buffer.Validate(); err != nil {
		return err
	}
	ctx = nonNilAudioContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	session, err := p.ensureSession(buffer.SampleRate, buffer.Channels)
	if err != nil {
		return err
	}
	pcm := float32ToPCM16(buffer.Samples)
	if err := writePlaybackProcess(ctx, session, pcm); err != nil {
		_ = p.resetSession(session)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("aplay write: %w", err)
	}
	return nil
}

// Finish ends the current response: it gracefully closes the playback input,
// waits until queued audio has been consumed, and only then rearms the mic.
func (p *Playback) Finish(ctx context.Context) error {
	if p == nil || p.guard == nil {
		return fmt.Errorf("playback is not initialized")
	}
	ctx = nonNilAudioContext(ctx)
	if err := ctx.Err(); err != nil {
		_ = p.Stop()
		return err
	}

	p.writeMu.Lock()
	p.mu.Lock()
	session := p.session
	p.session = nil
	p.rate = 0
	p.ch = 0
	p.mu.Unlock()
	p.writeMu.Unlock()

	if session == nil {
		return nil
	}
	if err := drainPlaybackProcess(ctx, session); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	if err := p.guard.Wait(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("post-playback guard: %w", err)
	}
	return nil
}

// Stop immediately discards the active playback session. A later Write lazily
// starts a fresh process, which is the primitive used by future barge-in.
func (p *Playback) Stop() error {
	if p == nil {
		return nil
	}
	p.writeMu.Lock()
	p.mu.Lock()
	session := p.session
	p.session = nil
	p.rate = 0
	p.ch = 0
	p.mu.Unlock()
	p.writeMu.Unlock()
	if session == nil {
		return nil
	}
	return session.Stop()
}

// Close permanently releases playback resources. It is safe to call more than
// once; subsequent Write calls fail rather than recreating a process.
func (p *Playback) Close() error {
	if p == nil {
		return nil
	}
	p.writeMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.writeMu.Unlock()
		return nil
	}
	p.closed = true
	session := p.session
	p.session = nil
	p.rate = 0
	p.ch = 0
	p.mu.Unlock()
	p.writeMu.Unlock()
	if session == nil {
		return nil
	}
	return session.Stop()
}

func (p *Playback) ensureSession(sampleRate, channels int) (playbackProcess, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("playback is closed")
	}
	if p.session != nil && p.rate == sampleRate && p.ch == channels {
		return p.session, nil
	}
	if p.session != nil {
		if err := p.session.Stop(); err != nil {
			p.session = nil
			p.rate = 0
			p.ch = 0
			return nil, fmt.Errorf("stopping incompatible playback session: %w", err)
		}
		p.session = nil
		p.rate = 0
		p.ch = 0
	}
	session, err := p.factory.Start(p.device, sampleRate, channels)
	if err != nil {
		return nil, err
	}
	p.session = session
	p.rate = sampleRate
	p.ch = channels
	return session, nil
}

func (p *Playback) resetSession(target playbackProcess) error {
	p.mu.Lock()
	if p.session != target {
		p.mu.Unlock()
		return nil
	}
	p.session = nil
	p.rate = 0
	p.ch = 0
	p.mu.Unlock()
	return target.Stop()
}

func writePlaybackProcess(ctx context.Context, process playbackProcess, data []byte) error {
	if process == nil {
		return fmt.Errorf("playback process is nil")
	}
	done := make(chan error, 1)
	go func() {
		n, err := process.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = process.Stop()
		<-done
		return ctx.Err()
	}
}

func drainPlaybackProcess(ctx context.Context, process playbackProcess) error {
	if process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- process.Drain() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = process.Stop()
		<-done
		return ctx.Err()
	}
}

func nonNilAudioContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// PlayRaw preserves the legacy one-shot raw-PCM entry point for callers outside
// the voice application. New streaming code should use Write/Finish.
func (p *Playback) PlayRaw(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if p == nil || p.runner == nil {
		return fmt.Errorf("playback is not initialized")
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return fmt.Errorf("playback is closed")
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
