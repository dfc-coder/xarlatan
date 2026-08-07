package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestRecorderCalculatesMonoChunkSize(t *testing.T) {
	got, err := pcmChunkBytes(16000, 1)
	if err != nil {
		t.Fatalf("pcmChunkBytes() error = %v", err)
	}
	if got != 2560 {
		t.Fatalf("pcmChunkBytes() = %d, want 2560", got)
	}
	if _, err := pcmChunkBytes(16000, 2); err == nil {
		t.Fatal("stereo capture should be rejected by the voice pipeline")
	}
}

func TestRecorderStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelReader{ctx: ctx, started: make(chan struct{})}
	recorder := NewRecorder(config.AudioConfig{
		SampleRate:        16000,
		Channels:          1,
		SilenceThreshold:  0.5,
		SilenceDurationMS: 100,
		MaxDurationS:      30,
		Device:            "default",
	})

	done := make(chan error, 1)
	go func() {
		_, err := recorder.recordFromReader(ctx, reader, time.Now().Add(time.Minute))
		done <- err
	}()
	<-reader.started
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("recordFromReader() error = %v, want context.Canceled", err)
	}
}

func TestRecorderProcessesPartialEOF(t *testing.T) {
	cfg := config.AudioConfig{
		SampleRate:        1000,
		Channels:          1,
		SilenceThreshold:  0.1,
		SilenceDurationMS: 500,
		MaxDurationS:      30,
		Device:            "default",
	}
	recorder := NewRecorder(cfg)

	const sampleCount = 300
	raw := make([]byte, sampleCount*2)
	for i := 0; i < sampleCount; i++ {
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(int16(20000)))
	}
	got, err := recorder.recordFromReader(context.Background(), bytes.NewReader(raw), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("recordFromReader() error = %v", err)
	}
	if len(got) != sampleCount {
		t.Fatalf("samples = %d, want %d", len(got), sampleCount)
	}
}

func TestBufferCloneIsDefensive(t *testing.T) {
	original := Buffer{Samples: []float32{0.1, 0.2}, SampleRate: 16000, Channels: 1}
	cloned := original.Clone()
	cloned.Samples[0] = 0.9
	if reflect.DeepEqual(original, cloned) || original.Samples[0] != 0.1 {
		t.Fatalf("clone aliases original: original=%+v cloned=%+v", original, cloned)
	}
}

func TestPlayerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	factory := newFakePlaybackFactory()
	factory.blockWrites = true
	player := NewPlayback("default", 16000, 1)
	player.factory = factory
	player.guard = playbackGuardFunc(func(context.Context) error { return nil })
	defer player.Close()

	done := make(chan error, 1)
	go func() {
		done <- player.Play(ctx, Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1})
	}()
	process := <-factory.started
	<-process.writeStarted
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Play() error = %v, want context.Canceled", err)
	}
}

type cancelReader struct {
	ctx     context.Context
	started chan struct{}
	once    bool
}

func (r *cancelReader) Read([]byte) (int, error) {
	if !r.once {
		close(r.started)
		r.once = true
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}
