package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebFetchRejectsPrivateDestinationsBeforeTransport(t *testing.T) {
	originalClient := webClient
	defer func() { webClient = originalClient }()

	var calls atomic.Int32
	webClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("private response")),
			Request:    req,
		}, nil
	})}

	tests := []struct {
		name string
		url  string
	}{
		{name: "loopback IPv4", url: "http://127.0.0.1/admin"},
		{name: "loopback IPv6", url: "http://[::1]/admin"},
		{name: "RFC1918", url: "http://10.0.0.7/internal"},
		{name: "IPv6 ULA", url: "http://[fd00::7]/internal"},
		{name: "link-local metadata", url: "http://169.254.169.254/latest/meta-data"},
		{name: "container metadata", url: "http://169.254.170.2/v2/credentials"},
		{name: "Alibaba metadata", url: "http://100.100.100.200/latest/meta-data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls.Store(0)
			args, err := json.Marshal(map[string]any{"url": tt.url})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result := (WebFetch{}).Execute(context.Background(), args)
			if !result.IsError {
				t.Fatalf("WebFetch.Execute() = %#v, want policy rejection", result)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("transport calls = %d, want 0", got)
			}
		})
	}
}
