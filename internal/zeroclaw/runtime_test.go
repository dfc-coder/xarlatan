package zeroclaw

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeStartsACPAndReapsChild(t *testing.T) {
	binary := writeACPHelper(t)
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Binary:          binary,
		AgentAlias:      "xarlatan",
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}

	responder, err := runtime.Responder()
	if err != nil {
		t.Fatalf("Responder() error = %v", err)
	}
	result, err := responder.Respond(context.Background(), "hola")
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Reply != "Hola desde ZeroClaw." {
		t.Fatalf("Reply = %q", result.Reply)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if runtime.cmd == nil || runtime.cmd.ProcessState == nil || !runtime.cmd.ProcessState.Exited() {
		t.Fatalf("child was not reaped: %#v", runtime.cmd)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestRuntimeSurfacesChildCrash(t *testing.T) {
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Binary:          writeACPHelper(t),
		AgentAlias:      "xarlatan",
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	defer runtime.Close()

	responder, err := runtime.Responder()
	if err != nil {
		t.Fatalf("Responder() error = %v", err)
	}
	if _, err := responder.Respond(context.Background(), "crash"); err == nil {
		t.Fatal("Respond(crash) error = nil")
	}
}

func TestRuntimeRejectsBadACPHandshakeAndReapsChild(t *testing.T) {
	t.Setenv("XARLATAN_TEST_ACP_VERSION", "2")
	_, err := StartRuntime(context.Background(), RuntimeConfig{
		Binary:          writeACPHelper(t),
		AgentAlias:      "xarlatan",
		ShutdownTimeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("StartRuntime() error = %v, want protocol error", err)
	}
}

func writeACPHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-zeroclaw")
	script := `#!/usr/bin/env python3
import json
import os
import sys

if len(sys.argv) < 2 or sys.argv[1] != "acp":
    sys.exit(31)
version = int(os.environ.get("XARLATAN_TEST_ACP_VERSION", "1"))
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    request_id = message.get("id")
    if method == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"protocolVersion":version}}), flush=True)
    elif method == "session/new":
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"sessionId":"s-runtime"}}), flush=True)
    elif method == "session/prompt":
        prompt = message.get("params", {}).get("prompt", "")
        if prompt == "crash":
            os._exit(7)
        print(json.dumps({"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-runtime","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hola desde ZeroClaw."}}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"sessionId":"s-runtime","stopReason":"end_turn","content":"Hola desde ZeroClaw."}}), flush=True)
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	return path
}
