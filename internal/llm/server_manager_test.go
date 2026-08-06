package llm

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProcess struct {
	startErr     error
	waitErr      error
	signalErr    error
	killErr      error
	exitOnSignal bool
	exitOnKill   bool
	started      atomic.Int32
	signals      atomic.Int32
	kills        atomic.Int32
	waitCalls    atomic.Int32
	exitOnce     sync.Once
	exit         chan struct{}
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{exit: make(chan struct{}), exitOnSignal: true, exitOnKill: true}
}

func (p *fakeProcess) Start() error {
	p.started.Add(1)
	return p.startErr
}

func (p *fakeProcess) Wait() error {
	p.waitCalls.Add(1)
	<-p.exit
	return p.waitErr
}

func (p *fakeProcess) Signal(os.Signal) error {
	p.signals.Add(1)
	if p.exitOnSignal {
		p.finish()
	}
	return p.signalErr
}

func (p *fakeProcess) Kill() error {
	p.kills.Add(1)
	if p.exitOnKill {
		p.finish()
	}
	return p.killErr
}

func (p *fakeProcess) finish() {
	p.exitOnce.Do(func() { close(p.exit) })
}

type fakeProcessFactory struct {
	process Process
	calls   atomic.Int32
	binary  string
	args    []string
}

func (f *fakeProcessFactory) New(binary string, args ...string) Process {
	f.calls.Add(1)
	f.binary = binary
	f.args = append([]string(nil), args...)
	return f.process
}

type fakeHealthProbe struct {
	check func(context.Context, string) error
	calls atomic.Int32
}

func (p *fakeHealthProbe) Check(ctx context.Context, healthURL string) error {
	p.calls.Add(1)
	if p.check == nil {
		return nil
	}
	return p.check(ctx, healthURL)
}

func testServerConfig() ServerConfig {
	return ServerConfig{
		Mode:            ServerModeManaged,
		Binary:          "llama-server",
		Args:            []string{"--model", "model.gguf"},
		HealthURL:       "http://127.0.0.1:8080/health",
		StartupTimeout:  100 * time.Millisecond,
		ShutdownTimeout: 20 * time.Millisecond,
		HealthInterval:  time.Millisecond,
	}
}

func TestServerManagerFailsForMissingBinary(t *testing.T) {
	cfg := testServerConfig()
	cfg.Binary = t.TempDir() + "/missing-llama-server"
	manager, err := NewServerManager(cfg, nil, &fakeHealthProbe{})
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}

	err = manager.Start(context.Background())
	if !IsLifecycleErrorCode(err, LifecycleErrorStartFailed) {
		t.Fatalf("Start() error = %v, want %s", err, LifecycleErrorStartFailed)
	}
}

func TestServerManagerDetectsEarlyExit(t *testing.T) {
	process := newFakeProcess()
	process.waitErr = errors.New("exit status 2")
	factory := &fakeProcessFactory{process: process}
	probe := &fakeHealthProbe{check: func(context.Context, string) error {
		return errors.New("not ready")
	}}
	manager, err := NewServerManager(testServerConfig(), factory, probe)
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	process.finish()

	err = manager.WaitReady(context.Background())
	if !IsLifecycleErrorCode(err, LifecycleErrorEarlyExit) {
		t.Fatalf("WaitReady() error = %v, want %s", err, LifecycleErrorEarlyExit)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("WaitReady() error = %v, want process cause", err)
	}
	if got := process.waitCalls.Load(); got != 1 {
		t.Fatalf("Wait calls = %d, want 1", got)
	}
}

func TestServerManagerTimesOutWhenHealthNeverReady(t *testing.T) {
	process := newFakeProcess()
	factory := &fakeProcessFactory{process: process}
	probe := &fakeHealthProbe{check: func(context.Context, string) error {
		return errors.New("not ready")
	}}
	cfg := testServerConfig()
	cfg.StartupTimeout = 15 * time.Millisecond
	manager, err := NewServerManager(cfg, factory, probe)
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = manager.WaitReady(context.Background())
	if !IsLifecycleErrorCode(err, LifecycleErrorStartupTimeout) {
		t.Fatalf("WaitReady() error = %v, want %s", err, LifecycleErrorStartupTimeout)
	}
	if got := process.signals.Load(); got != 1 {
		t.Fatalf("signals = %d, want 1", got)
	}
	if got := process.waitCalls.Load(); got != 1 {
		t.Fatalf("Wait calls = %d, want 1", got)
	}
}

func TestServerManagerStopsOnContextCancellation(t *testing.T) {
	process := newFakeProcess()
	factory := &fakeProcessFactory{process: process}
	probe := &fakeHealthProbe{check: func(context.Context, string) error {
		return errors.New("not ready")
	}}
	manager, err := NewServerManager(testServerConfig(), factory, probe)
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = manager.WaitReady(ctx)
	if !IsLifecycleErrorCode(err, LifecycleErrorCancelled) {
		t.Fatalf("WaitReady() error = %v, want %s", err, LifecycleErrorCancelled)
	}
	if got := process.signals.Load(); got != 1 {
		t.Fatalf("signals = %d, want 1", got)
	}
	if got := process.waitCalls.Load(); got != 1 {
		t.Fatalf("Wait calls = %d, want 1", got)
	}
}

func TestServerManagerStopIsIdempotent(t *testing.T) {
	process := newFakeProcess()
	factory := &fakeProcessFactory{process: process}
	manager, err := NewServerManager(testServerConfig(), factory, &fakeHealthProbe{})
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- manager.Stop(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	}
	if got := process.signals.Load(); got != 1 {
		t.Fatalf("signals = %d, want 1", got)
	}
	if got := process.kills.Load(); got != 0 {
		t.Fatalf("kills = %d, want 0", got)
	}
	if got := process.waitCalls.Load(); got != 1 {
		t.Fatalf("Wait calls = %d, want 1", got)
	}
}

func TestServerManagerExternalModeDoesNotSpawn(t *testing.T) {
	factory := &fakeProcessFactory{process: newFakeProcess()}
	probe := &fakeHealthProbe{}
	cfg := testServerConfig()
	cfg.Mode = ServerModeExternal
	cfg.Binary = ""
	cfg.Args = nil
	manager, err := NewServerManager(cfg, factory, probe)
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := factory.calls.Load(); got != 0 {
		t.Fatalf("factory calls = %d, want 0", got)
	}
}

func TestServerManagerSignalsThenKillsAfterTimeout(t *testing.T) {
	process := newFakeProcess()
	process.exitOnSignal = false
	factory := &fakeProcessFactory{process: process}
	cfg := testServerConfig()
	cfg.ShutdownTimeout = 5 * time.Millisecond
	manager, err := NewServerManager(cfg, factory, &fakeHealthProbe{})
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := process.signals.Load(); got != 1 {
		t.Fatalf("signals = %d, want 1", got)
	}
	if got := process.kills.Load(); got != 1 {
		t.Fatalf("kills = %d, want 1", got)
	}
	if got := process.waitCalls.Load(); got != 1 {
		t.Fatalf("Wait calls = %d, want 1", got)
	}
}

func TestServerManagerLeavesNoOrphanAfterStartupCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal integration uses Unix test-process behavior")
	}
	cfg := testServerConfig()
	cfg.Binary = os.Args[0]
	cfg.Args = []string{"-test.run=TestLLMHelperProcess", "--", "--llm-helper-process"}
	cfg.StartupTimeout = time.Second
	cfg.ShutdownTimeout = time.Second
	probe := &fakeHealthProbe{check: func(context.Context, string) error {
		return errors.New("not ready")
	}}
	manager, err := NewServerManager(cfg, nil, probe)
	if err != nil {
		t.Fatalf("NewServerManager() error = %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.WaitReady(ctx); !IsLifecycleErrorCode(err, LifecycleErrorCancelled) {
		t.Fatalf("WaitReady() error = %v, want %s", err, LifecycleErrorCancelled)
	}

	done := make(chan error, 1)
	go func() { done <- manager.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("managed subprocess was not reaped")
	}
}

func TestLLMHelperProcess(t *testing.T) {
	found := false
	for _, arg := range os.Args {
		if arg == "--llm-helper-process" {
			found = true
			break
		}
	}
	if !found {
		return
	}
	ch := make(chan os.Signal, 1)
	// The default exec-backed process receives os.Interrupt during graceful stop.
	// Exiting here lets the parent assert that Wait completed and no child remains.
	signal.Notify(ch, os.Interrupt)
	<-ch
	os.Exit(0)
}
