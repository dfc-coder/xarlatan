package zeroclaw

import (
	"bufio"
	"context"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestV07ACPUsesPortablePromptContentBlocks(t *testing.T) {
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	client, err := NewClient(clientReads, clientWrites, Config{CWD: "/tmp/xarlatan-v07"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	server := &acpTestServer{reader: bufio.NewReader(serverReads), writer: serverWrites}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverWrites.Close()
		_ = serverReads.Close()
	})

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
			case "session/new":
				params, _ := message.Raw["params"].(map[string]any)
				if params["cwd"] != "/tmp/xarlatan-v07" {
					if err := server.write(testRPCError(message.ID, "session/new did not carry the configured cwd")); err != nil {
						return err
					}
					return errServerDone
				}
				return server.write(newSessionResult(message.ID, "s-portable"))
			case "session/prompt":
				params, _ := message.Raw["params"].(map[string]any)
				blocks, ok := params["prompt"].([]any)
				if !ok || len(blocks) != 1 {
					if err := server.write(testRPCError(message.ID, "session/prompt must use an ACP content-block array")); err != nil {
						return err
					}
					return errServerDone
				}
				block, _ := blocks[0].(map[string]any)
				if block["type"] != "text" || block["text"] != "hola" {
					if err := server.write(testRPCError(message.ID, "session/prompt text block has the wrong shape")); err != nil {
						return err
					}
					return errServerDone
				}
				if err := server.write(sessionTextUpdate("s-portable", "agent_message_chunk", "Hola.")); err != nil {
					return err
				}
				if err := server.write(promptResult(message.ID, "s-portable", "end_turn", "Hola.")); err != nil {
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
	if _, err := client.RespondStream(ctx, "hola", nil); err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server contract error = %v", err)
	}
}

func TestV07ACPReconstructsNullClawReplyWithoutTerminalContent(t *testing.T) {
	client, server := newACPTestPair(t)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveACP(server, func(message rpcMessage) error {
			switch message.Method {
			case "initialize":
				return server.write(initializeResult(message.ID))
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
					"result": map[string]any{
						"stopReason": "end_turn",
					},
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
	var deltas []string
	result, err := client.RespondStream(ctx, "hola", func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if want := []string{"Hola", " desde", " NullClaw."}; !reflect.DeepEqual(deltas, want) {
		t.Fatalf("deltas = %q, want %q", deltas, want)
	}
	if result.Reply != "Hola desde NullClaw." {
		t.Fatalf("Reply = %q, want reconstructed streamed reply", result.Reply)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func testRPCError(id any, message string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32602,
			"message": message,
		},
	}
}
