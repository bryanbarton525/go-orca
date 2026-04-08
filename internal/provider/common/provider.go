// Package common defines the shared provider abstraction that all LLM / agent
// backends must implement.  New providers are registered via Register and
// looked up by name at runtime.
package common

import (
	"context"
	"io"
	"time"
)

// ─── Capability flags ────────────────────────────────────────────────────────

// Capability describes a feature flag exposed by a Provider.
type Capability string

const (
	CapabilityChat         Capability = "chat"
	CapabilityStreaming    Capability = "streaming"
	CapabilityToolCalling  Capability = "tool-calling"
	CapabilityAgentRuntime Capability = "agent-runtime"
	CapabilitySkills       Capability = "skills"
	CapabilityHandoffs     Capability = "handoffs"
	CapabilityEmbeddings   Capability = "embeddings"
	CapabilityModelList    Capability = "model-list"
)

// ─── Core request / response types ──────────────────────────────────────────

// Role identifies the participant in a conversation turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single turn in a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a model-generated request to invoke a tool.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolDefinition describes a tool the model may call.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema object
}

// ChatRequest is the canonical request sent to any provider.
type ChatRequest struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	Tools       []ToolDefinition  `json:"tools,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	// JSONMode asks the provider to constrain output to valid JSON.
	// Supported by Ollama (format=json) and OpenAI (response_format=json_object).
	// Ignored when OutputSchema is set (schema is strictly stronger).
	JSONMode bool `json:"json_mode,omitempty"`
	// OutputSchema is a JSON Schema object that constrains the model's output
	// to a specific structure. When set, providers use their strongest
	// structured-output mechanism (Ollama: format=<schema>,
	// OpenAI: response_format=json_schema, Anthropic: output_format=json_schema).
	OutputSchema map[string]any `json:"output_schema,omitempty"`
	// SchemaName is a short identifier for the schema, used by providers that
	// require a name (OpenAI json_schema response format).
	SchemaName string `json:"schema_name,omitempty"`
}

// ChatResponse is the canonical response from any provider.
type ChatResponse struct {
	ID           string        `json:"id"`
	Model        string        `json:"model"`
	Message      Message       `json:"message"`
	FinishReason string        `json:"finish_reason"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
	Latency      time.Duration `json:"latency"`
	// Truncated is true when the model stopped because it hit the token limit
	// rather than producing a complete response.
	Truncated bool `json:"truncated,omitempty"`
}

// StreamChunk is a single delta yielded during a streaming response.
type StreamChunk struct {
	ID    string `json:"id"`
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
	// Tool call deltas when streaming tool responses.
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}

// ModelInfo describes an available model on a provider.
type ModelInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ─── Provider interface ───────────────────────────────────────────────────────

// Provider is the interface that all LLM/agent backends must satisfy.
type Provider interface {
	// Name returns the unique identifier for this provider (e.g. "openai").
	Name() string

	// Capabilities returns the set of features this provider supports.
	Capabilities() []Capability

	// HasCapability reports whether the provider supports a given capability.
	HasCapability(Capability) bool

	// Chat performs a blocking completion request.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// Stream performs a streaming completion, writing chunks to the returned
	// reader.  Callers should close the reader when done.
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// Models lists the models available on this provider.
	Models(ctx context.Context) ([]ModelInfo, error)

	// HealthCheck verifies connectivity to the provider backend.
	HealthCheck(ctx context.Context) error
}

// ─── Registry ────────────────────────────────────────────────────────────────

var registry = map[string]Provider{}

// Register adds a provider to the global registry.
// Panics if a provider with the same name is already registered.
func Register(p Provider) {
	if _, exists := registry[p.Name()]; exists {
		panic("provider: duplicate registration: " + p.Name())
	}
	registry[p.Name()] = p
}

// Get returns the named provider, or (nil, false) if it is not registered.
func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns all registered providers.
func All() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

// ─── BaseProvider helper ──────────────────────────────────────────────────────

// BaseProvider provides default implementations of HasCapability.
// Embed this in concrete providers to avoid repeating the check loop.
type BaseProvider struct {
	caps []Capability
}

// NewBaseProvider initialises a BaseProvider with the given capability set.
func NewBaseProvider(caps ...Capability) BaseProvider {
	return BaseProvider{caps: caps}
}

// Capabilities returns the stored capability slice.
func (b BaseProvider) Capabilities() []Capability { return b.caps }

// HasCapability reports whether cap is in the stored set.
func (b BaseProvider) HasCapability(cap Capability) bool {
	for _, c := range b.caps {
		if c == cap {
			return true
		}
	}
	return false
}

// ─── Streaming helpers ───────────────────────────────────────────────────────

// DrainStream reads all chunks from ch and returns the concatenated text plus
// the final chunk.  Useful in non-streaming contexts that still call Stream.
func DrainStream(ch <-chan StreamChunk) (string, error) {
	var buf []byte
	for chunk := range ch {
		buf = append(buf, chunk.Delta...)
		if chunk.Done {
			break
		}
	}
	return string(buf), nil
}

// NopCloser wraps an io.Reader into an io.ReadCloser with a no-op Close.
func NopCloser(r io.Reader) io.ReadCloser {
	return io.NopCloser(r)
}
