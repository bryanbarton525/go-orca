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
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
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

// ─── sessionHolder ────────────────────────────────────────────────────────────

// sessionHolder owns the live MCP client session for a server and reconnects
// transparently when the underlying session is lost.  Server-side restarts
// (rolling updates, OOM kills, liveness-probe failures) drop the in-memory
// session even though the client keeps the same session ID, producing
// "session not found" errors on subsequent calls.  The holder catches these
// errors, closes the dead session, builds a fresh transport via newTransport,
// reconnects, and retries the call once.
//
// Reconnection is gated behind a mutex so concurrent failures do not race to
// open multiple replacements.  The holder is safe for concurrent use.
type sessionHolder struct {
	mu           sync.Mutex
	session      *sdkmcp.ClientSession
	newTransport func() (sdkmcp.Transport, error) // nil when reconnect is unsupported (e.g. opaque test transports)
}

// Current returns the live session pointer.  Used by the registry so the
// shutdown path can close any active session — a stale pointer left over
// from before a reconnect is harmless because Close() on an already-closed
// session is a no-op.
func (h *sessionHolder) Current() *sdkmcp.ClientSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.session
}

// Call invokes name on the held session.  On a session-lost error it
// reconnects once and retries; any other error is returned as-is.
func (h *sessionHolder) Call(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error) {
	h.mu.Lock()
	sess := h.session
	h.mu.Unlock()

	if sess == nil {
		if err := h.reconnect(ctx); err != nil {
			return nil, err
		}
		h.mu.Lock()
		sess = h.session
		h.mu.Unlock()
	}

	res, err := sess.CallTool(ctx, params)
	if err == nil {
		return res, nil
	}
	if h.newTransport == nil || !isSessionLostError(err) {
		return nil, err
	}

	if rerr := h.reconnect(ctx); rerr != nil {
		return nil, fmt.Errorf("%w (reconnect failed: %v)", err, rerr)
	}
	h.mu.Lock()
	sess = h.session
	h.mu.Unlock()
	return sess.CallTool(ctx, params)
}

// reconnect closes the current session (if any) and opens a new one using
// the configured transport factory.  Returns an error when reconnect is not
// supported (no factory configured) or when the new connection fails.
func (h *sessionHolder) reconnect(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.newTransport == nil {
		return errors.New("mcp: session reconnect not supported (no transport factory)")
	}

	if h.session != nil {
		_ = h.session.Close()
		h.session = nil
	}

	transport, err := h.newTransport()
	if err != nil {
		return fmt.Errorf("mcp: build reconnect transport: %w", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "go-orca", Version: "1.0.0"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("mcp: reconnect: %w", err)
	}
	h.session = sess
	return nil
}

// Close closes the held session.  Safe to call multiple times.
func (h *sessionHolder) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return nil
	}
	err := h.session.Close()
	h.session = nil
	return err
}

// isSessionLostError reports whether err indicates the MCP server-side
// session is gone and a fresh Connect() is required.  We match string
// fragments because the SDK does not (yet) expose typed errors for these
// transport states.  The patterns cover the three failure modes observed in
// production: a hard "session not found" rejection, a graceful "client is
// closing" mid-call, and a TCP-level "connection closed" / EOF.
func isSessionLostError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range []string{
		"session not found",
		"connection closed",
		"client is closing",
		"failed to connect",
		"EOF",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// ─── MCPTool ──────────────────────────────────────────────────────────────────

// MCPTool is a tools.Tool implementation that delegates calls to a live
// MCP ClientSession via a sessionHolder that reconnects on session loss.
type MCPTool struct {
	sdkTool    sdkmcp.Tool
	parameters json.RawMessage // cached JSON-Schema for Parameters()
	holder     *sessionHolder
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

	res, err := t.holder.Call(ctx, &sdkmcp.CallToolParams{
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
// needed, and close it when done.  Tool calls reconnect transparently when
// the server-side session is lost (e.g. after a server restart).
func Load(ctx context.Context, reg *tools.Registry, endpoint string, opts LoaderOptions) (*sdkmcp.ClientSession, error) {
	factory := func() (sdkmcp.Transport, error) {
		return buildHTTPTransport(endpoint, opts)
	}
	transport, err := factory()
	if err != nil {
		return nil, err
	}
	return connect(ctx, reg, transport, &sessionHolder{newTransport: factory})
}

// LoadTransport connects to an MCP server using an already-constructed
// Transport, discovers tools, and registers them into reg.
// This is the preferred entry point for testing (use NewInMemoryTransports).
//
// Reconnect-on-session-loss is NOT supported via this entry point because the
// caller-supplied transport is opaque and cannot be reproduced; tool calls
// after a session drop will return the original error.
func LoadTransport(ctx context.Context, reg *tools.Registry, transport sdkmcp.Transport) (*sdkmcp.ClientSession, error) {
	return connect(ctx, reg, transport, &sessionHolder{})
}

// LoadCommand launches a local MCP server subprocess and connects to it over
// stdio.  command is the executable and args are its arguments.
//
// The session must be kept open; closing it terminates the subprocess.
// Reconnect re-launches the subprocess so a crashed server is restarted on
// the next tool call.
func LoadCommand(ctx context.Context, reg *tools.Registry, command string, args []string, opts LoaderOptions) (*sdkmcp.ClientSession, error) {
	factory := func() (sdkmcp.Transport, error) {
		return &sdkmcp.CommandTransport{Command: exec.Command(command, args...)}, nil
	}
	transport, err := factory()
	if err != nil {
		return nil, err
	}
	return connect(ctx, reg, transport, &sessionHolder{newTransport: factory})
}

// connect creates an MCP client, connects it via transport, lists tools, and
// registers each as an MCPTool bound to holder.  After this call the holder
// owns the live session.
func connect(ctx context.Context, reg *tools.Registry, transport sdkmcp.Transport, holder *sessionHolder) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "go-orca",
		Version: "1.0.0",
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect: %w", err)
	}
	holder.session = session

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		holder.session = nil
		return nil, fmt.Errorf("mcp: list tools: %w", err)
	}

	for _, sdkTool := range result.Tools {
		t := sdkTool // capture loop variable
		params, err := marshalSchema(t.InputSchema)
		if err != nil {
			_ = session.Close()
			holder.session = nil
			return nil, fmt.Errorf("mcp: marshal schema for tool %q: %w", t.Name, err)
		}
		reg.Register(&MCPTool{
			sdkTool:    *t,
			parameters: params,
			holder:     holder,
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
