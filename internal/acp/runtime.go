package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultShutdownTimeout = 2 * time.Second

// RuntimeConfig controls the owned ACP subprocess.
type RuntimeConfig struct {
	Binary          string
	Args            []string
	CWD             string
	ShutdownTimeout time.Duration
}

// Runtime owns exactly one configured ACP child and its initialized client.
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

// StartRuntime launches the configured ACP server and returns after the session
// is ready for voice turns.
func StartRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRuntimeConfig(&cfg); err != nil {
		return nil, err
	}

	cmd := exec.Command(cfg.Binary, cfg.Args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("ACP stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("ACP stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start ACP command: %w", err)
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

	client, err := NewClient(stdout, stdin, ClientConfig{CWD: cfg.CWD})
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("ACP client: %w", err)
	}
	runtime.client = client
	voice, err := NewVoiceResponder(client)
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("ACP voice responder: %w", err)
	}
	runtime.voice = voice

	if err := client.Initialize(ctx); err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("initialize ACP: %w", err)
	}
	return runtime, nil
}

// Responder returns the initialized responder used by Coordinator.
func (r *Runtime) Responder() (*VoiceResponder, error) {
	if r == nil || r.voice == nil {
		return nil, errors.New("ACP runtime is not initialized")
	}
	return r.voice, nil
}

// AgentInfo returns metadata advertised by the active ACP runtime.
func (r *Runtime) AgentInfo() AgentInfo {
	if r == nil || r.client == nil {
		return AgentInfo{}
	}
	return r.client.AgentInfo()
}

// Close stops and reaps the owned child exactly once. Normal shutdown is stdin
// EOF; Kill is used only after the bounded shutdown timeout.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var errs []error
		if r.client != nil {
			if err := r.client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close ACP client: %w", err))
			}
		} else if r.stdin != nil {
			if err := r.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, fmt.Errorf("close ACP stdin: %w", err))
			}
		}

		waitErr, timedOut := r.waitForExit(r.cfg.ShutdownTimeout)
		if timedOut && r.cmd != nil && r.cmd.Process != nil {
			if err := r.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				errs = append(errs, fmt.Errorf("kill ACP child: %w", err))
			}
			waitErr, _ = r.waitForExit(r.cfg.ShutdownTimeout)
		}
		if waitErr != nil && !isExpectedExitAfterKill(waitErr, timedOut) {
			errs = append(errs, fmt.Errorf("wait ACP child: %w", waitErr))
		}
		if r.stdout != nil {
			if err := r.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, fmt.Errorf("close ACP stdout: %w", err))
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

func validateRuntimeConfig(cfg *RuntimeConfig) error {
	cfg.Binary = strings.TrimSpace(cfg.Binary)
	if cfg.Binary == "" {
		return errors.New("ACP runtime binary is required")
	}
	for i := range cfg.Args {
		cfg.Args[i] = strings.TrimSpace(cfg.Args[i])
		if cfg.Args[i] == "" {
			return fmt.Errorf("ACP runtime arg %d must not be blank", i)
		}
	}
	cfg.CWD = strings.TrimSpace(cfg.CWD)
	if cfg.CWD == "" {
		return errors.New("ACP runtime cwd is required")
	}
	if !filepath.IsAbs(cfg.CWD) {
		return errors.New("ACP runtime cwd must be an absolute path")
	}
	info, err := os.Stat(cfg.CWD)
	if err != nil {
		return fmt.Errorf("ACP runtime cwd: %w", err)
	}
	if !info.IsDir() {
		return errors.New("ACP runtime cwd must be a directory")
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	return nil
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
