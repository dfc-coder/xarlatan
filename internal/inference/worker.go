package inference

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

const defaultWorkerShutdownTimeout = 2 * time.Second

// WorkerConfig configures one persistent OpenVINO inference worker.
type WorkerConfig struct {
	Python          string
	Script          string
	Mode            string
	ModelDir        string
	VoiceFile       string
	Language        string
	RequestedDevice string
	FallbackDevice  string
	CacheDir        string
	Env             []string
	ShutdownTimeout time.Duration
}

// Worker owns one persistent subprocess at a time and restarts it only after a
// crash or a cancelled inference. Calls are serialized to keep framing simple.
type Worker struct {
	cfg WorkerConfig

	opMu sync.Mutex
	mu   sync.Mutex

	cmd    *exec.Cmd
	client *Client
	waitCh chan error
	live   bool
	closed bool

	closeOnce sync.Once
	closeErr  error
}

func StartWorker(ctx context.Context, cfg WorkerConfig) (*Worker, error) {
	cfg.Python = strings.TrimSpace(cfg.Python)
	cfg.Script = strings.TrimSpace(cfg.Script)
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	cfg.RequestedDevice = strings.TrimSpace(cfg.RequestedDevice)
	cfg.FallbackDevice = strings.TrimSpace(cfg.FallbackDevice)
	if cfg.Python == "" {
		return nil, errors.New("inference worker python is required")
	}
	if cfg.Script == "" {
		return nil, errors.New("inference worker script is required")
	}
	if cfg.Mode != "stt" && cfg.Mode != "tts" {
		return nil, fmt.Errorf("inference worker mode must be stt or tts, got %q", cfg.Mode)
	}
	if cfg.RequestedDevice == "" {
		cfg.RequestedDevice = "CPU"
	}
	if cfg.FallbackDevice == "" {
		cfg.FallbackDevice = "CPU"
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultWorkerShutdownTimeout
	}
	worker := &Worker{cfg: cfg}
	if err := worker.ensureStarted(nonNilContext(ctx)); err != nil {
		return nil, err
	}
	if _, err := worker.Health(nonNilContext(ctx)); err != nil {
		_ = worker.Close()
		return nil, fmt.Errorf("inference worker readiness: %w", err)
	}
	return worker, nil
}

func (w *Worker) Health(ctx context.Context) (Health, error) {
	return runWorkerCall(w, ctx, func(client *Client) (Health, error) {
		return client.Health(ctx)
	})
}

func (w *Worker) Transcribe(ctx context.Context, buffer audio.Buffer) (string, error) {
	return runWorkerCall(w, ctx, func(client *Client) (string, error) {
		return client.Transcribe(ctx, buffer)
	})
}

func (w *Worker) Synthesize(ctx context.Context, text string) (audio.Buffer, error) {
	return runWorkerCall(w, ctx, func(client *Client) (audio.Buffer, error) {
		return client.Synthesize(ctx, text)
	})
}

func runWorkerCall[T any](w *Worker, ctx context.Context, call func(*Client) (T, error)) (T, error) {
	var zero T
	if w == nil {
		return zero, errors.New("inference worker is nil")
	}
	ctx = nonNilContext(ctx)
	w.opMu.Lock()
	defer w.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := w.ensureStarted(ctx); err != nil {
		return zero, err
	}

	w.mu.Lock()
	client := w.client
	w.mu.Unlock()
	if client == nil {
		return zero, errors.New("inference worker client is unavailable")
	}

	type outcome struct {
		value T
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := call(client)
		done <- outcome{value: value, err: err}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			w.reapIfExited()
		}
		return result.value, result.err
	case <-ctx.Done():
		w.abortProcess()
		select {
		case <-done:
		case <-time.After(w.cfg.ShutdownTimeout):
		}
		return zero, ctx.Err()
	}
}

func (w *Worker) ensureStarted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("inference worker is closed")
	}
	if w.live && w.client != nil {
		return nil
	}
	return w.startLocked()
}

func (w *Worker) startLocked() error {
	args := []string{w.cfg.Script,
		"--mode", w.cfg.Mode,
		"--device", w.cfg.RequestedDevice,
		"--fallback-device", w.cfg.FallbackDevice,
	}
	if w.cfg.ModelDir != "" {
		args = append(args, "--model-dir", w.cfg.ModelDir)
	}
	if w.cfg.VoiceFile != "" {
		args = append(args, "--voice-file", w.cfg.VoiceFile)
	}
	if w.cfg.Language != "" {
		args = append(args, "--language", w.cfg.Language)
	}
	if w.cfg.CacheDir != "" {
		args = append(args, "--cache-dir", w.cfg.CacheDir)
	}

	cmd := exec.Command(w.cfg.Python, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), w.cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("inference worker stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("inference worker stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("start inference worker: %w", err)
	}
	client, err := NewClient(stdout, stdin)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		close(waitCh)
	}()
	w.cmd = cmd
	w.client = client
	w.waitCh = waitCh
	w.live = true
	return nil
}

func (w *Worker) reapIfExited() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.live || w.waitCh == nil {
		return
	}
	select {
	case <-w.waitCh:
		if w.client != nil {
			_ = w.client.Close()
		}
		w.client = nil
		w.live = false
	default:
	}
}

func (w *Worker) abortProcess() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopLocked(true)
}

func (w *Worker) stopLocked(force bool) {
	if !w.live {
		return
	}
	if w.client != nil {
		_ = w.client.Close()
	}
	if force && w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	if w.waitCh != nil {
		timer := time.NewTimer(w.cfg.ShutdownTimeout)
		select {
		case <-w.waitCh:
			timer.Stop()
		case <-timer.C:
			if w.cmd != nil && w.cmd.Process != nil {
				_ = w.cmd.Process.Kill()
			}
			select {
			case <-w.waitCh:
			case <-time.After(w.cfg.ShutdownTimeout):
			}
		}
	}
	w.client = nil
	w.live = false
}

func (w *Worker) Close() error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() {
		w.opMu.Lock()
		defer w.opMu.Unlock()
		w.mu.Lock()
		defer w.mu.Unlock()
		w.closed = true
		w.stopLocked(false)
	})
	return w.closeErr
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
