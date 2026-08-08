package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/application"
)

func TestMainComposesTranscriptBridgeWithoutWrappingBargeSTT(t *testing.T) {
	source := readMainSource(t)
	for _, required := range []string{
		"application.NewTranscriptBridge(",
		"voiceInference.transcriber",
		"newConsoleTranscriptObserver(os.Stdout)",
		"newFanoutObserver(",
		"Transcriber: transcriptBridge",
		"application.NewBargeInController(\n\t\t\trecorder,\n\t\t\tvoiceInference.transcriber,",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("main.go missing WI-11C2 composition %q", required)
		}
	}
}

func TestConsoleTranscriptObserverPrintsOnlyPartial(t *testing.T) {
	var output bytes.Buffer
	observer := newConsoleTranscriptObserver(&output)
	observer.OnTranscript(application.TranscriptEvent{TurnID: 1, Kind: application.TranscriptPartial, Text: "Xarlatan cómo"})
	observer.OnTranscript(application.TranscriptEvent{TurnID: 1, Kind: application.TranscriptFinal, Text: "Xarlatan cómo estás"})
	if got := output.String(); got != "[partial] Xarlatan cómo\n" {
		t.Fatalf("console transcript output = %q", got)
	}
}

func TestFanoutObserverPreservesAllMetadataEvents(t *testing.T) {
	left := &fakeApplicationObserver{}
	right := &fakeApplicationObserver{}
	observer := newFanoutObserver(left, nil, right)
	observer.OnEvent(application.Event{TurnID: 9, State: application.StateListening})
	if len(left.states) != 1 || len(right.states) != 1 {
		t.Fatalf("fanout left=%v right=%v", left.states, right.states)
	}
}
