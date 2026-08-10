package acp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestCompatibilityHelpersAndPermissionFallback(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int
		ok    bool
	}{
		{float64(7), 7, true},
		{float64(7.5), 7, false},
		{7, 7, true},
		{uint64(7), 7, true},
		{json.Number("7"), 7, true},
		{json.Number("x"), 0, false},
		{"7", 0, false},
	} {
		got, ok := numberAsInt(tc.value)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("numberAsInt(%#v) = %d,%v want %d,%v", tc.value, got, ok, tc.want, tc.ok)
		}
	}
	if _, err := responseResult(map[string]any{"error": map[string]any{"message": "bad"}}); err == nil {
		t.Fatal("responseResult(error) error = nil")
	}
	if _, err := responseResult(map[string]any{}); err == nil {
		t.Fatal("responseResult(missing result) error = nil")
	}

	client, server := newTestPair(t)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "agent"))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-fallback"))
			case "session/prompt":
				if err := server.write(map[string]any{
					"jsonrpc": "2.0", "id": "permission-no-reject", "method": "session/request_permission",
					"params": map[string]any{
						"sessionId": "s-fallback",
						"options":   []any{map[string]any{"optionId": "allow", "kind": "allow_once"}},
					},
				}); err != nil {
					return err
				}
				response, err := server.read()
				if err != nil {
					return err
				}
				result, _ := response.Raw["result"].(map[string]any)
				outcome, _ := result["outcome"].(map[string]any)
				if outcome["outcome"] != "cancelled" {
					return errors.New("permission fallback was not cancelled")
				}
				if err := server.write(promptResult(message.ID, "end_turn", "cancelled")); err != nil {
					return err
				}
				return errServerDone
			}
			return nil
		})
	}()
	if err := client.Initialize(nil); err != nil {
		t.Fatalf("Initialize(nil) error = %v", err)
	}
	if _, err := client.RespondStream(nil, "ask", nil); err != nil {
		t.Fatalf("RespondStream(nil) error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestNilRuntimeAccessorsAndExitClassification(t *testing.T) {
	var runtime *Runtime
	if _, err := runtime.Responder(); err == nil {
		t.Fatal("nil Runtime.Responder() error = nil")
	}
	if got := runtime.AgentInfo(); got != (AgentInfo{}) {
		t.Fatalf("nil Runtime.AgentInfo() = %+v", got)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("nil Runtime.Close() = %v", err)
	}
	if err, timedOut := runtime.waitForExit(time.Millisecond); err != nil || timedOut {
		t.Fatalf("nil waitForExit() = %v,%v", err, timedOut)
	}
	if !isExpectedExitAfterKill(nil, false) {
		t.Fatal("nil exit must be expected")
	}
	if isExpectedExitAfterKill(errors.New("boom"), false) {
		t.Fatal("unexpected non-killed error classified expected")
	}
	if _, err := StartRuntime(context.Background(), RuntimeConfig{Binary: "", CWD: "/tmp"}); err == nil {
		t.Fatal("StartRuntime(empty binary) error = nil")
	}
}
