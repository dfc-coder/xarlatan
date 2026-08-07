package vad

import (
	"testing"
	"time"
)

func TestEnergyVADImplementsDetectorLifecycle(t *testing.T) {
	detector := New(0.1, 0.08, 80*time.Millisecond, 1000)
	called := false
	detector.SetEventHandler(func(Event) { called = true })

	samples := make([]float32, 80)
	for i := range samples {
		samples[i] = 0.5
	}
	detector.ProcessChunk(samples)
	if !called {
		t.Fatal("SetEventHandler did not receive detector event")
	}
	if err := detector.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
