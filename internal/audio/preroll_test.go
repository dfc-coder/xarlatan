package audio

import "testing"

func TestPreRollBuffer_StartRecordingIncludesRecentAudio(t *testing.T) {
	b := newPreRollBuffer(2)

	pre1 := []float32{1, 1}
	pre2 := []float32{2, 2}
	voice := []float32{3, 3}

	b.add(pre1)
	b.add(pre2)

	got := b.startRecording(voice)
	want := []float32{1, 1, 2, 2, 3, 3}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestPreRollBuffer_EnforcesMaxChunks(t *testing.T) {
	b := newPreRollBuffer(2)

	b.add([]float32{1})
	b.add([]float32{2})
	b.add([]float32{3})

	got := b.startRecording([]float32{4})
	want := []float32{2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestPreRollBuffer_ResetClearsChunks(t *testing.T) {
	b := newPreRollBuffer(2)
	b.add([]float32{1})
	b.add([]float32{2})
	b.reset()

	got := b.startRecording([]float32{3})
	want := []float32{3}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
