package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/dfc-coder/xarlatan/internal/application"
	"github.com/dfc-coder/xarlatan/internal/console"
)

type consoleObserver struct {
	status *console.StatusPrinter
}

func newConsoleObserver(status *console.StatusPrinter) *consoleObserver {
	return &consoleObserver{status: status}
}

func (o *consoleObserver) OnEvent(event application.Event) {
	if o == nil || o.status == nil {
		return
	}
	label := "Escuchando"
	switch event.State {
	case application.StateIdle, application.StateListening:
		label = "Escuchando"
	case application.StateTranscribing:
		label = "Procesando STT"
	case application.StateThinking:
		label = "Pensando"
	case application.StateSynthesizing:
		label = "Sintetizando"
	case application.StateSpeaking:
		label = "Hablando"
	case application.StateStopping:
		label = "Deteniendo"
	}
	o.status.Set(label)
	slog.Debug("voice state", "turn_id", event.TurnID, "state", event.State)
}

type consoleView struct {
	out io.Writer
}

func newConsoleView(out io.Writer) *consoleView {
	return &consoleView{out: out}
}

func (v *consoleView) ShowUser(text string) {
	if v == nil || v.out == nil {
		return
	}
	_, _ = fmt.Fprintf(v.out, "\n👤  %s\n", text)
}

func (v *consoleView) ShowAssistant(text string) {
	if v == nil || v.out == nil {
		return
	}
	_, _ = fmt.Fprintf(v.out, "🤖  %s\n", text)
}
