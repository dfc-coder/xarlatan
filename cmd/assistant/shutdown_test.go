package main

import (
	"strings"
	"testing"
)

func TestMainClosesOwnedResourcesInReverseAcquisitionOrder(t *testing.T) {
	source := readMainSource(t)

	voiceAcquire := strings.Index(source, "voiceInference, err := newVoiceInferenceRuntime(")
	voiceDefer := strings.Index(source, "defer func() {\n\t\tif err := voiceInference.Close()")
	responseAcquire := strings.Index(source, "responseRuntime, err := newResponseRuntime(")
	responseDefer := strings.Index(source, "defer func() {\n\t\tif err := responseRuntime.Close()")
	playbackAcquire := strings.Index(source, "playback := audio.NewPlayback(")
	playbackDefer := strings.Index(source, "defer func() {\n\t\tif err := player.Close()")

	positions := []struct {
		name  string
		value int
	}{
		{"voice inference acquire", voiceAcquire},
		{"voice inference defer", voiceDefer},
		{"response runtime acquire", responseAcquire},
		{"response runtime defer", responseDefer},
		{"playback acquire", playbackAcquire},
		{"playback defer", playbackDefer},
	}
	for _, position := range positions {
		if position.value < 0 {
			t.Fatalf("main.go missing %s", position.name)
		}
	}

	if !(voiceAcquire < voiceDefer && voiceDefer < responseAcquire && responseAcquire < responseDefer && responseDefer < playbackAcquire && playbackAcquire < playbackDefer) {
		t.Fatalf("resource acquisition/defer order is not voice inference -> response runtime -> playback: %+v", positions)
	}
	// Go executes defers in LIFO order: playback -> response runtime -> voice inference.
}
