package main

import (
	"strings"
	"testing"
)

func TestMainClosesOwnedResourcesInReverseAcquisitionOrder(t *testing.T) {
	source := readMainSource(t)

	transcriberAcquire := strings.Index(source, "transcriber, err := stt.New(")
	transcriberDefer := strings.Index(source, "defer func() {\n\t\tif err := transcriber.Close()")
	serverAcquire := strings.Index(source, "serverManager, err := llm.NewServerManager(")
	serverDefer := strings.Index(source, "defer func() {\n\t\tif err := serverManager.Stop(context.Background())")
	synthesizerAcquire := strings.Index(source, "synthesizer, err := tts.New(")
	synthesizerDefer := strings.Index(source, "defer func() {\n\t\tif err := synthesizer.Close()")

	positions := []struct {
		name  string
		value int
	}{
		{"transcriber acquire", transcriberAcquire},
		{"transcriber defer", transcriberDefer},
		{"server acquire", serverAcquire},
		{"server defer", serverDefer},
		{"synthesizer acquire", synthesizerAcquire},
		{"synthesizer defer", synthesizerDefer},
	}
	for _, position := range positions {
		if position.value < 0 {
			t.Fatalf("main.go missing %s", position.name)
		}
	}

	if !(transcriberAcquire < transcriberDefer && transcriberDefer < serverAcquire && serverAcquire < serverDefer && serverDefer < synthesizerAcquire && synthesizerAcquire < synthesizerDefer) {
		t.Fatalf("resource acquisition/defer order is not transcriber -> server -> synthesizer: %+v", positions)
	}
	// Go executes defers in LIFO order, yielding synthesizer -> server -> transcriber.
}
