package main

import "github.com/dfc-coder/xarlatan/internal/application"

type captureSuppressor interface {
	SetSuppressed(bool)
}

type captureGateObserver struct {
	downstream application.Observer
	capture    captureSuppressor
}

func newCaptureGateObserver(downstream application.Observer, capture captureSuppressor) application.Observer {
	return &captureGateObserver{downstream: downstream, capture: capture}
}

func (o *captureGateObserver) OnEvent(event application.Event) {
	if o != nil && o.capture != nil {
		switch event.State {
		case application.StateSpeaking:
			o.capture.SetSuppressed(true)
		case application.StateIdle, application.StateInterrupted, application.StateStopping:
			o.capture.SetSuppressed(false)
		}
	}
	if o != nil && o.downstream != nil {
		o.downstream.OnEvent(event)
	}
}
