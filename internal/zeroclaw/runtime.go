package zeroclaw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const defaultShutdownTimeout = 2 * time.Second

// RuntimeConfig controls the owned ZeroClaw ACP subprocess.
type RuntimeConfig struct {
	Binary          string
	AgentAlias      string
	CWD             string
	ShutdownTimeout time.Duration
}

// Runtime owns exactly one `zeroclaw acp` child and its ACP client.
type Runtime struct {
	cfg RuntimeConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	client *Client
	voice  *VoiceResponder
	waitCh chan error

	closeOnce sync.Once
	closeErr  error
}

// StartRuntime launches ZeroClaw, completes ACP initialization, and returns only
// after the remote session is ready for voice turns.
func StartRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg.Binary = strings.TrimSpace(cfg.Binary)
	if cfg.Binary == "" {
		return nil, errors.New("zeroclaw runtime binary is required")
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}

	cmd := exec.Command(cfg.Binary, "acp")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("zeroclaw stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("zeroclaw stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start zeroclaw acp: %w", err)
	}

	runtime := &Runtime{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		waitCh: make(chan error, 1),
	}
	go func() {
		runtime.waitCh <- cmd.Wait()
		close(runtime.waitCh)
	}()

	client, err := NewClient(stdout, stdin, Config{AgentAlias: cfg.AgentAlias, CWD: cfg.CWD})
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("zeroclaw ACP client: %w", err)
	}
	runtime.client = client
	voice, err := NewVoiceResponder(client)
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("zeroclaw voice responder: %w", err)
	}
	runtime.voice = voice

	if err := client.Initialize(ctx); err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("initialize zeroclaw ACP: %w", err)
	}
	return runtime, nil
}

// Responder returns the initialized ZeroClaw responder used by Coordinator.
func (r *Runtime) Responder() (*VoiceResponder, error) {
	if r == nil || r.voice == nil {
		return nil, errors.New("zeroclaw runtime is not initialized")
	}
	return r.voice, nil
}

// Close stops and reaps the owned child exactly once. Normal shutdown is stdin
// EOF; Kill is used only when the child exceeds the bounded shutdown timeout.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var errs []error
		if r.client != nil {
			if err := r.client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close zeroclaw ACP client: %w", err))
			}
		} else if r.stdin != nil {
			if err := r.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, fmt.Errorf("close zeroclaw stdin: %w", err))
			}
		}

		waitErr, timedOut := r.waitForExit(r.cfg.ShutdownTimeout)
		if timedOut && r.cmd != nil && r.cmd.Process != nil {
			if err := r.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				errs = append(errs, fmt.Errorf("kill zeroclaw ACP: %w", err))
			}
			waitErr, _ = r.waitForExit(r.cfg.ShutdownTimeout)
		}
		if waitErr != nil && !isExpectedExitAfterKill(waitErr, timedOut) {
			errs = append(errs, fmt.Errorf("wait zeroclaw ACP: %w", waitErr))
		}
		if r.stdout != nil {
			if err := r.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, fmt.Errorf("close zeroclaw stdout: %w", err))
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

func (r *Runtime) waitForExit(timeout time.Duration) (error, bool) {
	if r == nil || r.waitCh == nil {
		return nil, false
	}
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err, ok := <-r.waitCh:
		if !ok {
			return nil, false
		}
		return err, false
	case <-timer.C:
		return nil, true
	}
}

func isExpectedExitAfterKill(err error, killed bool) bool {
	if err == nil {
		return true
	}
	if !killed {
		return false
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
