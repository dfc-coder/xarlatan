package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type closeTrackingBody struct {
	reader io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHealthProbeClosesResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := &closeTrackingBody{reader: strings.NewReader("health")}
			probe := NewHTTPHealthProbe(doerFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       body,
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}))
			err := probe.Check(context.Background(), "http://127.0.0.1:8080/health")
			if status == http.StatusOK && err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if status != http.StatusOK && err == nil {
				t.Fatal("Check() error = nil for non-ready status")
			}
			if !body.closed.Load() {
				t.Fatal("health response body was not closed")
			}
		})
	}
}

func TestHealthProbeReturnsRequestAndTransportErrors(t *testing.T) {
	probe := NewHTTPHealthProbe(doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	}))
	if err := probe.Check(context.Background(), "://bad"); err == nil {
		t.Fatal("Check() invalid URL error = nil")
	}
	if err := probe.Check(context.Background(), "http://127.0.0.1:8080/health"); err == nil || !strings.Contains(err.Error(), "transport failed") {
		t.Fatalf("Check() error = %v, want transport failure", err)
	}
}
