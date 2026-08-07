package main

import (
	"strings"
	"testing"
)

func TestMainComposesResponseScopedPlaybackForStreaming(t *testing.T) {
	source := readMainSource(t)
	for _, required := range []string{
		"audio.NewPlayback(",
		"audio.NewResponsePlayback(playback)",
		"Player:      player",
		"player.Close()",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("main.go missing WI-11A playback composition %q", required)
		}
	}
}
