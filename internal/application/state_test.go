package application

import (
	"context"
	"reflect"
	"testing"
)

func TestApplicationEmitsOrderedStates(t *testing.T) {
	observer := &recordingObserver{}
	app := newTestApplication(t, &callLog{})
	app.observer = observer

	if _, err := app.RunTurn(context.Background()); err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	want := []State{
		StateIdle,
		StateListening,
		StateTranscribing,
		StateThinking,
		StateSynthesizing,
		StateSpeaking,
		StateIdle,
	}
	if got := observer.states(); !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}
