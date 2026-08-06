package llm

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func validClientConfig(baseURL string) ClientConfig {
	return ClientConfig{
		BaseURL:     baseURL,
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   64,
	}
}

func TestClientRejectsInvalidBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"",
		"::://bad",
		"ftp://127.0.0.1:8080",
		"http://",
		"http://127.0.0.1:8080/unexpected/path",
		"http://user:pass@127.0.0.1:8080",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewClient(validClientConfig(baseURL)); err == nil {
				t.Fatalf("NewClient(%q) error = nil", baseURL)
			}
		})
	}
}

func TestClientReturnsMarshalOrRequestErrors(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		client, err := NewClient(
			validClientConfig("http://127.0.0.1:8080"),
			withJSONMarshal(func(any) ([]byte, error) {
				return nil, errors.New("marshal failed")
			}),
		)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		_, _, _, err = client.Generate(context.Background(), nil, "hello", nil)
		if err == nil || !strings.Contains(err.Error(), "marshal failed") {
			t.Fatalf("Generate() error = %v, want marshal failure", err)
		}
	})

	t.Run("request", func(t *testing.T) {
		client, err := NewClient(
			validClientConfig("http://127.0.0.1:8080"),
			withRequestFactory(func(context.Context, string, string, []byte) (*http.Request, error) {
				return nil, errors.New("request failed")
			}),
		)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		_, _, _, err = client.Generate(context.Background(), nil, "hello", nil)
		if err == nil || !strings.Contains(err.Error(), "request failed") {
			t.Fatalf("Generate() error = %v, want request failure", err)
		}
	})
}

func TestClientHasNoProcessLifecycle(t *testing.T) {
	clientType := "Client"
	for _, forbidden := range []string{"Start", "WaitReady", "Wait", "Stop", "Close"} {
		if hasMethod(&Client{}, forbidden) {
			t.Fatalf("%s unexpectedly exposes process lifecycle method %s", clientType, forbidden)
		}
	}
}

func hasMethod(value any, name string) bool {
	_, ok := reflect.TypeOf(value).MethodByName(name)
	return ok
}
