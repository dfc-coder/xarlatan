package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type staticIPResolver map[string][]net.IP

func (r staticIPResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := r[host]
	if !ok {
		return nil, fmt.Errorf("no DNS fixture for %s", host)
	}
	addresses := make([]net.IPAddr, len(ips))
	for i, ip := range ips {
		addresses[i] = net.IPAddr{IP: ip}
	}
	return addresses, nil
}

func TestWebFetchRejectsPrivateDestinationsBeforeTransport(t *testing.T) {
	var calls atomic.Int32
	client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), staticIPResolver{}, func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("transport must not run")
	})

	tests := []struct {
		name string
		url  string
	}{
		{name: "loopback IPv4", url: "http://127.0.0.1/admin"},
		{name: "loopback IPv6", url: "http://[::1]/admin"},
		{name: "RFC1918 ten", url: "http://10.0.0.7/internal"},
		{name: "RFC1918 one-seven-two", url: "http://172.16.0.7/internal"},
		{name: "RFC1918 one-nine-two", url: "http://192.168.1.7/internal"},
		{name: "IPv6 ULA", url: "http://[fd00::7]/internal"},
		{name: "link-local metadata", url: "http://169.254.169.254/latest/meta-data"},
		{name: "container metadata", url: "http://169.254.170.2/v2/credentials"},
		{name: "Alibaba metadata", url: "http://100.100.100.200/latest/meta-data"},
		{name: "CGNAT", url: "http://100.64.0.1/internal"},
		{name: "unspecified", url: "http://0.0.0.0/internal"},
		{name: "multicast", url: "http://224.0.0.1/internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls.Store(0)
			args, err := json.Marshal(map[string]any{"url": tt.url})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result := (WebFetch{Client: client}).Execute(context.Background(), args)
			if !result.IsError || !strings.Contains(result.Content, string(HTTPPolicyViolation)) {
				t.Fatalf("WebFetch.Execute() = %#v, want policy rejection", result)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("transport calls = %d, want 0", got)
			}
		})
	}
}

func TestSafeHTTPRejectsInvalidURLsAsPolicy(t *testing.T) {
	var calls atomic.Int32
	client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), staticIPResolver{}, func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("transport must not run")
	})

	for _, rawURL := range []string{
		"",
		"/relative/path",
		"ftp://example.com/file",
		"http://user:secret@example.com/private",
		"http://localhost/admin",
		"http://service.localhost/admin",
		"http://example.com:0/",
		"http://example.com:70000/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			calls.Store(0)
			_, err := client.Get(context.Background(), rawURL, nil, 0)
			if !IsHTTPErrorCode(err, HTTPPolicyViolation) {
				t.Fatalf("Get(%q) error = %v, want policy error", rawURL, err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("transport calls = %d, want 0", got)
			}
		})
	}
}

func TestSafeHTTPRejectsBlockedOrMixedDNSAnswers(t *testing.T) {
	var calls atomic.Int32
	resolver := staticIPResolver{
		"private.test": {net.ParseIP("10.0.0.8")},
		"mixed.test":   {net.ParseIP("203.0.113.8"), net.ParseIP("192.168.1.8")},
		"local.test":   {net.ParseIP("169.254.20.8")},
	}
	client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), resolver, func(
		context.Context,
		string,
		string,
	) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("transport must not run")
	})

	for _, hostname := range []string{"private.test", "mixed.test", "local.test"} {
		t.Run(hostname, func(t *testing.T) {
			calls.Store(0)
			_, err := client.Get(context.Background(), "http://"+hostname+"/resource", nil, 0)
			if !IsHTTPErrorCode(err, HTTPPolicyViolation) {
				t.Fatalf("Get() error = %v, want policy error", err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("dial calls = %d, want 0", got)
			}
		})
	}
}

func TestSafeHTTPPinsDialToValidatedIP(t *testing.T) {
	var dialed string
	client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.9")},
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("stop after dial capture")
	})

	_, err := client.Get(context.Background(), "http://public.test:8080/data", nil, 0)
	if !IsHTTPErrorCode(err, HTTPNetworkFailure) {
		t.Fatalf("Get() error = %v, want network error", err)
	}
	if dialed != "203.0.113.9:8080" {
		t.Fatalf("dialed address = %q, want validated IP", dialed)
	}
}

func TestSafeHTTPAllowsBoundedPublicTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("public response"))
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.10")},
	})
	response, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/data"), nil, 64)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(response.Body) != "public response" || response.Truncated {
		t.Fatalf("response = %#v", response)
	}
	if response.ContentType != "text/plain" {
		t.Fatalf("ContentType = %q, want text/plain", response.ContentType)
	}
}

func TestSafeHTTPRejectsRedirectToPrivateDestination(t *testing.T) {
	var privateRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/start":
			http.Redirect(w, req, redirectURL(t, req.Host, "private.test", "/private"), http.StatusFound)
		case "/private":
			privateRequests.Add(1)
			_, _ = w.Write([]byte("secret"))
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test":  {net.ParseIP("203.0.113.11")},
		"private.test": {net.ParseIP("10.0.0.11")},
	})
	_, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/start"), nil, 64)
	if !IsHTTPErrorCode(err, HTTPPolicyViolation) {
		t.Fatalf("Get() error = %v, want policy error", err)
	}
	if got := privateRequests.Load(); got != 0 {
		t.Fatalf("private endpoint requests = %d, want 0", got)
	}
}

func TestSafeHTTPRejectsRedirectLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/loop", http.StatusFound)
	}))
	defer server.Close()

	config := DefaultSafeHTTPConfig()
	config.MaxRedirects = 2
	client := routedTestClient(t, server.URL, config, staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.12")},
	})
	_, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/loop"), nil, 64)
	if !IsHTTPErrorCode(err, HTTPRedirectLimit) {
		t.Fatalf("Get() error = %v, want redirect-limit error", err)
	}
}

func TestSafeHTTPStripsSensitiveHeadersOnCrossHostRedirect(t *testing.T) {
	var leaked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/start":
			http.Redirect(w, req, redirectURL(t, req.Host, "other.test", "/target"), http.StatusFound)
		case "/target":
			for _, header := range []string{
				"Authorization",
				"Proxy-Authorization",
				"Cookie",
				"X-Subscription-Token",
				"X-Api-Key",
			} {
				if value := req.Header.Get(header); value != "" {
					leaked = append(leaked, header+"="+value)
				}
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.13")},
		"other.test":  {net.ParseIP("203.0.113.14")},
	})
	_, err := client.Get(
		context.Background(),
		routedURL(t, server.URL, "public.test", "/start"),
		map[string]string{
			"Authorization":        "Bearer secret",
			"Proxy-Authorization":  "Basic secret",
			"Cookie":               "session=secret",
			"X-Subscription-Token": "provider-secret",
			"X-Api-Key":            "api-secret",
		},
		64,
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("sensitive headers leaked: %v", leaked)
	}
}

func TestSafeHTTPTruncatesBodyAtRequestedLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1234567890"))
	}))
	defer server.Close()

	config := DefaultSafeHTTPConfig()
	config.MaxBodyBytes = 8
	client := routedTestClient(t, server.URL, config, staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.15")},
	})
	response, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/body"), nil, 4)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(response.Body) != "1234" || !response.Truncated {
		t.Fatalf("response = %#v, want four bytes and truncated=true", response)
	}
}

func TestWebFetchMarksTruncatedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1234567890"))
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.16")},
	})
	args, err := json.Marshal(map[string]any{
		"url":       routedURL(t, server.URL, "public.test", "/body"),
		"max_bytes": 4,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result := (WebFetch{Client: client}).Execute(context.Background(), args)
	if result.IsError || !strings.Contains(result.Content, "1234\n[truncated]") {
		t.Fatalf("WebFetch.Execute() = %#v", result)
	}
}

func TestWebSearchRejectsTruncatedStructuredResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"long result","url":"https://example.com","content":"body"}]}`))
	}))
	defer server.Close()

	config := DefaultSafeHTTPConfig()
	config.MaxBodyBytes = 16
	client := routedTestClient(t, server.URL, config, staticIPResolver{
		"search.test": {net.ParseIP("203.0.113.17")},
	})
	tool := &WebSearch{
		Provider: "searxng",
		BaseURL:  routedURL(t, server.URL, "search.test", ""),
		Client:   client,
	}
	result := tool.Execute(context.Background(), json.RawMessage(`{"query":"x"}`))
	if !result.IsError || !strings.Contains(result.Content, string(HTTPBodyLimitExceeded)) {
		t.Fatalf("WebSearch.Execute() = %#v, want body-limit error", result)
	}
}

func TestSafeHTTPRejectsBinaryContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2, 3})
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.18")},
	})
	_, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/binary"), nil, 64)
	if !IsHTTPErrorCode(err, HTTPContentTypeDenied) {
		t.Fatalf("Get() error = %v, want content-type error", err)
	}
}

func TestSafeHTTPClassifiesHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := routedTestClient(t, server.URL, DefaultSafeHTTPConfig(), staticIPResolver{
		"public.test": {net.ParseIP("203.0.113.19")},
	})
	_, err := client.Get(context.Background(), routedURL(t, server.URL, "public.test", "/status"), nil, 64)
	if !IsHTTPErrorCode(err, HTTPStatusFailure) {
		t.Fatalf("Get() error = %v, want HTTP status error", err)
	}
	var typed *HTTPError
	if !errors.As(err, &typed) || typed.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("typed error = %#v", typed)
	}
}

func TestSafeHTTPHonorsCancellationAndDeadline(t *testing.T) {
	resolver := staticIPResolver{"public.test": {net.ParseIP("203.0.113.20")}}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	t.Run("cancelled", func(t *testing.T) {
		client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), resolver, dial)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Get(ctx, "http://public.test/data", nil, 64)
		if !IsHTTPErrorCode(err, HTTPContextCancelled) {
			t.Fatalf("Get() error = %v, want cancellation", err)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		client := newTestSafeHTTPClient(t, DefaultSafeHTTPConfig(), resolver, dial)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err := client.Get(ctx, "http://public.test/data", nil, 64)
		if !IsHTTPErrorCode(err, HTTPTimeout) {
			t.Fatalf("Get() error = %v, want timeout", err)
		}
	})
}

func TestNewSafeHTTPClientRejectsInvalidConfiguration(t *testing.T) {
	resolver := staticIPResolver{}
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("unused")
	}

	tests := []SafeHTTPConfig{
		{Timeout: 0, MaxBodyBytes: 1, MaxRedirects: 1},
		{Timeout: time.Second, MaxBodyBytes: 0, MaxRedirects: 1},
		{Timeout: time.Second, MaxBodyBytes: 1, MaxRedirects: -1},
	}
	for _, config := range tests {
		if _, err := newSafeHTTPClient(config, resolver, dial); err == nil {
			t.Fatalf("newSafeHTTPClient(%+v) error = nil", config)
		}
	}
}

func newTestSafeHTTPClient(
	t *testing.T,
	config SafeHTTPConfig,
	resolver ipResolver,
	dial dialContextFunc,
) *SafeHTTPClient {
	t.Helper()
	client, err := newSafeHTTPClient(config, resolver, dial)
	if err != nil {
		t.Fatalf("newSafeHTTPClient() error = %v", err)
	}
	return client
}

func routedTestClient(
	t *testing.T,
	serverURL string,
	config SafeHTTPConfig,
	resolver ipResolver,
) *SafeHTTPClient {
	t.Helper()
	server, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	dialer := &net.Dialer{}
	return newTestSafeHTTPClient(t, config, resolver, func(
		ctx context.Context,
		network string,
		_ string,
	) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Host)
	})
}

func routedURL(t *testing.T, serverURL, hostname, path string) string {
	t.Helper()
	server, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port := server.Port()
	if port == "" {
		t.Fatalf("server URL %q has no port", serverURL)
	}
	return "http://" + net.JoinHostPort(hostname, port) + path
}

func redirectURL(t *testing.T, requestHost, hostname, path string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(requestHost)
	if err != nil {
		t.Fatalf("split request host: %v", err)
	}
	return "http://" + net.JoinHostPort(hostname, port) + path
}
