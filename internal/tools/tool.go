// Package tools defines the Tool interface, the registry, and JSON-schema
// helpers used to advertise tools to the LLM via OpenAI function-calling.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ─── Core types ───────────────────────────────────────────────────────────────

// Result is the structured response returned by a Tool.
type Result struct {
	Content string
	IsError bool
}

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() ParameterSchema
	Execute(ctx context.Context, args json.RawMessage) Result
}

// Errorf builds an error Result — shared by all tools (DRY).
func Errorf(format string, a ...any) Result {
	return Result{Content: "ERROR: " + fmt.Sprintf(format, a...), IsError: true}
}

// ─── JSON Schema ──────────────────────────────────────────────────────────────

type ParameterSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

func NewSchema(required []string, props map[string]Property) ParameterSchema {
	return ParameterSchema{Type: "object", Properties: props, Required: required}
}

type Definition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  ParameterSchema `json:"parameters"`
	} `json:"function"`
}

// Registry holds registered tools in insertion order.
type Registry struct {
	tools  []Tool
	policy ToolPolicy
	denied map[string]struct{}
}

// NewRegistry accepts an explicit policy. Omitting it uses the secure default.
func NewRegistry(policies ...ToolPolicy) *Registry {
	policy := DefaultToolPolicy()
	if len(policies) > 0 {
		policy = policies[0]
	}
	return &Registry{policy: policy, denied: make(map[string]struct{})}
}

// Register stores an allowed tool. Denied tools are remembered so execution
// can distinguish a policy rejection from an unknown tool name.
func (r *Registry) Register(t Tool) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if t == nil {
		return fmt.Errorf("tool is nil")
	}
	name := t.Name()
	if !r.policy.Allows(name) {
		r.denied[name] = struct{}{}
		return &ToolDeniedError{Name: name}
	}
	if _, exists := r.Get(name); exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools = append(r.tools, t)
	return nil
}

func (r *Registry) IsDenied(name string) bool {
	if r == nil {
		return false
	}
	_, denied := r.denied[name]
	return denied
}

func (r *Registry) Get(name string) (Tool, bool) {
	for _, t := range r.tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

func (r *Registry) Definitions() []Definition {
	defs := make([]Definition, len(r.tools))
	for i, t := range r.tools {
		defs[i].Type = "function"
		defs[i].Function.Name = t.Name()
		defs[i].Function.Description = t.Description()
		defs[i].Function.Parameters = t.Schema()
	}
	return defs
}

func (r *Registry) Names() []string {
	names := make([]string, len(r.tools))
	for i, t := range r.tools {
		names[i] = t.Name()
	}
	return names
}
