package main

import (
	"fmt"
	"io"
	"sync"

	"github.com/dfc-coder/xarlatan/internal/application"
)

type fanoutObserver struct {
	observers []application.Observer
}

func newFanoutObserver(observers ...application.Observer) application.Observer {
	filtered := make([]application.Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}
	return &fanoutObserver{observers: filtered}
}

func (o *fanoutObserver) OnEvent(event application.Event) {
	if o == nil {
		return
	}
	for _, observer := range o.observers {
		observer.OnEvent(event)
	}
}

type consoleTranscriptObserver struct {
	writer io.Writer
	mu     sync.Mutex
}

func newConsoleTranscriptObserver(writer io.Writer) application.TranscriptObserver {
	if writer == nil {
		writer = io.Discard
	}
	return &consoleTranscriptObserver{writer: writer}
}

func (o *consoleTranscriptObserver) OnTranscript(event application.TranscriptEvent) {
	if o == nil || event.Kind != application.TranscriptPartial || event.Text == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = fmt.Fprintf(o.writer, "[partial] %s\n", event.Text)
}
