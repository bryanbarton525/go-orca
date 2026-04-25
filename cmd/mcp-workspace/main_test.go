package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// TestCreateAndInfo verifies workspace_create creates a fresh subdir and
// workspace_info reports its existence + non-git status.
func TestCreateAndInfo(t *testing.T) {
	root := t.TempDir()
	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	cr := decode[createResult](t, call(t, session, "workspace_create", map[string]any{
		"workflow_id": "wf-1",
	}))
	if !cr.Created {
		t.Errorf("expected Created=true, got %+v", cr)
	}
	if cr.RelPath != "wf-1" {
		t.Errorf("expected RelPath=wf-1, got %q", cr.RelPath)
	}
	if _, err := os.Stat(filepath.Join(root, "wf-1")); err != nil {
		t.Errorf("workspace dir missing: %v", err)
	}

	info := decode[infoResult](t, call(t, session, "workspace_info", map[string]any{
		"workflow_id": "wf-1",
	}))
	if !info.Exists {
		t.Errorf("expected Exists=true")
	}
	if info.IsGitRepo {
		t.Errorf("expected IsGitRepo=false for fresh dir")
	}
}

// TestInit_GitRepo verifies workspace_init makes a git repo and
// workspace_info reports the branch name.
func TestInit_GitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	root := t.TempDir()
	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	_ = call(t, session, "workspace_init", map[string]any{
		"workflow_id": "wf-2",
		"branch":      "main",
	})

	info := decode[infoResult](t, call(t, session, "workspace_info", map[string]any{
		"workflow_id": "wf-2",
	}))
	if !info.Exists || !info.IsGitRepo {
		t.Errorf("expected git repo, got %+v", info)
	}
	if info.Branch != "main" {
		t.Errorf("expected branch=main, got %q", info.Branch)
	}
}

// TestDestroy verifies workspace_destroy removes the workspace and
// workspace_info subsequently reports Exists=false.
func TestDestroy(t *testing.T) {
	root := t.TempDir()
	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	_ = call(t, session, "workspace_create", map[string]any{"workflow_id": "wf-3"})

	dr := decode[destroyResult](t, call(t, session, "workspace_destroy", map[string]any{
		"workflow_id": "wf-3",
	}))
	if !dr.Destroyed {
		t.Errorf("expected Destroyed=true, got %+v", dr)
	}
	if _, err := os.Stat(filepath.Join(root, "wf-3")); !os.IsNotExist(err) {
		t.Errorf("expected workspace removed, stat err=%v", err)
	}
}

// TestRejectsBadWorkflowID verifies that workflow IDs containing path
// separators or leading dots are rejected at the validation layer.
func TestRejectsBadWorkflowID(t *testing.T) {
	root := t.TempDir()
	srv := newServer(t, root)
	session := connect(t, srv)
	defer session.Close()

	for _, bad := range []string{"../escape", "wf/1", ".hidden", ""} {
		res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "workspace_create",
			Arguments: map[string]any{"workflow_id": bad},
		})
		// The SDK may return the rejection either as a Go error or as
		// CallToolResult.IsError = true; both indicate rejection.
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("expected rejection for workflow_id=%q", bad)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newServer(t *testing.T, root string) *server.Server {
	t.Helper()
	srv := server.New(server.Options{Name: "test"})
	allow := policy.Allowlist{
		"git": {"clone", "init", "config", "rev-parse", "symbolic-ref"},
	}
	register(srv, root, allow, policy.NopAuditor{}, zap.NewNop())
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

func call(t *testing.T, s *sdkmcp.ClientSession, name string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s call: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s reported error: %+v", name, res)
	}
	return res
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
