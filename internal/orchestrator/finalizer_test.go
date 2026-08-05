package orchestrator

import (
	"context"
	"testing"
)

func TestFinalizer_RunCompletesFlow(t *testing.T) {
	node := Finalizer{}

	t.Run("copies draft into final response", func(t *testing.T) {
		state, next, err := node.Run(context.Background(), State{DraftResponse: "respuesta"})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := state.FinalResponse; got != "respuesta" {
			t.Fatalf("FinalResponse = %q, want %q", got, "respuesta")
		}
		if !state.Done {
			t.Fatalf("Done = false, want true")
		}
		if next != "" {
			t.Fatalf("next = %q, want empty", next)
		}
	})

	t.Run("uses fallback when draft is empty", func(t *testing.T) {
		state, _, err := node.Run(context.Background(), State{})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := state.FinalResponse; got != "Sin respuesta disponible." {
			t.Fatalf("FinalResponse = %q, want %q", got, "Sin respuesta disponible.")
		}
		if !state.Done {
			t.Fatalf("Done = false, want true")
		}
	})
}
