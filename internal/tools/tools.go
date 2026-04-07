// Package tools defines the Tool interface and the global tool registry.
//
// Tools are discrete, callable capabilities that personas can invoke during
// execution (e.g. web search, code execution, file read, HTTP request).
// Built-in tools are compiled into the binary and registered at startup.
// External tools communicate over HTTP/JSON-RPC and are discovered by manifest.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Tool is the interface every tool must implement.
type Tool interface {
	// Name returns the unique tool identifier.
	Name() string
	// Description returns a human-readable description for prompt injection.
	Description() string
	// Parameters returns the JSON Schema (as raw bytes) describing accepted inputs.
	Parameters() json.RawMessage
	// Call executes the tool with the given JSON-encoded arguments and returns
	// the result as a JSON-encoded value.
	Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// Result wraps a tool call outcome.
type Result struct {
	ToolName string          `json:"tool_name"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// Registry holds all registered tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry. Panics on duplicate.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		panic("tools: duplicate registration: " + t.Name())
	}
	r.tools[t.Name()] = t
}

// Get returns the named tool, or (nil, false).
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns a snapshot of all registered tools.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Call dispatches a named tool call.
func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage) Result {
	t, ok := r.Get(name)
	if !ok {
		return Result{ToolName: name, Error: fmt.Sprintf("tool %q not registered", name)}
	}
	out, err := t.Call(ctx, args)
	if err != nil {
		return Result{ToolName: name, Error: err.Error()}
	}
	return Result{ToolName: name, Output: out}
}

// ToolSpec is the serializable descriptor injected into persona system prompts.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Specs returns a slice of ToolSpec for all registered tools.
func (r *Registry) Specs() []ToolSpec {
	all := r.All()
	specs := make([]ToolSpec, 0, len(all))
	for _, t := range all {
		specs = append(specs, ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return specs
}

// Global is the process-wide tool registry.
var Global = NewRegistry()
