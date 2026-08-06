package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultToolPolicyDeniesMutation(t *testing.T) {
	policy := DefaultToolPolicy()
	for _, name := range []string{"fs_write", "fs_delete", "fs_mkdir"} {
		if policy.Allows(name) {
			t.Fatalf("DefaultToolPolicy().Allows(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"web_search", "web_fetch"} {
		if !policy.Allows(name) {
			t.Fatalf("DefaultToolPolicy().Allows(%q) = false, want true", name)
		}
	}
}

func TestRegistryContainsOnlyAllowedTools(t *testing.T) {
	registry := NewRegistry(NewToolPolicy("fs_read"))
	if err := registry.Register(FSRead{}); err != nil {
		t.Fatalf("Register(fs_read) error = %v", err)
	}
	if err := registry.Register(FSWrite{}); !IsToolDenied(err) {
		t.Fatalf("Register(fs_write) error = %v, want ToolDeniedError", err)
	}
	if got, want := strings.Join(registry.Names(), ","), "fs_read"; got != want {
		t.Fatalf("Names() = %q, want %q", got, want)
	}
	if _, ok := registry.Get("fs_write"); ok {
		t.Fatal("Get(fs_write) ok = true, want false")
	}
}

func TestExecutorRejectsDeniedTool(t *testing.T) {
	registry := NewRegistry(DefaultToolPolicy())
	if err := registry.Register(FSWrite{}); !IsToolDenied(err) {
		t.Fatalf("Register(fs_write) error = %v, want ToolDeniedError", err)
	}
	executor := NewExecutor(registry)
	messages, logLine := executor.RunAll(context.Background(), []ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: CallFunction{
			Name:      "fs_write",
			Arguments: json.RawMessage(`{"path":"x","content":"unsafe"}`),
		},
	}})
	if got, want := messages[0].Content, `ERROR: tool "fs_write" denied by policy`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if got, want := logLine, "✗ fs_write — denied"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestExecutorKeepsUnknownDistinctFromDenied(t *testing.T) {
	registry := NewRegistry(DefaultToolPolicy())
	executor := NewExecutor(registry)
	messages, _ := executor.RunAll(context.Background(), []ToolCall{{
		ID:       "call-1",
		Function: CallFunction{Name: "not_registered", Arguments: json.RawMessage(`{}`)},
	}})
	if got, want := messages[0].Content, `ERROR: unknown tool "not_registered"`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
