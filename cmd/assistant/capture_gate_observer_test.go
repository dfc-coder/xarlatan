package main

import (
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/application"
)

func TestCaptureGateObserverSuppressesOnlyDuringSpeaking(t *testing.T) {
	capture := &fakeCaptureSuppressor{}
	downstream := &fakeApplicationObserver{}
	observer := newCaptureGateObserver(downstream, capture)

	for _, state := range []application.State{
		application.StateIdle,
		application.StateThinking,
		application.StateSpeaking,
		application.StateInterrupted,
	} {
		observer.OnEvent(application.Event{TurnID: 7, State: state})
	}

	if !reflect.DeepEqual(capture.values, []bool{false, true, false}) {
		t.Fatalf("suppression = %v, want [false true false]", capture.values)
	}
	if !reflect.DeepEqual(downstream.states, []application.State{
		application.StateIdle,
		application.StateThinking,
		application.StateSpeaking,
		application.StateInterrupted,
	}) {
		t.Fatalf("downstream states = %v", downstream.states)
	}
}

type fakeCaptureSuppressor struct {
	values []bool
}

func (f *fakeCaptureSuppressor) SetSuppressed(value bool) {
	f.values = append(f.values, value)
}

type fakeApplicationObserver struct {
	states []application.State
}

func (f *fakeApplicationObserver) OnEvent(event application.Event) {
	f.states = append(f.states, event.State)
}
