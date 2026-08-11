package acp

import (
	"fmt"
	"reflect"
	"testing"
)

func TestBudgetedResponderRotatesLogicalSessionAfterMaxTurns(t *testing.T) {
	client, server := newTestPair(t)
	defer client.Close()

	serverErr := make(chan error, 1)
	var sessionCount int
	var promptSessions []string
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "nullclaw"))
			case "session/new":
				sessionCount++
				return server.write(newSessionResult(message.ID, fmt.Sprintf("s-%d", sessionCount)))
			case "session/prompt":
				params, _ := message.Raw["params"].(map[string]any)
				sessionID, _ := params["sessionId"].(string)
				promptSessions = append(promptSessions, sessionID)
				if err := server.write(promptResult(message.ID, "end_turn", "ok")); err != nil {
					return err
				}
				if len(promptSessions) == 3 {
					return errServerDone
				}
			}
			return nil
		})
	}()

	ctx, cancel := testContext(t)
	defer cancel()
	mustInitialize(t, client, ctx)

	runtime := &Runtime{client: client}
	responder, err := runtime.BudgetedResponder(2)
	if err != nil {
		t.Fatalf("BudgetedResponder() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		result, err := responder.Respond(ctx, fmt.Sprintf("turn-%d", i+1))
		if err != nil {
			t.Fatalf("Respond(%d) error = %v", i+1, err)
		}
		if result.Reply != "ok" {
			t.Fatalf("Respond(%d) reply = %q, want ok", i+1, result.Reply)
		}
	}

	if want := []string{"s-1", "s-1", "s-2"}; !reflect.DeepEqual(promptSessions, want) {
		t.Fatalf("prompt sessions = %v, want %v", promptSessions, want)
	}
	if sessionCount != 2 {
		t.Fatalf("session/new count = %d, want 2", sessionCount)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestBudgetedResponderRejectsUninitializedRuntime(t *testing.T) {
	if _, err := (*Runtime)(nil).BudgetedResponder(3); err == nil {
		t.Fatal("BudgetedResponder() on nil runtime error = nil")
	}

	runtime := &Runtime{}
	if _, err := runtime.BudgetedResponder(3); err == nil {
		t.Fatal("BudgetedResponder() on uninitialized runtime error = nil")
	}
}
