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
	Type       string              `json:"type"` // always "object"
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

// ─── OpenAI wire types ────────────────────────────────────────────────────────

type Definition struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  ParameterSchema `json:"parameters"`
	} `json:"function"`
}

// ─── Registry — simple slice, N is always small ───────────────────────────────

// Registry holds registered tools in insertion order.
// Linear scan over <20 tools is faster than a map with hashing overhead.
type Registry struct {
	tools []Tool
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(t Tool) { r.tools = append(r.tools, t) }

func (r *Registry) Get(name string) (Tool, bool) {
	for _, t := range r.tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Definitions returns all tools in OpenAI wire format.
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
