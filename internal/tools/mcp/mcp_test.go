package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/tools"
	"github.com/go-orca/go-orca/internal/tools/mcp"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// startInProcessServer creates an MCP server, adds tools to it via setUp,
// then connects a client through an in-memory transport and returns the
// live session along with the loaded tool registry.
func startInProcessServer(t *testing.T, setUp func(srv *sdkmcp.Server)) (*sdkmcp.ClientSession, *tools.Registry) {
	t.Helper()

	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "1.0"}, nil)
	if setUp != nil {
		setUp(srv)
	}

	// In-memory transport: serverT connects the server, clientT connects the client.
	serverT, clientT := sdkmcp.NewInMemoryTransports()

	// Run the server in a goroutine; it will stop when the transport closes.
	go func() {
		_ = srv.Run(context.Background(), serverT)
	}()

	reg := tools.NewRegistry()
	session, err := mcp.LoadTransport(context.Background(), reg, clientT)
	if err != nil {
		t.Fatalf("LoadTransport: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, reg
}

// ─── Load tests ───────────────────────────────────────────────────────────────

func TestLoad_RegistersTools(t *testing.T) {
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "echo", Description: "Echoes input"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil, nil
		})
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "ping", Description: "Pings"}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil, nil
		})
	})

	for _, name := range []string{"echo", "ping"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered after Load", name)
		}
	}
}

func TestLoad_ToolMetadata(t *testing.T) {
	type shoutArgs struct {
		Text string `json:"text" jsonschema:"the text to shout"`
	}
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "shout", Description: "Shouts text"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, args shoutArgs) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: args.Text}}}, nil, nil
			})
	})

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
	raw := tool.Parameters()
	if len(raw) == 0 {
		t.Error("Parameters: empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Errorf("Parameters: not valid JSON: %v", err)
	}
}

func TestLoad_EmptyToolList(t *testing.T) {
	_, reg := startInProcessServer(t, nil) // no tools registered

	if n := len(reg.All()); n != 0 {
		t.Errorf("expected 0 tools, got %d", n)
	}
}

func TestLoad_Unreachable(t *testing.T) {
	reg := tools.NewRegistry()
	// Use a port that is not listening to verify connection error is returned.
	_, err := mcp.Load(context.Background(), reg, "http://127.0.0.1:19999/mcp", mcp.LoaderOptions{})
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

// ─── MCPTool.Call tests ───────────────────────────────────────────────────────

func TestMCPTool_Call_Success(t *testing.T) {
	type echoArgs struct {
		Text string `json:"text"`
	}
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "echo", Description: "echo"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, args echoArgs) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf(`{"echo":%q}`, args.Text)}},
				}, nil, nil
			})
	})

	tool, _ := reg.Get("echo")
	raw, err := tool.Call(context.Background(), json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["echo"] != "hello" {
		t.Errorf("echo: got %q, want %q", result["echo"], "hello")
	}
}

func TestMCPTool_Call_ToolError(t *testing.T) {
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "fail", Description: "always fails"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
				return nil, nil, fmt.Errorf("intentional failure")
			})
	})

	tool, _ := reg.Get("fail")
	_, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error from failing tool, got nil")
	}
}

func TestMCPTool_Call_NonJSONTextResult(t *testing.T) {
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "plain", Description: "returns plain text"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hello world"}},
				}, nil, nil
			})
	})

	tool, _ := reg.Get("plain")
	raw, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// Non-JSON text should be returned as a JSON string.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("result should be a JSON string: %v", err)
	}
	if s != "hello world" {
		t.Errorf("got %q, want %q", s, "hello world")
	}
}

func TestMCPTool_Call_NilArgs(t *testing.T) {
	_, reg := startInProcessServer(t, func(srv *sdkmcp.Server) {
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "noop", Description: "no args"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
				}, nil, nil
			})
	})

	tool, _ := reg.Get("noop")
	_, err := tool.Call(context.Background(), nil)
	if err != nil {
		t.Fatalf("Call with nil args: %v", err)
	}
}

// ─── Reconnect tests ──────────────────────────────────────────────────────────

// TestMCPTool_Call_ReconnectsOnSessionLost simulates an MCP server restart by
// stopping the streaming HTTP server backing the client session, then starting
// a fresh server on the same address.  The expectation is that the next tool
// call transparently reconnects via the holder's session-lost detection +
// reconnect path, returning a successful result without manual intervention.
//
// This is the regression test for the production failure mode where pod
// rolling updates dropped server-side sessions and every subsequent tool
// call surfaced "session not found" until the API was restarted.
func TestMCPTool_Call_ReconnectsOnSessionLost(t *testing.T) {
	ctx := context.Background()

	// Reserve a port — we'll use the same one for both server "lifetimes".
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	var callCount atomic.Int64

	startServer := func() *http.Server {
		srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "restart-test", Version: "1.0"}, nil)
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "ping", Description: "responds with the call count"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, _ any) (*sdkmcp.CallToolResult, any, error) {
				n := callCount.Add(1)
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf(`{"n":%d}`, n)}},
				}, nil, nil
			})
		handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)

		mux := http.NewServeMux()
		mux.Handle("/mcp", handler)
		s := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		ln, lerr := net.Listen("tcp", addr)
		if lerr != nil {
			t.Fatalf("relisten: %v", lerr)
		}
		go func() { _ = s.Serve(ln) }()
		// Give the listener a moment to be ready before the client dials.
		time.Sleep(50 * time.Millisecond)
		return s
	}

	first := startServer()

	reg := tools.NewRegistry()
	session, err := mcp.Load(ctx, reg, "http://"+addr+"/mcp", mcp.LoaderOptions{HTTPTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer session.Close() //nolint:errcheck

	tool, ok := reg.Get("ping")
	if !ok {
		t.Fatal("ping tool not registered")
	}

	// First call — succeeds against the original server.
	if _, err := tool.Call(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("initial Call: %v", err)
	}

	// Simulate a server restart: shut the first server down hard and start a
	// second one on the same port.  The client's session ID will no longer
	// exist on the new server.
	shutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_ = first.Shutdown(shutCtx)
	cancel()

	_ = startServer() // new server, fresh in-memory session map

	// The next call MUST reconnect transparently.  Without the holder's
	// reconnect logic this returns "session not found".
	raw, err := tool.Call(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("post-restart Call: %v", err)
	}
	var got struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal post-restart result: %v", err)
	}
	if got.N == 0 {
		t.Errorf("expected n>0 after reconnect, got %d", got.N)
	}
}
