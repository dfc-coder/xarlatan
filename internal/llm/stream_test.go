package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

func TestGenerateStreamPublishesContentDeltasAndPreservesFinalReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hola"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":", mundo."}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := newStreamingTestClient(t, server)
	var deltas []string
	reply, toolLog, history, err := client.GenerateStream(
		context.Background(),
		[]Message{{Role: "system", Content: "system"}},
		"saluda",
		nil,
		func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if !reflect.DeepEqual(deltas, []string{"Hola", ", mundo."}) {
		t.Fatalf("deltas = %v", deltas)
	}
	if reply != "Hola, mundo." || toolLog != "" {
		t.Fatalf("reply=%q toolLog=%q", reply, toolLog)
	}
	if got := history[len(history)-1]; got.Role != "assistant" || got.Content != reply {
		t.Fatalf("last history = %+v", got)
	}
}

func TestGenerateStreamSuppressesDeltasWhenToolsAreExposed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Voy a revisar. "}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"noop","arguments":"{}"}}]}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	registry := tools.NewRegistry(tools.AllowAllToolPolicy())
	if err := registry.Register(noopTool{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := newStreamingTestClient(t, server)
	called := false
	_, toolLog, _, err := client.GenerateStream(context.Background(), nil, "test", registry, func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if called {
		t.Fatal("content delta published while tools were exposed")
	}
	if toolLog == "" {
		t.Fatal("tool call was not reconstructed")
	}
}

func TestGenerateStreamPropagatesDeltaSinkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"primer fragmento"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"segundo fragmento"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := newStreamingTestClient(t, server)
	sinkErr := errors.New("downstream cancelled")
	calls := 0
	_, _, _, err := client.GenerateStream(context.Background(), nil, "test", nil, func(string) error {
		calls++
		return sinkErr
	})
	if !errors.Is(err, sinkErr) {
		t.Fatalf("GenerateStream() error = %v, want sink error", err)
	}
	if calls != 1 {
		t.Fatalf("sink calls = %d, want 1", calls)
	}
}

func TestParseStreamWrapperRetainsBufferedContract(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"uno \"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"dos\"}}]}\n" +
		"data: [DONE]\n"
	content, calls, err := parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseStream() error = %v", err)
	}
	if content != "uno dos" || len(calls) != 0 {
		t.Fatalf("content=%q calls=%v", content, calls)
	}
}

func newStreamingTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   64,
		HTTPClient:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
