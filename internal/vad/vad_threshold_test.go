package vad

import "testing"

func TestNew_SetsIndependentThresholds(t *testing.T) {
	v := New(0.5, 0.35, 800000000, 10)

	if v.threshold != 0.5 {
		t.Fatalf("threshold = %v, want 0.5", v.threshold)
	}
	if v.releaseThreshold != 0.35 {
		t.Fatalf("releaseThreshold = %v, want 0.35", v.releaseThreshold)
	}
	if v.emaAlpha != 0.25 {
		t.Fatalf("emaAlpha = %v, want 0.25", v.emaAlpha)
	}
}
