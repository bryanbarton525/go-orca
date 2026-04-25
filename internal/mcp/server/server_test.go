package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// TestAddCapability_RoundTrip verifies that AddCapability registers a tool
// the SDK auto-derives its schema from capabilities.Args, the handler is
// invoked with the decoded args, and the marshalled CapabilityResult comes
// back in a TextContent payload that downstream parsers can decode.
func TestAddCapability_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := server.New(server.Options{Name: "test", Version: "0.1.0"})
	server.AddCapability(srv, "echo_phase", "echo the phase",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			return capabilities.Result{
				Passed:  true,
				Success: true,
				Stdout:  "phase=" + args.Phase + " workflow=" + args.WorkflowID,
			}
		})

	// Connect the SDK client to the server's underlying *sdkmcp.Server via
	// in-memory transport; the framework's HTTP layer is exercised manually
	// in integration tests.
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	go func() {
		_ = srv.MCPServer().Run(ctx, serverT)
	}()
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	session, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	found := false
	for _, tool := range listed.Tools {
		if tool.Name == "echo_phase" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("echo_phase tool not advertised")
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "echo_phase",
		Arguments: map[string]any{
			"phase":       "implementation",
			"workflow_id": "wf-42",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %+v", res)
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	var parsed capabilities.Result
	if err := json.Unmarshal([]byte(tc.Text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !parsed.Passed || !strings.Contains(parsed.Stdout, "phase=implementation") {
		t.Errorf("unexpected result: %+v", parsed)
	}
	if !strings.Contains(parsed.Stdout, "workflow=wf-42") {
		t.Errorf("workflow_id not echoed: %q", parsed.Stdout)
	}
}

func TestAddCheckpointCapability_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := server.New(server.Options{Name: "test"})
	server.AddCheckpointCapability(srv, "git_checkpoint", "create a checkpoint",
		func(ctx context.Context, args capabilities.Args) (capabilities.CheckpointResult, error) {
			return capabilities.CheckpointResult{
				CommitSHA: "abc123",
				Branch:    "workflow/" + args.WorkflowID,
				Message:   "checkpoint after " + args.Phase,
				Pushed:    args.Push,
			}, nil
		})

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.MCPServer().Run(ctx, serverT) }()
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	session, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "git_checkpoint",
		Arguments: map[string]any{
			"phase":       "implementation",
			"workflow_id": "wf-7",
			"push":        true,
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tc := res.Content[0].(*sdkmcp.TextContent)
	var parsed capabilities.CheckpointResult
	if err := json.Unmarshal([]byte(tc.Text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.CommitSHA != "abc123" || parsed.Branch != "workflow/wf-7" || !parsed.Pushed {
		t.Errorf("unexpected checkpoint result: %+v", parsed)
	}
}
