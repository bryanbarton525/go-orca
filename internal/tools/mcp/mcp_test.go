package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-orca/go-orca/internal/tools"
	"github.com/go-orca/go-orca/internal/tools/mcp"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// startMCPServer starts an httptest server that serves a manifest at /manifest
// and handles JSON-RPC calls for each tool at the endpoint defined in the manifest.
func startMCPServer(t *testing.T, manifest mcp.Manifest, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	})
	if handler != nil {
		for _, def := range manifest.Tools {
			ep := def.Endpoint
			mux.HandleFunc(ep, handler)
		}
	}
	return httptest.NewServer(mux)
}

func rpcSuccessBody(id string, result interface{}) []byte {
	type resp struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      string      `json:"id"`
		Result  interface{} `json:"result"`
	}
	b, _ := json.Marshal(resp{JSONRPC: "2.0", ID: id, Result: result})
	return b
}

func rpcErrorBody(id, msg string) []byte {
	type errObj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Error   errObj `json:"error"`
	}
	b, _ := json.Marshal(resp{JSONRPC: "2.0", ID: id, Error: errObj{Code: -1, Message: msg}})
	return b
}

// ─── Load tests ───────────────────────────────────────────────────────────────

func TestLoad_RegistersTools(t *testing.T) {
	manifest := mcp.Manifest{
		Name:    "test-server",
		Version: "1.0.0",
		Tools: []mcp.ToolDef{
			{Name: "echo", Description: "Echoes input", Parameters: json.RawMessage(`{}`), Endpoint: "/tools/echo"},
			{Name: "ping", Description: "Pings", Parameters: json.RawMessage(`{}`), Endpoint: "/tools/ping"},
		},
	}
	srv := startMCPServer(t, manifest, nil)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, name := range []string{"echo", "ping"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered after Load", name)
		}
	}
}

func TestLoad_ToolMetadata(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
	manifest := mcp.Manifest{
		Name:    "meta-server",
		Version: "2.0",
		Tools: []mcp.ToolDef{
			{Name: "shout", Description: "Shouts text", Parameters: params, Endpoint: "/tools/shout"},
		},
	}
	srv := startMCPServer(t, manifest, nil)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	tool, ok := reg.Get("shout")
	if !ok {
		t.Fatal("tool shout not found")
	}
	if tool.Name() != "shout" {
		t.Errorf("Name: got %q", tool.Name())
	}
	if tool.Description() != "Shouts text" {
		t.Errorf("Description: got %q", tool.Description())
	}
	rawParams := tool.Parameters()
	if len(rawParams) == 0 {
		t.Error("Parameters: empty")
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(rawParams, &schema); err != nil {
		t.Errorf("Parameters: not valid JSON: %v", err)
	}
}

func TestLoad_ManifestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{})
	if err == nil {
		t.Error("expected error for server-500 manifest, got nil")
	}
}

func TestLoad_ManifestUnreachable(t *testing.T) {
	reg := tools.NewRegistry()
	// Use a port that is not listening.
	err := mcp.Load(reg, "http://127.0.0.1:19999/manifest", mcp.LoaderOptions{})
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestLoad_ManifestInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-valid-json{"))
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	err := mcp.Load(reg, srv.URL, mcp.LoaderOptions{})
	if err == nil {
		t.Error("expected error for invalid manifest JSON, got nil")
	}
}

func TestLoad_EmptyToolList(t *testing.T) {
	manifest := mcp.Manifest{Name: "empty", Version: "0.1", Tools: []mcp.ToolDef{}}
	srv := startMCPServer(t, manifest, nil)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.All()) != 0 {
		t.Errorf("expected 0 tools, got %d", len(reg.All()))
	}
}

// ─── MCPTool.Call tests ───────────────────────────────────────────────────────

func TestMCPTool_Call_Success(t *testing.T) {
	manifest := mcp.Manifest{
		Name:    "echo-server",
		Version: "1.0",
		Tools: []mcp.ToolDef{
			{Name: "echo", Description: "echo", Parameters: json.RawMessage(`{}`), Endpoint: "/tools/echo"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	})
	mux.HandleFunc("/tools/echo", func(w http.ResponseWriter, r *http.Request) {
		// Parse incoming RPC request to echo back the id
		type rpcReq struct {
			ID string `json:"id"`
		}
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rpcSuccessBody(req.ID, map[string]string{"echo": "pong"}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	tool, _ := reg.Get("echo")
	raw, err := tool.Call(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["echo"] != "pong" {
		t.Errorf("echo: got %q, want %q", result["echo"], "pong")
	}
}

func TestMCPTool_Call_RPCError(t *testing.T) {
	manifest := mcp.Manifest{
		Name:    "err-server",
		Version: "1.0",
		Tools: []mcp.ToolDef{
			{Name: "fail", Description: "always fails", Parameters: json.RawMessage(`{}`), Endpoint: "/tools/fail"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	})
	mux.HandleFunc("/tools/fail", func(w http.ResponseWriter, r *http.Request) {
		type rpcReq struct {
			ID string `json:"id"`
		}
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rpcErrorBody(req.ID, "something went wrong"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	tool, _ := reg.Get("fail")
	_, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from RPC error response, got nil")
	}
}

func TestMCPTool_Call_HTTPError(t *testing.T) {
	manifest := mcp.Manifest{
		Name:    "http-err-server",
		Version: "1.0",
		Tools: []mcp.ToolDef{
			{Name: "badtool", Description: "http fail", Parameters: json.RawMessage(`{}`), Endpoint: "/tools/badtool"},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	})
	mux.HandleFunc("/tools/badtool", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("not json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	tool, _ := reg.Get("badtool")
	_, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for non-JSON response, got nil")
	}
}

// ─── resolveEndpoint (via Load) ───────────────────────────────────────────────

func TestLoad_AbsoluteEndpoint(t *testing.T) {
	// The tool endpoint points to a different path served by the same server.
	mux := http.NewServeMux()
	var capturedPath string
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		// Manifest where endpoint is absolute (uses full server URL).
		// We don't know the server URL yet at write time, so we'll update after server starts.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Serve the manifest with an absolute endpoint URL.
	absEndpoint := srv.URL + "/absolute/tool"
	mux.HandleFunc("/absolute/tool", func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		type rpcReq struct{ ID string }
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rpcSuccessBody(req.ID, "ok"))
	})

	// Override the manifest handler now that we have absEndpoint.
	mux.HandleFunc("/manifest-abs", func(w http.ResponseWriter, r *http.Request) {
		m := mcp.Manifest{
			Name: "abs", Version: "1",
			Tools: []mcp.ToolDef{{Name: "abstool", Description: "d", Parameters: json.RawMessage(`{}`), Endpoint: absEndpoint}},
		}
		_ = json.NewEncoder(w).Encode(m)
	})

	reg := tools.NewRegistry()
	if err := mcp.Load(reg, srv.URL+"/manifest-abs", mcp.LoaderOptions{}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	tool, ok := reg.Get("abstool")
	if !ok {
		t.Fatal("abstool not registered")
	}
	_, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if capturedPath != "/absolute/tool" {
		t.Errorf("endpoint path: got %q, want /absolute/tool", capturedPath)
	}
}
