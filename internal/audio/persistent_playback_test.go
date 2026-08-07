package audio

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestPersistentPlaybackReusesSessionForCompatibleBuffers(t *testing.T) {
	factory := newFakePlaybackFactory()
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	player.guard = playbackGuardFunc(func(context.Context) error { return nil })
	defer player.Close()

	buffer := Buffer{Samples: []float32{0.1, 0.2}, SampleRate: 22050, Channels: 1}
	if err := player.Play(context.Background(), buffer); err != nil {
		t.Fatalf("first Play() error = %v", err)
	}
	if err := player.Play(context.Background(), buffer); err != nil {
		t.Fatalf("second Play() error = %v", err)
	}

	if got := factory.startCount(); got != 1 {
		t.Fatalf("aplay starts = %d, want 1", got)
	}
	process := factory.session(0)
	if got := process.writeCount(); got != 2 {
		t.Fatalf("writes = %d, want 2", got)
	}
}

func TestPersistentPlaybackRestartsWhenFormatChanges(t *testing.T) {
	factory := newFakePlaybackFactory()
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	player.guard = playbackGuardFunc(func(context.Context) error { return nil })
	defer player.Close()

	if err := player.Play(context.Background(), Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1}); err != nil {
		t.Fatalf("first Play() error = %v", err)
	}
	if err := player.Play(context.Background(), Buffer{Samples: []float32{0.2}, SampleRate: 22050, Channels: 1}); err != nil {
		t.Fatalf("second Play() error = %v", err)
	}

	if got := factory.startCount(); got != 2 {
		t.Fatalf("aplay starts = %d, want 2", got)
	}
	if got := factory.session(0).stops(); got != 1 {
		t.Fatalf("first process stops = %d, want 1", got)
	}
}

func TestPersistentPlaybackCancellationStopsActiveSession(t *testing.T) {
	factory := newFakePlaybackFactory()
	factory.blockWrites = true
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	player.guard = playbackGuardFunc(func(context.Context) error { return nil })
	defer player.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- player.Write(ctx, Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1})
	}()
	process := <-factory.started
	<-process.writeStarted
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() error = %v, want context.Canceled", err)
	}
	if got := process.stops(); got != 1 {
		t.Fatalf("process stops = %d, want 1", got)
	}
}

func TestPersistentPlaybackStopAllowsLazyRestart(t *testing.T) {
	factory := newFakePlaybackFactory()
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	defer player.Close()

	buffer := Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1}
	if err := player.Write(context.Background(), buffer); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if err := player.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := player.Write(context.Background(), buffer); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}

	if got := factory.startCount(); got != 2 {
		t.Fatalf("aplay starts = %d, want 2", got)
	}
}

func TestPersistentPlaybackCloseIsIdempotentAndTerminal(t *testing.T) {
	factory := newFakePlaybackFactory()
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	buffer := Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1}

	if err := player.Write(context.Background(), buffer); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := player.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := player.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := factory.session(0).stops(); got != 1 {
		t.Fatalf("process stops = %d, want 1", got)
	}
	if err := player.Write(context.Background(), buffer); err == nil {
		t.Fatal("Write() after Close() = nil, want error")
	}
}

type fakePlaybackFactory struct {
	mu          sync.Mutex
	sessions    []*fakePlaybackProcess
	started     chan *fakePlaybackProcess
	blockWrites bool
	writeErr    error
	onWrite     func()
}

func newFakePlaybackFactory() *fakePlaybackFactory {
	return &fakePlaybackFactory{started: make(chan *fakePlaybackProcess, 16)}
}

func (f *fakePlaybackFactory) Start(_ string, sampleRate, channels int) (playbackProcess, error) {
	process := &fakePlaybackProcess{
		sampleRate:   sampleRate,
		channels:     channels,
		blockWrite:   f.blockWrites,
		writeErr:     f.writeErr,
		onWrite:      f.onWrite,
		stopped:      make(chan struct{}),
		writeStarted: make(chan struct{}),
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, process)
	f.mu.Unlock()
	f.started <- process
	return process, nil
}

func (f *fakePlaybackFactory) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

func (f *fakePlaybackFactory) session(index int) *fakePlaybackProcess {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[index]
}

type fakePlaybackProcess struct {
	mu           sync.Mutex
	sampleRate   int
	channels     int
	writes       [][]byte
	stopCount    int
	blockWrite   bool
	writeErr     error
	onWrite      func()
	stopped      chan struct{}
	stopOnce     sync.Once
	writeStarted chan struct{}
	writeOnce    sync.Once
}

func (p *fakePlaybackProcess) Write(data []byte) (int, error) {
	p.writeOnce.Do(func() { close(p.writeStarted) })
	if p.onWrite != nil {
		p.onWrite()
	}
	if p.blockWrite {
		<-p.stopped
		return 0, errors.New("playback stopped")
	}
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	copyData := append([]byte(nil), data...)
	p.mu.Lock()
	p.writes = append(p.writes, copyData)
	p.mu.Unlock()
	return len(data), nil
}

func (p *fakePlaybackProcess) Stop() error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopCount++
		p.mu.Unlock()
		close(p.stopped)
	})
	return nil
}

func (p *fakePlaybackProcess) writeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.writes)
}

func (p *fakePlaybackProcess) stops() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopCount
}
