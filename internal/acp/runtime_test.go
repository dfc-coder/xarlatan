package acp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLaunchesConfiguredCommandAndReapsChild(t *testing.T) {
	cwd := t.TempDir()
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Binary:          writeACPHelper(t),
		Args:            []string{"serve", "acp"},
		CWD:             cwd,
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if got := runtime.AgentInfo(); got.Name != "fake-acp" {
		t.Fatalf("AgentInfo() = %+v", got)
	}
	responder, err := runtime.Responder()
	if err != nil {
		t.Fatalf("Responder() error = %v", err)
	}
	result, err := responder.Respond(context.Background(), "hola")
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Reply != "Hola desde ACP." {
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
		Args:            []string{"serve", "acp"},
		CWD:             t.TempDir(),
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

func TestRuntimeRejectsBadHandshake(t *testing.T) {
	t.Setenv("XARLATAN_TEST_ACP_VERSION", "2")
	_, err := StartRuntime(context.Background(), RuntimeConfig{
		Binary:          writeACPHelper(t),
		Args:            []string{"serve", "acp"},
		CWD:             t.TempDir(),
		ShutdownTimeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("StartRuntime() error = %v, want protocol error", err)
	}
}

func TestRuntimeConfigValidation(t *testing.T) {
	binary := writeACPHelper(t)
	file := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []RuntimeConfig{
		{},
		{Binary: binary, CWD: "relative"},
		{Binary: binary, CWD: t.TempDir(), Args: []string{""}},
		{Binary: binary, CWD: file},
		{Binary: binary, CWD: "/definitely/missing/xarlatan-v07"},
	}
	for i := range cases {
		cfg := cases[i]
		if err := validateRuntimeConfig(&cfg); err == nil {
			t.Fatalf("case %d validateRuntimeConfig() error = nil", i)
		}
	}
}

func writeACPHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-acp")
	script := `#!/usr/bin/env python3
import json
import os
import sys

if sys.argv[1:] != ["serve", "acp"]:
    sys.exit(31)
version = int(os.environ.get("XARLATAN_TEST_ACP_VERSION", "1"))
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    request_id = message.get("id")
    if method == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"protocolVersion":version,"agentInfo":{"name":"fake-acp","title":"Fake ACP","version":"1"}}}), flush=True)
    elif method == "session/new":
        cwd = message.get("params", {}).get("cwd")
        if not cwd or not os.path.isabs(cwd):
            sys.exit(32)
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"sessionId":"s-runtime"}}), flush=True)
    elif method == "session/prompt":
        raw_prompt = message.get("params", {}).get("prompt", [])
        prompt = "\n\n".join(part.get("text", "") for part in raw_prompt if isinstance(part, dict))
        if prompt == "crash":
            os._exit(7)
        print(json.dumps({"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-runtime","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hola desde ACP."}}}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":request_id,"result":{"stopReason":"end_turn"}}), flush=True)
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	return path
}
