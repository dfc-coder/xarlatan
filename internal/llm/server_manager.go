package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ServerMode selects whether Xarlatan owns a local llama-server process or
// connects to a server supervised externally.
type ServerMode string

const (
	ServerModeManaged  ServerMode = "managed"
	ServerModeExternal ServerMode = "external"
)

// LifecycleErrorCode identifies deterministic server lifecycle failures.
type LifecycleErrorCode string

const (
	LifecycleErrorInvalidConfig  LifecycleErrorCode = "invalid_config"
	LifecycleErrorStartFailed    LifecycleErrorCode = "start_failed"
	LifecycleErrorEarlyExit      LifecycleErrorCode = "early_exit"
	LifecycleErrorStartupTimeout LifecycleErrorCode = "startup_timeout"
	LifecycleErrorCancelled      LifecycleErrorCode = "cancelled"
	LifecycleErrorShutdownFailed LifecycleErrorCode = "shutdown_failed"
)

// LifecycleError preserves a stable code and wrapped operational cause.
type LifecycleError struct {
	Code LifecycleErrorCode
	Op   string
	Err  error
}

func (e *LifecycleError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("llama-server %s: %s", e.Op, e.Code)
	}
	return fmt.Sprintf("llama-server %s: %s: %v", e.Op, e.Code, e.Err)
}

func (e *LifecycleError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsLifecycleErrorCode checks a wrapped lifecycle error code.
func IsLifecycleErrorCode(err error, code LifecycleErrorCode) bool {
	var lifecycleErr *LifecycleError
	return errors.As(err, &lifecycleErr) && lifecycleErr.Code == code
}

func lifecycleError(code LifecycleErrorCode, op string, err error) error {
	return &LifecycleError{Code: code, Op: op, Err: err}
}

// Process is the minimal subprocess boundary required by ServerManager.
type Process interface {
	Start() error
	Wait() error
	Signal(os.Signal) error
	Kill() error
}

// ProcessFactory creates a process without starting it.
type ProcessFactory interface {
	New(binary string, args ...string) Process
}

// HealthProbe reports whether an endpoint is ready.
type HealthProbe interface {
	Check(context.Context, string) error
}

// ServerConfig configures process ownership and bounded lifecycle operations.
type ServerConfig struct {
	Mode            ServerMode
	Binary          string
	Args            []string
	HealthURL       string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	HealthInterval  time.Duration
}

// ServerManager owns exactly one managed process and its single Wait call.
type ServerManager struct {
	cfg     ServerConfig
	factory ProcessFactory
	probe   HealthProbe

	mu       sync.Mutex
	state    serverState
	process  Process
	waitDone chan struct{}
	waitErr  error
	stopOnce sync.Once
	stopErr  error
}

type serverState uint8

const (
	serverStateNew serverState = iota
	serverStateStarted
	serverStateReady
	serverStateExited
	serverStateStopped
)

var errProcessExitedBeforeReady = errors.New("process exited before health became ready")

// NewServerManager validates lifecycle configuration and installs production
// process/probe boundaries when nil implementations are supplied.
func NewServerManager(cfg ServerConfig, factory ProcessFactory, probe HealthProbe) (*ServerManager, error) {
	if cfg.Mode == "" {
		cfg.Mode = ServerModeManaged
	}
	if cfg.Mode != ServerModeManaged && cfg.Mode != ServerModeExternal {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", fmt.Errorf("mode must be managed or external"))
	}
	if cfg.Mode == ServerModeManaged && strings.TrimSpace(cfg.Binary) == "" {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", fmt.Errorf("binary is required in managed mode"))
	}
	if cfg.StartupTimeout <= 0 {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", fmt.Errorf("startup timeout must be greater than zero"))
	}
	if cfg.ShutdownTimeout <= 0 {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", fmt.Errorf("shutdown timeout must be greater than zero"))
	}
	if cfg.HealthInterval <= 0 {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", fmt.Errorf("health interval must be greater than zero"))
	}
	if err := validateHealthURL(cfg.HealthURL); err != nil {
		return nil, lifecycleError(LifecycleErrorInvalidConfig, "configure", err)
	}
	if factory == nil {
		factory = execProcessFactory{}
	}
	if probe == nil {
		probe = NewHTTPHealthProbe(nil)
	}
	return &ServerManager{
		cfg:      cfg,
		factory:  factory,
		probe:    probe,
		state:    serverStateNew,
		waitDone: make(chan struct{}),
	}, nil
}

func validateHealthURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse health URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("health URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("health URL host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("health URL userinfo is not allowed")
	}
	return nil
}

// Start launches the managed process or marks external mode active without
// spawning anything.
func (m *ServerManager) Start(ctx context.Context) error {
	if m == nil {
		return lifecycleError(LifecycleErrorInvalidConfig, "start", fmt.Errorf("manager is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return lifecycleError(LifecycleErrorCancelled, "start", ctx.Err())
	default:
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != serverStateNew {
		return lifecycleError(LifecycleErrorInvalidConfig, "start", fmt.Errorf("invalid state %d", m.state))
	}
	if m.cfg.Mode == ServerModeExternal {
		m.state = serverStateStarted
		return nil
	}

	process := m.factory.New(m.cfg.Binary, m.cfg.Args...)
	if process == nil {
		return lifecycleError(LifecycleErrorStartFailed, "start", fmt.Errorf("process factory returned nil"))
	}
	if err := process.Start(); err != nil {
		m.state = serverStateStopped
		return lifecycleError(LifecycleErrorStartFailed, "start", err)
	}
	m.process = process
	m.state = serverStateStarted
	go m.waitProcess(process)
	return nil
}

func (m *ServerManager) waitProcess(process Process) {
	err := process.Wait()
	m.mu.Lock()
	m.waitErr = err
	if m.state != serverStateStopped {
		m.state = serverStateExited
	}
	m.mu.Unlock()
	close(m.waitDone)
}

// WaitReady probes health until success, process exit, caller cancellation or
// startup timeout. Any managed startup failure stops and reaps the process.
func (m *ServerManager) WaitReady(ctx context.Context) error {
	if m == nil {
		return lifecycleError(LifecycleErrorInvalidConfig, "wait-ready", fmt.Errorf("manager is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == serverStateReady {
		return nil
	}
	if state != serverStateStarted {
		return lifecycleError(LifecycleErrorInvalidConfig, "wait-ready", fmt.Errorf("server has not been started"))
	}

	startupCtx, cancel := context.WithTimeout(ctx, m.cfg.StartupTimeout)
	defer cancel()
	ticker := time.NewTicker(m.cfg.HealthInterval)
	defer ticker.Stop()

	for {
		if m.cfg.Mode == ServerModeManaged {
			select {
			case <-m.waitDone:
				return m.earlyExitError()
			default:
			}
		}

		if err := m.probe.Check(startupCtx, m.cfg.HealthURL); err == nil {
			if m.cfg.Mode == ServerModeManaged {
				select {
				case <-m.waitDone:
					return m.earlyExitError()
				default:
				}
			}
			m.mu.Lock()
			if m.state != serverStateStarted {
				exitErr := m.waitErr
				m.mu.Unlock()
				if exitErr == nil {
					exitErr = errProcessExitedBeforeReady
				}
				return lifecycleError(LifecycleErrorEarlyExit, "wait-ready", exitErr)
			}
			m.state = serverStateReady
			m.mu.Unlock()
			return nil
		}

		select {
		case <-ticker.C:
			continue
		case <-m.waitDone:
			if m.cfg.Mode == ServerModeManaged {
				return m.earlyExitError()
			}
		case <-startupCtx.Done():
			var readinessErr error
			if ctx.Err() != nil {
				readinessErr = lifecycleError(LifecycleErrorCancelled, "wait-ready", ctx.Err())
			} else {
				readinessErr = lifecycleError(LifecycleErrorStartupTimeout, "wait-ready", startupCtx.Err())
			}
			return m.cleanupReadinessFailure(readinessErr)
		}
	}
}

func (m *ServerManager) earlyExitError() error {
	m.mu.Lock()
	exitErr := m.waitErr
	m.mu.Unlock()
	if exitErr == nil {
		exitErr = errProcessExitedBeforeReady
	}
	return lifecycleError(LifecycleErrorEarlyExit, "wait-ready", exitErr)
}

func (m *ServerManager) cleanupReadinessFailure(readinessErr error) error {
	if m.cfg.Mode == ServerModeExternal {
		return readinessErr
	}
	if stopErr := m.Stop(context.Background()); stopErr != nil {
		return errors.Join(readinessErr, stopErr)
	}
	return readinessErr
}

// Wait returns the single stored process result. External mode has no process
// and therefore returns immediately.
func (m *ServerManager) Wait() error {
	if m == nil {
		return lifecycleError(LifecycleErrorInvalidConfig, "wait", fmt.Errorf("manager is nil"))
	}
	if m.cfg.Mode == ServerModeExternal {
		return nil
	}
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == serverStateNew {
		return lifecycleError(LifecycleErrorInvalidConfig, "wait", fmt.Errorf("server has not been started"))
	}
	<-m.waitDone
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.waitErr
}

// Stop performs one idempotent graceful-signal, bounded wait and kill fallback
// sequence. Cleanup continues even if the caller context is already cancelled.
func (m *ServerManager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.stopOnce.Do(func() {
		m.stopErr = m.stop(ctx)
	})
	return m.stopErr
}

func (m *ServerManager) stop(ctx context.Context) error {
	m.mu.Lock()
	if m.cfg.Mode == ServerModeExternal {
		m.state = serverStateStopped
		m.mu.Unlock()
		return nil
	}
	process := m.process
	state := m.state
	m.mu.Unlock()

	if process == nil || state == serverStateNew || state == serverStateStopped {
		m.markStopped()
		return nil
	}
	select {
	case <-m.waitDone:
		m.markStopped()
		return nil
	default:
	}

	signalErr := process.Signal(os.Interrupt)
	if waitChannel(ctx, m.waitDone, m.cfg.ShutdownTimeout) {
		m.markStopped()
		return nil
	}

	killErr := process.Kill()
	if killErr != nil {
		select {
		case <-m.waitDone:
			m.markStopped()
			return nil
		default:
		}
		return lifecycleError(LifecycleErrorShutdownFailed, "stop", errors.Join(signalErr, killErr))
	}
	if !waitChannel(context.Background(), m.waitDone, m.cfg.ShutdownTimeout) {
		return lifecycleError(LifecycleErrorShutdownFailed, "stop", fmt.Errorf("process did not exit after kill"))
	}
	m.markStopped()
	return nil
}

func waitChannel(ctx context.Context, done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (m *ServerManager) markStopped() {
	m.mu.Lock()
	m.state = serverStateStopped
	m.mu.Unlock()
}

type execProcessFactory struct{}

func (execProcessFactory) New(binary string, args ...string) Process {
	command := exec.Command(binary, args...)
	command.Stdout = io.Discard
	command.Stderr = os.Stderr
	return &execProcess{command: command}
}

type execProcess struct {
	command *exec.Cmd
}

func (p *execProcess) Start() error { return p.command.Start() }
func (p *execProcess) Wait() error  { return p.command.Wait() }
func (p *execProcess) Signal(signal os.Signal) error {
	if p.command.Process == nil {
		return fmt.Errorf("process has not started")
	}
	return p.command.Process.Signal(signal)
}
func (p *execProcess) Kill() error {
	if p.command.Process == nil {
		return fmt.Errorf("process has not started")
	}
	return p.command.Process.Kill()
}

// HTTPHealthProbe checks a health endpoint and always drains/closes its body.
type HTTPHealthProbe struct {
	client HTTPDoer
}

// NewHTTPHealthProbe creates a probe. A bounded default client is used when
// client is nil.
func NewHTTPHealthProbe(client HTTPDoer) *HTTPHealthProbe {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPHealthProbe{client: client}
}

// Check returns nil only for HTTP 200 and closes every received body.
func (p *HTTPHealthProbe) Check(ctx context.Context, healthURL string) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("health probe client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	if response == nil || response.Body == nil {
		return fmt.Errorf("health response has no body")
	}
	_, drainErr := io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
	closeErr := response.Body.Close()
	if drainErr != nil || closeErr != nil {
		return fmt.Errorf("close health response: %w", errors.Join(drainErr, closeErr))
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", response.StatusCode)
	}
	return nil
}
