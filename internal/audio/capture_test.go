package audio

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
)

func TestRecorderProcessChunk_PrependsPreRollOnSpeechStart(t *testing.T) {
	r := NewRecorder(configForTest())

	var recording []float32
	recording, shouldFinish := r.processChunk(recording, []float32{0.1, 0.1})
	if shouldFinish {
		t.Fatal("silent chunk should not finish")
	}
	recording, shouldFinish = r.processChunk(recording, []float32{0.2, 0.2})
	if shouldFinish {
		t.Fatal("silent chunk should not finish")
	}
	recording, shouldFinish = r.processChunk(recording, []float32{0.9, 0.9})
	if shouldFinish {
		t.Fatal("speech chunk should not finish")
	}

	want := []float32{0.1, 0.1, 0.2, 0.2, 0.9, 0.9}
	assertFloat32Slice(t, recording, want)
}

func TestRecorderProcessChunk_IncludesTrailingSilenceBeforeFinish(t *testing.T) {
	r := NewRecorder(configForTest())

	var recording []float32
	recording, shouldFinish := r.processChunk(recording, []float32{0.9, 0.9})
	if shouldFinish {
		t.Fatal("voice chunk should not finish")
	}
	recording, shouldFinish = r.processChunk(recording, []float32{0.1, 0.1})
	if shouldFinish {
		t.Fatal("first quiet chunk should not finish")
	}
	recording, shouldFinish = r.processChunk(recording, []float32{0.1, 0.1})
	if !shouldFinish {
		t.Fatal("second quiet chunk should finish")
	}

	want := []float32{0.9, 0.9, 0.1, 0.1, 0.1, 0.1}
	assertFloat32Slice(t, recording, want)
}

func TestRecorderRecordFromReader_TimeoutWithoutSpeechReturnsNil(t *testing.T) {
	r := NewRecorder(configForTest())

	got, err := r.recordFromReader(context.Background(), bytes.NewReader(nil), time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("recordFromReader error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("recordFromReader result = %v, want nil", got)
	}
}

func configForTest() config.AudioConfig {
	return config.AudioConfig{
		SampleRate:        10,
		Channels:          1,
		SilenceThreshold:  0.5,
		SilenceDurationMS: 100,
		Device:            "default",
	}
}

func assertFloat32Slice(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
