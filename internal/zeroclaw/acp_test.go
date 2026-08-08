package zeroclaw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestACPInitializeAndPromptStreamsOnlyAgentMessages(t *testing.T) {
	client, server := newACPTestPair(t)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-test"))
			case "session/prompt":
				for _, update := range []map[string]any{
					sessionTextUpdate("s-test", "agent_thought_chunk", "private reasoning"),
					toolUpdate("s-test", "tool_call", "must-not-speak"),
					sessionTextUpdate("s-test", "agent_message_chunk", "Hola"),
					toolUpdate("s-test", "tool_call_update", "must-not-speak"),
					sessionTextUpdate("s-test", "agent_message_chunk", " mundo."),
				} {
					if err := server.write(update); err != nil {
						return err
					}
				}
				if err := server.write(promptResult(message.ID, "s-test", "end_turn", "Hola mundo.")); err != nil {
					return err
				}
				return errServerDone
			}
			return nil
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	var deltas []string
	result, err := client.RespondStream(ctx, "saluda", func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if want := []string{"Hola", " mundo."}; !reflect.DeepEqual(deltas, want) {
		t.Fatalf("deltas = %q, want %q", deltas, want)
	}
	if result.Reply != "Hola mundo." || result.StopReason != "end_turn" {
		t.Fatalf("result = %+v", result)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestACPPermissionRequestIsRejectedWithoutApprovalUI(t *testing.T) {
	client, server := newACPTestPair(t)
	permissionResponse := make(chan map[string]any, 1)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-safe"))
			case "session/prompt":
				if err := server.write(permissionRequest("s-safe")); err != nil {
					return err
				}
				response, err := server.read()
				if err != nil {
					return err
				}
				permissionResponse <- response.Raw
				if err := server.write(promptResult(message.ID, "s-safe", "end_turn", "No ejecuté la acción.")); err != nil {
					return err
				}
				return errServerDone
			}
			return nil
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := client.RespondStream(ctx, "acción sensible", nil); err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}

	response := <-permissionResponse
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("permission response result = %#v", response["result"])
	}
	outcome, ok := result["outcome"].(map[string]any)
	if !ok {
		t.Fatalf("permission outcome = %#v", result["outcome"])
	}
	if outcome["outcome"] != "selected" || outcome["optionId"] != "reject-once" {
		t.Fatalf("unsafe permission response = %#v", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestACPCancelSendsSessionCancelAndDropsLateDelta(t *testing.T) {
	client, server := newACPTestPair(t)
	promptReceived := make(chan struct{})
	cancelReceived := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-cancel"))
			case "session/prompt":
				close(promptReceived)
				cancelMessage, err := server.read()
				if err != nil {
					return err
				}
				if cancelMessage.Method != "session/cancel" {
					return errors.New("expected session/cancel")
				}
				close(cancelReceived)
				if err := server.write(sessionTextUpdate("s-cancel", "agent_message_chunk", "late")); err != nil {
					return err
				}
				if err := server.write(promptResult(message.ID, "s-cancel", "cancelled", "partial")); err != nil {
					return err
				}
				return errServerDone
			}
			return nil
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	turnCtx, stopTurn := context.WithCancel(ctx)
	var mu sync.Mutex
	var deltas []string
	done := make(chan error, 1)
	go func() {
		_, err := client.RespondStream(turnCtx, "respuesta larga", func(delta string) error {
			mu.Lock()
			deltas = append(deltas, delta)
			mu.Unlock()
			return nil
		})
		done <- err
	}()

	<-promptReceived
	stopTurn()
	<-cancelReceived
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("RespondStream() error = %v, want context.Canceled", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deltas) != 0 {
		t.Fatalf("late deltas reached voice stream: %q", deltas)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestACPRejectsConcurrentPromptOnSameSession(t *testing.T) {
	client, server := newACPTestPair(t)
	firstPrompt := make(chan struct{})
	releaseFirst := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-busy"))
			case "session/prompt":
				close(firstPrompt)
				<-releaseFirst
				if err := server.write(promptResult(message.ID, "s-busy", "end_turn", "done")); err != nil {
					return err
				}
				return errServerDone
			}
			return nil
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := client.RespondStream(ctx, "first", nil)
		firstDone <- err
	}()
	<-firstPrompt
	if _, err := client.RespondStream(ctx, "second", nil); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second prompt error = %v, want ErrSessionBusy", err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first prompt error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestACPRejectsUnsupportedProtocolVersion(t *testing.T) {
	client, server := newACPTestPair(t)
	go func() {
		_ = serveACP(server, func(message rpcMessage) error {
			if message.Method != "initialize" {
				return nil
			}
			if err := server.write(map[string]any{
				"jsonrpc": "2.0",
				"id":      message.ID,
				"result":  map[string]any{"protocolVersion": 2},
			}); err != nil {
				return err
			}
			return errServerDone
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Initialize(ctx); !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("Initialize() error = %v, want ErrProtocolVersion", err)
	}
}

var errServerDone = errors.New("test server done")

type acpTestServer struct {
	reader *bufio.Reader
	writer *io.PipeWriter
}

type rpcMessage struct {
	ID     any
	Method string
	Raw    map[string]any
}

func newACPTestPair(t *testing.T) (*Client, *acpTestServer) {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	client, err := NewClient(clientReads, clientWrites, Config{AgentAlias: "xarlatan"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverWrites.Close()
		_ = serverReads.Close()
	})
	return client, &acpTestServer{reader: bufio.NewReader(serverReads), writer: serverWrites}
}

func (s *acpTestServer) read() (rpcMessage, error) {
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return rpcMessage{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return rpcMessage{}, err
	}
	method, _ := raw["method"].(string)
	return rpcMessage{ID: raw["id"], Method: method, Raw: raw}, nil
}

func (s *acpTestServer) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.writer.Write(append(data, '\n'))
	return err
}

func serveACP(server *acpTestServer, handler func(rpcMessage) error) error {
	for {
		message, err := server.read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				return nil
			}
			return err
		}
		if err := handler(message); err != nil {
			if errors.Is(err, errServerDone) {
				return nil
			}
			return err
		}
	}
}

func initializeResult(id any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"protocolVersion":   1,
			"agentCapabilities": map[string]any{"loadSession": true},
		},
	}
}

func newSessionResult(id any, sessionID string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"sessionId": sessionID},
	}
}

func promptResult(id any, sessionID, stopReason, content string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"sessionId":  sessionID,
			"stopReason": stopReason,
			"content":    content,
		},
	}
}

func sessionTextUpdate(sessionID, kind, text string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": kind,
				"content": map[string]any{
					"type": "text",
					"text": text,
				},
			},
		},
	}
}

func toolUpdate(sessionID, kind, secret string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": kind,
				"toolCallId":    "tc-1",
				"rawOutput":     secret,
			},
		},
	}
}

func permissionRequest(sessionID string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      "zc-out-0",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": sessionID,
			"options": []any{
				map[string]any{"optionId": "allow-once", "kind": "allow_once"},
				map[string]any{"optionId": "reject-once", "kind": "reject_once"},
			},
			"toolCall": map[string]any{
				"toolCallId": "tc-secret",
				"rawInput":   map[string]any{"token": "secret"},
			},
		},
	}
}
