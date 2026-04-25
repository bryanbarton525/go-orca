package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/server"
)

// TestRoundTrip exercises the workspace_write -> workspace_read -> workspace_list
// -> workspace_stat -> workspace_mkdir sequence end to end.
func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "wf-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	call := func(name string, args map[string]any) *sdkmcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: name, Arguments: args,
		})
		if err != nil {
			t.Fatalf("%s call: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s reported error: %+v", name, res)
		}
		return res
	}

	wr := decode[writeResult](t, call("workspace_write", map[string]any{
		"workspace_path": "wf-1",
		"path":           "src/main.go",
		"content":        "package main\nfunc main() {}\n",
		"create_dirs":    true,
	}))
	if wr.BytesWritten == 0 {
		t.Errorf("expected non-zero BytesWritten, got %+v", wr)
	}

	rd := decode[readResult](t, call("workspace_read", map[string]any{
		"workspace_path": "wf-1",
		"path":           "src/main.go",
	}))
	if !strings.Contains(rd.Content, "package main") {
		t.Errorf("read content unexpected: %q", rd.Content)
	}
	if rd.Truncated {
		t.Errorf("expected not truncated for small file")
	}

	lr := decode[listResult](t, call("workspace_list", map[string]any{
		"workspace_path": "wf-1",
		"path":           "src",
	}))
	if len(lr.Entries) != 1 || lr.Entries[0].Name != "main.go" {
		t.Errorf("unexpected list entries: %+v", lr.Entries)
	}

	sr := decode[statResult](t, call("workspace_stat", map[string]any{
		"workspace_path": "wf-1",
		"path":           "src/main.go",
	}))
	if !sr.Exists || sr.IsDir {
		t.Errorf("unexpected stat: %+v", sr)
	}

	mr := decode[mkdirResult](t, call("workspace_mkdir", map[string]any{
		"workspace_path": "wf-1",
		"path":           "tests",
		"parents":        true,
	}))
	if !mr.Created {
		t.Errorf("expected mkdir to succeed, got %+v", mr)
	}
}

// TestEscape verifies that workspace_read rejects path traversal at the
// policy layer before any file is touched.
func TestEscape(t *testing.T) {
	root := t.TempDir()
	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	r, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "workspace_read",
		Arguments: map[string]any{
			"workspace_path": "wf-x",
			"path":           "../../../etc/passwd",
		},
	})
	if err == nil && (r == nil || !r.IsError) {
		t.Fatal("expected escape rejection")
	}
}

// TestReadCap verifies that workspace_read truncates large files.
func TestReadCap(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "wf-2")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", 32)
	if err := os.WriteFile(filepath.Join(workspace, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := server.New(server.Options{Name: "test"})
	register(srv, root, 16, 1024, zap.NewNop())

	session := connect(t, srv)
	defer session.Close()

	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "workspace_read",
		Arguments: map[string]any{
			"workspace_path": "wf-2",
			"path":           "big.txt",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rd := decode[readResult](t, res)
	if !rd.Truncated {
		t.Errorf("expected Truncated=true for oversize file, got %+v", rd)
	}
	if len(rd.Content) != 16 {
		t.Errorf("expected 16-byte content, got %d", len(rd.Content))
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newServer(t *testing.T, root string) *server.Server {
	t.Helper()
	srv := server.New(server.Options{Name: "test"})
	register(srv, root, 1<<20, 8<<20, zap.NewNop())
	return srv
}

func connect(t *testing.T, srv *server.Server) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.MCPServer().Run(ctx, serverT) }()
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	session, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return session
}

func decode[T any](t *testing.T, r *sdkmcp.CallToolResult) T {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("empty content")
	}
	tc, ok := r.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	var v T
	if err := json.Unmarshal([]byte(tc.Text), &v); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, tc.Text)
	}
	return v
}
