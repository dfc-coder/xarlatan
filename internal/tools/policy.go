package tools

import (
	"errors"
	"fmt"
)

var defaultAllowedTools = []string{"web_search", "web_fetch"}

// ToolPolicy is an immutable allowlist used by both Registry and Executor.
type ToolPolicy struct {
	allowed  map[string]struct{}
	allowAll bool
}

// NewToolPolicy builds an allowlist policy.
func NewToolPolicy(names ...string) ToolPolicy {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	return ToolPolicy{allowed: allowed}
}

// DefaultToolPolicy enables network lookup tools and denies filesystem tools.
func DefaultToolPolicy() ToolPolicy {
	return NewToolPolicy(defaultAllowedTools...)
}

// AllowAllToolPolicy is intended for isolated tests and trusted internal wiring.
func AllowAllToolPolicy() ToolPolicy {
	return ToolPolicy{allowAll: true}
}

// WithAllowed returns a copy with additional names allowed.
func (p ToolPolicy) WithAllowed(names ...string) ToolPolicy {
	if p.allowAll {
		return p
	}
	copyPolicy := NewToolPolicy()
	for name := range p.allowed {
		copyPolicy.allowed[name] = struct{}{}
	}
	for _, name := range names {
		if name != "" {
			copyPolicy.allowed[name] = struct{}{}
		}
	}
	return copyPolicy
}

// Allows reports whether name may be registered and executed.
func (p ToolPolicy) Allows(name string) bool {
	if p.allowAll {
		return true
	}
	_, ok := p.allowed[name]
	return ok
}

// ToolDeniedError identifies a policy rejection separately from an unknown tool.
type ToolDeniedError struct {
	Name string
}

func (e *ToolDeniedError) Error() string {
	return fmt.Sprintf("tool %q denied by policy", e.Name)
}

// IsToolDenied reports whether err is a policy rejection.
func IsToolDenied(err error) bool {
	var denied *ToolDeniedError
	return errors.As(err, &denied)
}
