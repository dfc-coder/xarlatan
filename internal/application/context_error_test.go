package application

import (
	"context"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestApplicationTreatsDirectStageCancellationAsTerminal(t *testing.T) {
	app := newTestApplication(t, &callLog{})
	app.transcriber = transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
		return "", context.Canceled
	})

	_, err := app.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorCancelled) {
		t.Fatalf("RunTurn() error = %v, want cancelled", err)
	}
}

func TestApplicationTreatsDirectStageDeadlineAsTerminal(t *testing.T) {
	app := newTestApplication(t, &callLog{})
	app.player = playerFunc(func(context.Context, audio.Buffer) error {
		return context.DeadlineExceeded
	})

	_, err := app.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorDeadlineExceeded) {
		t.Fatalf("RunTurn() error = %v, want deadline_exceeded", err)
	}
}
