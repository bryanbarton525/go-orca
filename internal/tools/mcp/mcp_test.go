package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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
