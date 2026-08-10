package acp

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

func TestInitializeUsesPortableSessionAndCapturesAgentInfo(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(map[string]any{
					"jsonrpc": "2.0",
					"id":      message.ID,
					"result": map[string]any{
						"protocolVersion": 1,
						"agentInfo": map[string]any{
							"name":    "nullclaw",
							"title":   "NullClaw ACP",
							"version": "0.9.0",
						},
					},
				})
			case "session/new":
				params, _ := message.Raw["params"].(map[string]any)
				if params["cwd"] != "/tmp/xarlatan-acp" {
					return errors.New("session/new missing portable cwd")
				}
				if _, exists := params["agentAlias"]; exists {
					return errors.New("session/new leaked provider-specific agentAlias")
				}
				if err := server.write(newSessionResult(message.ID, "s-info")); err != nil {
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
	if got, want := client.AgentInfo(), (AgentInfo{Name: "nullclaw", Title: "NullClaw ACP", Version: "0.9.0"}); got != want {
		t.Fatalf("AgentInfo() = %+v, want %+v", got, want)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestPromptUsesContentBlocksAndFiltersNonVoiceUpdates(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "zeroclaw-acp"))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-stream"))
			case "session/prompt":
				params, _ := message.Raw["params"].(map[string]any)
				blocks, ok := params["prompt"].([]any)
				if !ok || len(blocks) != 1 {
					return errors.New("prompt is not a one-element content array")
				}
				block, _ := blocks[0].(map[string]any)
				if block["type"] != "text" || block["text"] != "hola" {
					return errors.New("wrong ACP text block")
				}
				for _, update := range []map[string]any{
					sessionTextUpdate("s-stream", "agent_thought_chunk", "private"),
					planUpdate("s-stream", "must-not-speak"),
					toolUpdate("s-stream", "tool_call", "raw input"),
					sessionTextUpdate("s-stream", "agent_message_chunk", "Hola"),
					toolUpdate("s-stream", "tool_call_update", "raw output"),
					sessionTextUpdate("other-session", "agent_message_chunk", "wrong session"),
					sessionTextUpdate("s-stream", "agent_message_chunk", " mundo."),
				} {
					if err := server.write(update); err != nil {
						return err
					}
				}
				if err := server.write(promptResult(message.ID, "end_turn", "Hola mundo.")); err != nil {
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
	result, err := client.RespondStream(ctx, "hola", func(delta string) error {
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

func TestNullClawStyleTerminalWithoutContentUsesStreamedReply(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "nullclaw"))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-null"))
			case "session/prompt":
				for _, delta := range []string{"Hola", " desde", " NullClaw."} {
					if err := server.write(sessionTextUpdate("s-null", "agent_message_chunk", delta)); err != nil {
						return err
					}
				}
				if err := server.write(map[string]any{
					"jsonrpc": "2.0",
					"id":      message.ID,
					"result":  map[string]any{"stopReason": "end_turn"},
				}); err != nil {
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
	result, err := client.RespondStream(ctx, "hola", nil)
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if result.Reply != "Hola desde NullClaw." {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestPermissionRequestIsRejected(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	permissionResponse := make(chan map[string]any, 1)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "agent"))
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
				if err := server.write(promptResult(message.ID, "end_turn", "No ejecuté la acción.")); err != nil {
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
	result, _ := response["result"].(map[string]any)
	outcome, _ := result["outcome"].(map[string]any)
	if outcome["outcome"] != "selected" || outcome["optionId"] != "reject-once" {
		t.Fatalf("unsafe permission response = %#v", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestPermissionWithoutRejectOptionIsCancelled(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "agent"))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-cancel-permission"))
			case "session/prompt":
				if err := server.write(map[string]any{
					"jsonrpc": "2.0",
					"id":      "agent-request",
					"method":  "session/request_permission",
					"params": map[string]any{
						"sessionId": "s-cancel-permission",
						"options": []any{
							map[string]any{"optionId": "allow", "kind": "allow_once"},
						},
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
					return errors.New("permission without reject option was not cancelled")
				}
				if err := server.write(promptResult(message.ID, "end_turn", "cancelled")); err != nil {
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
	if _, err := client.RespondStream(ctx, "ask", nil); err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestCancelSendsSessionCancelAndDropsLateDelta(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	promptReceived := make(chan struct{})
	cancelReceived := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "agent"))
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
				if err := server.write(promptResult(message.ID, "cancelled", "partial")); err != nil {
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

func TestRejectsConcurrentPrompt(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	firstPrompt := make(chan struct{})
	release := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveTestACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID, "agent"))
			case "session/new":
				return server.write(newSessionResult(message.ID, "s-busy"))
			case "session/prompt":
				close(firstPrompt)
				<-release
				if err := server.write(promptResult(message.ID, "end_turn", "done")); err != nil {
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
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first prompt error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestRejectsUnsupportedProtocolVersion(t *testing.T) {
	client, server := newTestPair(t, "/tmp/xarlatan-acp")
	go func() {
		_ = serveTestACP(server, func(message rpcMessage) error {
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

func TestNewClientValidatesStreamsAndCWD(t *testing.T) {
	if _, err := NewClient(nil, io.Discard, ClientConfig{CWD: "/tmp"}); err == nil {
		t.Fatal("NewClient(nil reader) error = nil")
	}
	if _, err := NewClient(strings.NewReader(""), nil, ClientConfig{CWD: "/tmp"}); err == nil {
		t.Fatal("NewClient(nil writer) error = nil")
	}
	if _, err := NewClient(strings.NewReader(""), io.Discard, ClientConfig{}); err == nil {
		t.Fatal("NewClient(empty cwd) error = nil")
	}
	if _, err := NewClient(strings.NewReader(""), io.Discard, ClientConfig{CWD: "relative"}); err == nil {
		t.Fatal("NewClient(relative cwd) error = nil")
	}
}

var errServerDone = errors.New("test server done")

type testServer struct {
	reader *bufio.Reader
	writer *io.PipeWriter
}

type rpcMessage struct {
	ID     any
	Method string
	Raw    map[string]any
}

func newTestPair(t *testing.T, cwd string) (*Client, *testServer) {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	client, err := NewClient(clientReads, clientWrites, ClientConfig{CWD: cwd})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverWrites.Close()
		_ = serverReads.Close()
	})
	return client, &testServer{reader: bufio.NewReader(serverReads), writer: serverWrites}
}

func (s *testServer) read() (rpcMessage, error) {
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

func (s *testServer) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.writer.Write(append(data, '\n'))
	return err
}

func serveTestACP(server *testServer, handler func(rpcMessage) error) error {
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

func initializeResult(id any, name string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"protocolVersion": 1,
			"agentInfo": map[string]any{
				"name": name,
			},
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

func promptResult(id any, stopReason, content string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
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

func planUpdate(sessionID, text string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "plan",
				"entries": []any{
					map[string]any{"content": text},
				},
			},
		},
	}
}

func toolUpdate(sessionID, kind, rawText string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": kind,
				"rawOutput":     rawText,
				"content": map[string]any{
					"type": "text",
					"text": rawText,
				},
			},
		},
	}
}

func permissionRequest(sessionID string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      "agent-request-1",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": sessionID,
			"options": []any{
				map[string]any{"optionId": "allow-once", "kind": "allow_once"},
				map[string]any{"optionId": "reject-once", "kind": "reject_once"},
			},
		},
	}
}
