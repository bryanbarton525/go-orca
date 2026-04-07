// Package mcp provides an adapter that loads external tools via the
// Model Context Protocol (MCP) manifest format and bridges them into the
// gorca tool registry.
//
// # Manifest format
//
// An MCP server exposes a JSON manifest at a well-known URL (e.g.
// http://localhost:8181/manifest).  gorca expects the following shape:
//
//	{
//	  "name":    "my-mcp-server",
//	  "version": "1.0.0",
//	  "tools": [
//	    {
//	      "name":        "search_web",
//	      "description": "Searches the web and returns top results.",
//	      "parameters":  { "type": "object", "properties": { "query": { "type": "string" } }, "required": ["query"] },
//	      "endpoint":    "/tools/search_web"   // relative to manifest base URL
//	    }
//	  ]
//	}
//
// # JSON-RPC call protocol
//
// Each tool is invoked via HTTP POST to its endpoint with a JSON-RPC 2.0
// request body:
//
//	{ "jsonrpc": "2.0", "id": "<uuid>", "method": "<tool_name>", "params": <args> }
//
// The server must respond with either:
//
//	{ "jsonrpc": "2.0", "id": "<uuid>", "result": <json_value> }
//	{ "jsonrpc": "2.0", "id": "<uuid>", "error": { "code": -1, "message": "..." } }
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/go-orca/go-orca/internal/tools"
)

// ─── Manifest types ───────────────────────────────────────────────────────────

// Manifest is the top-level MCP server manifest returned by the /manifest
// endpoint (or equivalent).
type Manifest struct {
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Tools   []ToolDef `json:"tools"`
}

// ToolDef describes a single callable tool exposed by the MCP server.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	// Endpoint is the path (relative to the manifest base URL) to POST JSON-RPC calls to.
	Endpoint string `json:"endpoint"`
}

// ─── JSON-RPC wire types ──────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCPTool ──────────────────────────────────────────────────────────────────

// MCPTool is a tools.Tool implementation backed by a remote MCP endpoint.
type MCPTool struct {
	def        ToolDef
	callURL    string // fully resolved URL for JSON-RPC calls
	httpClient *http.Client
}

var _ tools.Tool = (*MCPTool)(nil)

// Name implements tools.Tool.
func (t *MCPTool) Name() string { return t.def.Name }

// Description implements tools.Tool.
func (t *MCPTool) Description() string { return t.def.Description }

// Parameters implements tools.Tool.
func (t *MCPTool) Parameters() json.RawMessage { return t.def.Parameters }

// Call implements tools.Tool — sends a JSON-RPC 2.0 request and returns the result.
func (t *MCPTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  t.def.Name,
		Params:  args,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.callURL,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read body: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, fmt.Errorf("mcp: parse response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// ─── Loader ───────────────────────────────────────────────────────────────────

// LoaderOptions configures the MCP manifest loader.
type LoaderOptions struct {
	// HTTPTimeout is the timeout for both manifest fetch and tool calls.
	// Defaults to 30 seconds.
	HTTPTimeout time.Duration
}

// Load fetches the MCP manifest from manifestURL, constructs an MCPTool for
// each tool defined in the manifest, and registers them all into reg.
//
// The manifestURL must be an absolute HTTP/HTTPS URL. Tool endpoint paths are
// resolved relative to the manifest base URL (scheme + host).
func Load(reg *tools.Registry, manifestURL string, opts LoaderOptions) error {
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 30 * time.Second
	}
	client := &http.Client{Timeout: opts.HTTPTimeout}

	// Fetch manifest.
	resp, err := client.Get(manifestURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("mcp: fetch manifest %s: %w", manifestURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp: manifest %s returned HTTP %d", manifestURL, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mcp: read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("mcp: parse manifest: %w", err)
	}

	// Resolve base URL (scheme + host) for relative endpoint paths.
	base, err := url.Parse(manifestURL)
	if err != nil {
		return fmt.Errorf("mcp: parse manifest url: %w", err)
	}
	baseURL := fmt.Sprintf("%s://%s", base.Scheme, base.Host)

	for _, def := range manifest.Tools {
		callURL := resolveEndpoint(baseURL, def.Endpoint)
		t := &MCPTool{
			def:        def,
			callURL:    callURL,
			httpClient: client,
		}
		reg.Register(t)
	}
	return nil
}

// resolveEndpoint resolves a possibly-relative endpoint path against a base URL.
func resolveEndpoint(baseURL, endpoint string) string {
	if endpoint == "" {
		return baseURL
	}
	// If endpoint is already absolute, use it directly.
	if u, err := url.Parse(endpoint); err == nil && u.IsAbs() {
		return endpoint
	}
	// Ensure exactly one slash between base and path.
	if len(endpoint) > 0 && endpoint[0] != '/' {
		endpoint = "/" + endpoint
	}
	return baseURL + endpoint
}
