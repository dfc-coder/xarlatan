package main

import (
	"strings"
	"testing"
)

func TestMainClosesOwnedResourcesInReverseAcquisitionOrder(t *testing.T) {
	source := readMainSource(t)

	transcriberAcquire := strings.Index(source, "transcriber, err := stt.New(")
	transcriberDefer := strings.Index(source, "defer func() {\n\t\tif err := transcriber.Close()")
	responseAcquire := strings.Index(source, "responseRuntime, err := newResponseRuntime(")
	responseDefer := strings.Index(source, "defer func() {\n\t\tif err := responseRuntime.Close()")
	synthesizerAcquire := strings.Index(source, "synthesizer, err := tts.New(")
	synthesizerDefer := strings.Index(source, "defer func() {\n\t\tif err := synthesizer.Close()")

	positions := []struct {
		name  string
		value int
	}{
		{"transcriber acquire", transcriberAcquire},
		{"transcriber defer", transcriberDefer},
		{"response runtime acquire", responseAcquire},
		{"response runtime defer", responseDefer},
		{"synthesizer acquire", synthesizerAcquire},
		{"synthesizer defer", synthesizerDefer},
	}
	for _, position := range positions {
		if position.value < 0 {
			t.Fatalf("main.go missing %s", position.name)
		}
	}

	if !(transcriberAcquire < transcriberDefer && transcriberDefer < responseAcquire && responseAcquire < responseDefer && responseDefer < synthesizerAcquire && synthesizerAcquire < synthesizerDefer) {
		t.Fatalf("resource acquisition/defer order is not transcriber -> response runtime -> synthesizer: %+v", positions)
	}
	// Go executes defers in LIFO order, yielding synthesizer -> response runtime -> transcriber.
}
