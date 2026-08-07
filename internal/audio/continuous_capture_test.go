package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestContinuousRecorderReusesSingleCaptureAcrossTurns(t *testing.T) {
	cfg := config.AudioConfig{SampleRate: 1000, Channels: 1, Device: "default"}
	detector := &fakeDetector{steps: []detectorStep{
		{}, {}, {speaking: true}, {speaking: true, finish: true},
		{}, {}, {speaking: true}, {speaking: true, finish: true},
	}}
	factory := &fakeContinuousFactory{data: pcmChunks(8, 80, 0.5)}
	recorder, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 2, QueueDepth: 4}, factory)
	if err != nil {
		t.Fatalf("newContinuousRecorder() error = %v", err)
	}
	defer recorder.Close()

	first, err := recorder.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	second, err := recorder.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if first.Empty() || second.Empty() {
		t.Fatalf("utterances empty: first=%d second=%d", len(first.Samples), len(second.Samples))
	}
	if factory.starts() != 1 {
		t.Fatalf("capture starts = %d, want 1", factory.starts())
	}
}

func TestContinuousRecorderCancelledNextDoesNotStopSource(t *testing.T) {
	cfg := config.AudioConfig{SampleRate: 1000, Channels: 1, Device: "default"}
	detector := &fakeDetector{}
	process := newBlockingCaptureProcess()
	factory := &fakeContinuousFactory{process: process}
	recorder, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 2, QueueDepth: 2}, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := recorder.Next(ctx)
		done <- err
	}()
	<-process.readStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Next() error = %v, want canceled", err)
	}
	if process.stopCount() != 0 {
		t.Fatalf("process stopped with turn cancellation")
	}
	if factory.starts() != 1 {
		t.Fatalf("capture starts = %d, want 1", factory.starts())
	}
}

func TestContinuousRecorderQueueDropsOldestAndStaysBounded(t *testing.T) {
	cfg := config.AudioConfig{SampleRate: 1000, Channels: 1, Device: "default"}
	detector := &fakeDetector{}
	recorder, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 1, QueueDepth: 2}, &fakeContinuousFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()

	for value := float32(1); value <= 3; value++ {
		recorder.publishUtterance(makeSamples(300, value))
	}
	if got := len(recorder.utterances); got != 2 {
		t.Fatalf("queue length = %d, want 2", got)
	}
	if got := recorder.DroppedUtterances(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	first := <-recorder.utterances
	if first.Samples[0] != 2 {
		t.Fatalf("oldest retained sample = %v, want 2", first.Samples[0])
	}
}

func TestContinuousRecorderCloseIsIdempotent(t *testing.T) {
	cfg := config.AudioConfig{SampleRate: 1000, Channels: 1, Device: "default"}
	detector := &fakeDetector{}
	process := newBlockingCaptureProcess()
	factory := &fakeContinuousFactory{process: process}
	recorder, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 1, QueueDepth: 1}, factory)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _ = recorder.Next(ctx)
	if err := recorder.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if process.stopCount() != 1 {
		t.Fatalf("process Stop calls = %d, want 1", process.stopCount())
	}
	if detector.closeCalls != 1 {
		t.Fatalf("detector Close calls = %d, want 1", detector.closeCalls)
	}
}

func TestContinuousRecorderRejectsInvalidBounds(t *testing.T) {
	cfg := config.AudioConfig{SampleRate: 16000, Channels: 1}
	detector := &fakeDetector{}
	if _, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 0, QueueDepth: 1}, &fakeContinuousFactory{}); err == nil {
		t.Fatal("zero pre-roll error = nil")
	}
	if _, err := newContinuousRecorder(cfg, detector, ContinuousOptions{PreRollChunks: 1, QueueDepth: 0}, &fakeContinuousFactory{}); err == nil {
		t.Fatal("zero queue error = nil")
	}
}

type fakeContinuousFactory struct {
	mu      sync.Mutex
	count   int
	data    []byte
	process captureProcess
}

func (f *fakeContinuousFactory) Start(config.AudioConfig) (captureProcess, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.process != nil {
		return f.process, nil
	}
	return &fakeCaptureProcess{Reader: bytes.NewReader(append([]byte(nil), f.data...))}, nil
}

func (f *fakeContinuousFactory) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

type fakeCaptureProcess struct {
	io.Reader
	stopped bool
}

func (p *fakeCaptureProcess) Stop() error {
	p.stopped = true
	return nil
}

type blockingCaptureProcess struct {
	readStarted chan struct{}
	stopped     chan struct{}
	readOnce    sync.Once
	stopOnce    sync.Once
	mu          sync.Mutex
	stops       int
}

func newBlockingCaptureProcess() *blockingCaptureProcess {
	return &blockingCaptureProcess{readStarted: make(chan struct{}), stopped: make(chan struct{})}
}

func (p *blockingCaptureProcess) Read([]byte) (int, error) {
	p.readOnce.Do(func() { close(p.readStarted) })
	<-p.stopped
	return 0, io.EOF
}

func (p *blockingCaptureProcess) Stop() error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stops++
		p.mu.Unlock()
		close(p.stopped)
	})
	return nil
}

func (p *blockingCaptureProcess) stopCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stops
}

func pcmChunks(count, samplesPerChunk int, value float32) []byte {
	raw := make([]byte, count*samplesPerChunk*pcmBytesPerSample)
	pcm := int16(value * 32767)
	for i := 0; i < count*samplesPerChunk; i++ {
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(pcm))
	}
	return raw
}

func makeSamples(count int, value float32) []float32 {
	samples := make([]float32, count)
	for i := range samples {
		samples[i] = value
	}
	return samples
}
