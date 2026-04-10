// Package mcp provides an adapter that connects to remote MCP servers using
// the official Model Context Protocol Go SDK and bridges their tools into the
// gorca tool registry.
//
// # Transport support
//
// Two HTTP transports are supported, matching the two MCP HTTP specs:
//
//   - [TransportStreamable] (default) — 2025-03-26 Streamable HTTP transport.
//     Connect to servers that expose a single HTTP endpoint (e.g. /mcp).
//
//   - [TransportSSE] — 2024-11-05 SSE transport.
//     Connect to legacy servers that use a GET+SSE handshake.
//
// # Usage
//
//	reg := tools.NewRegistry()
//	session, err := mcp.Load(ctx, reg, "http://localhost:3000/mcp", mcp.LoaderOptions{})
//	if err != nil { ... }
//	defer session.Close()
//
// The returned [*mcp.ClientSession] must be kept alive as long as the tools
// are in use; closing it prevents further tool calls.
//
// # Stdio servers
//
// For command-based MCP servers use [LoadCommand]:
//
//	session, err := mcp.LoadCommand(ctx, reg, "uvx", []string{"mcp-server-fetch"}, mcp.LoaderOptions{})
package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/tools"
)

// Transport selects the HTTP transport variant used when connecting to a
// remote MCP server over HTTP/HTTPS.
type Transport int

const (
	// TransportStreamable uses the 2025-03-26 Streamable HTTP transport
	// (single endpoint, POST + optional SSE stream).  This is the default.
	TransportStreamable Transport = iota

	// TransportSSE uses the 2024-11-05 SSE transport
	// (GET handshake + persistent SSE connection + POST messages endpoint).
	// Use this for servers that have not yet migrated to the streamable spec.
	TransportSSE
)

// ─── MCPTool ──────────────────────────────────────────────────────────────────

// MCPTool is a tools.Tool implementation that delegates calls to a live
// MCP ClientSession.
type MCPTool struct {
	sdkTool    sdkmcp.Tool
	parameters json.RawMessage // cached JSON-Schema for Parameters()
	session    *sdkmcp.ClientSession
}

var _ tools.Tool = (*MCPTool)(nil)

// Name implements tools.Tool.
func (t *MCPTool) Name() string { return t.sdkTool.Name }

// Description implements tools.Tool.
func (t *MCPTool) Description() string { return t.sdkTool.Description }

// Parameters implements tools.Tool — returns the tool's JSON input schema.
func (t *MCPTool) Parameters() json.RawMessage { return t.parameters }

// Call implements tools.Tool.
// args must be a valid JSON object matching the tool's input schema.
// The first non-error text content from the MCP response is returned as JSON.
func (t *MCPTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	// Unmarshal the args into a map so we can pass them to the SDK.
	var argsMap map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return nil, fmt.Errorf("mcp: unmarshal args: %w", err)
		}
	}

	res, err := t.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      t.sdkTool.Name,
		Arguments: argsMap,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: call tool %q: %w", t.sdkTool.Name, err)
	}
	if res.IsError {
		// Collect the error message from text content, if any.
		msg := t.sdkTool.Name + ": tool reported an error"
		for _, c := range res.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				msg = tc.Text
				break
			}
		}
		return nil, fmt.Errorf("mcp: %s", msg)
	}

	// Collect text content from the result.  If the response contains
	// structured content, it takes precedence over plain text.
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			// Attempt to return as JSON; fall back to a JSON string if not valid JSON.
			raw := json.RawMessage(tc.Text)
			if json.Valid(raw) {
				return raw, nil
			}
			quoted, _ := json.Marshal(tc.Text)
			return quoted, nil
		}
	}
	return json.RawMessage(`null`), nil
}

// ─── Loader ───────────────────────────────────────────────────────────────────

// LoaderOptions configures the MCP server connection.
type LoaderOptions struct {
	// HTTPTimeout is the timeout applied to the HTTP client used for transport.
	// It controls individual request timeouts, not the session lifetime.
	// Defaults to 30 seconds.
	HTTPTimeout time.Duration

	// HTTPTransport selects the HTTP transport variant.  Defaults to [TransportStreamable].
	// Ignored by [LoadCommand].
	HTTPTransport Transport

	// HTTPClient overrides the HTTP client used for the transport.
	// When nil a default client with HTTPTimeout is used.
	// Ignored by [LoadCommand].
	HTTPClient *http.Client

	// TLSSkipVerify disables TLS certificate verification.
	// Use only in development environments with self-signed certificates.
	// Ignored by [LoadCommand] and when HTTPClient is set.
	TLSSkipVerify bool
}

// Load connects to a remote MCP server at endpoint, discovers its tools via
// ListTools, registers each as an MCPTool in reg, and returns the live session.
//
// The caller must keep the returned session open for as long as the tools are
// needed, and close it when done.
func Load(ctx context.Context, reg *tools.Registry, endpoint string, opts LoaderOptions) (*sdkmcp.ClientSession, error) {
	transport, err := buildHTTPTransport(endpoint, opts)
	if err != nil {
		return nil, err
	}
	return connect(ctx, reg, transport)
}

// LoadTransport connects to an MCP server using an already-constructed
// Transport, discovers tools, and registers them into reg.
// This is the preferred entry point for testing (use NewInMemoryTransports).
func LoadTransport(ctx context.Context, reg *tools.Registry, transport sdkmcp.Transport) (*sdkmcp.ClientSession, error) {
	return connect(ctx, reg, transport)
}

// LoadCommand launches a local MCP server subprocess and connects to it over
// stdio.  command is the executable and args are its arguments.
//
// The session must be kept open; closing it terminates the subprocess.
func LoadCommand(ctx context.Context, reg *tools.Registry, command string, args []string, opts LoaderOptions) (*sdkmcp.ClientSession, error) {
	transport := &sdkmcp.CommandTransport{Command: exec.Command(command, args...)}
	return connect(ctx, reg, transport)
}

// connect creates an MCP client, connects it via transport, lists tools, and
// registers each as an MCPTool.
func connect(ctx context.Context, reg *tools.Registry, transport sdkmcp.Transport) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "go-orca",
		Version: "1.0.0",
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect: %w", err)
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("mcp: list tools: %w", err)
	}

	for _, sdkTool := range result.Tools {
		t := sdkTool // capture loop variable
		params, err := marshalSchema(t.InputSchema)
		if err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("mcp: marshal schema for tool %q: %w", t.Name, err)
		}
		reg.Register(&MCPTool{
			sdkTool:    *t,
			parameters: params,
			session:    session,
		})
	}
	return session, nil
}

// buildHTTPTransport constructs the appropriate HTTP transport based on opts.
func buildHTTPTransport(endpoint string, opts LoaderOptions) (sdkmcp.Transport, error) {
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		transport := http.DefaultTransport
		if opts.TLSSkipVerify {
			transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for dev
			}
		}
		httpClient = &http.Client{Timeout: opts.HTTPTimeout, Transport: transport}
	}

	switch opts.HTTPTransport {
	case TransportSSE:
		return &sdkmcp.SSEClientTransport{
			Endpoint:   endpoint,
			HTTPClient: httpClient,
		}, nil
	default: // TransportStreamable
		return &sdkmcp.StreamableClientTransport{
			Endpoint:             endpoint,
			HTTPClient:           httpClient,
			DisableStandaloneSSE: true, // tool-call only; no server-initiated messages needed
		}, nil
	}
}

// marshalSchema converts the InputSchema (any) returned by the SDK into a
// json.RawMessage suitable for tools.Tool.Parameters().
func marshalSchema(schema any) (json.RawMessage, error) {
	if schema == nil {
		return json.RawMessage(`{}`), nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return b, nil
}
