package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

type noopTool struct{}

func (noopTool) Name() string        { return "noop" }
func (noopTool) Description() string { return "no-op" }
func (noopTool) Schema() tools.ParameterSchema {
	return tools.NewSchema(nil, map[string]tools.Property{})
}
func (noopTool) Execute(_ context.Context, _ json.RawMessage) tools.Result {
	return tools.Result{Content: "ok"}
}

func TestGenerate_UsesSingleLLMCallPerTurn(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			return
		case "/v1/chat/completions":
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		count := atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if count == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}}]}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"final"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	port := mustPort(t, parsed)

	r := tools.NewRegistry(tools.AllowAllToolPolicy())
	r.Register(noopTool{})

	c := &Client{
		cfg: config.LLMConfig{
			Host:         parsed.Hostname(),
			Port:         port,
			Temperature:  0.7,
			TopP:         0.9,
			MaxTokens:    64,
			SystemPrompt: "system",
		},
		http: server.Client(),
	}

	history := []Message{{Role: "system", Content: "system"}}
	reply, toolLog, nextHistory, err := c.Generate(context.Background(), history, "hello", r)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if !strings.Contains(toolLog, "noop") {
		t.Fatalf("toolLog = %q, want noop", toolLog)
	}
	if reply != "" {
		t.Fatalf("reply = %q, want empty string on tool call turn", reply)
	}
	if got, want := len(history), 1; got != want {
		t.Fatalf("history len = %d, want %d", got, want)
	}
	if got, want := len(nextHistory), 3; got != want {
		t.Fatalf("nextHistory len = %d, want %d", got, want)
	}
	if nextHistory[1].Role != "user" || nextHistory[2].Role != "assistant" {
		t.Fatalf("nextHistory roles = %q %q, want user assistant", nextHistory[1].Role, nextHistory[2].Role)
	}
}

func mustPort(t *testing.T, u *url.URL) int {
	t.Helper()
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var p int
	if _, err := fmt.Sscanf(port, "%d", &p); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return p
}
