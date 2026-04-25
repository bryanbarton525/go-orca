package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// TestCheckpoint_HappyPath verifies that the git_checkpoint capability:
//   - auto-initialises a workspace that is not yet a git repo
//   - stages and commits the workspace contents
//   - returns a non-empty CommitSHA and a Branch
//
// The test uses a real `git` binary; it is skipped when git is unavailable
// (so CI on minimal runners doesn't fail spuriously).
func TestCheckpoint_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping integration test")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "wf-1")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, root)
	session := connectClient(t, srv)
	defer session.Close()

	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "git_checkpoint",
		Arguments: map[string]any{
			"workflow_id":    "wf-1",
			"phase":          "implementation",
			"workspace_path": "wf-1",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res)
	}

	cp := decodeCheckpoint(t, res)
	if cp.CommitSHA == "" {
		t.Errorf("expected non-empty CommitSHA, got %+v", cp)
	}
	if cp.Branch == "" {
		t.Errorf("expected non-empty Branch")
	}
	if !strings.Contains(cp.Message, "checkpoint implementation") {
		t.Errorf("unexpected commit message: %q", cp.Message)
	}

	// .git should now exist inside the workspace.
	if _, err := os.Stat(filepath.Join(workspace, ".git")); err != nil {
		t.Errorf(".git was not created: %v", err)
	}
}

// TestCheckpoint_NoChanges verifies that running git_checkpoint twice does
// not error — the second call has nothing to commit and is treated as a
// no-op (empty CommitSHA, no error).
func TestCheckpoint_NoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping integration test")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "wf-2")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "x.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, root)
	session := connectClient(t, srv)
	defer session.Close()

	args := map[string]any{
		"workflow_id":    "wf-2",
		"phase":          "implementation",
		"workspace_path": "wf-2",
	}
	r1, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "git_checkpoint", Arguments: args})
	if err != nil || r1.IsError {
		t.Fatalf("first checkpoint failed: err=%v isError=%v", err, r1.IsError)
	}

	r2, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "git_checkpoint", Arguments: args})
	if err != nil {
		t.Fatalf("second checkpoint failed: %v", err)
	}
	if r2.IsError {
		t.Fatalf("second checkpoint reported error: %+v", r2)
	}
	cp := decodeCheckpoint(t, r2)
	// The second call has nothing to commit; the SHA should still resolve
	// (it points at the previous commit) but Pushed should be false.
	if cp.Pushed {
		t.Errorf("expected Pushed=false on no-op checkpoint")
	}
}

// TestStatus_PorcelainOutput verifies git_status returns the expected
// porcelain output.  `?? ` lines indicate untracked files.
func TestStatus_PorcelainOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping integration test")
	}

	root := t.TempDir()
	workspace := filepath.Join(root, "wf-3")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	// Initialise as git repo manually so we can test git_status against an
	// already-init'd workspace.
	for _, argv := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "t@x"},
		{"git", "config", "user.name", "t"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = workspace
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, root)
	session := connectClient(t, srv)
	defer session.Close()

	r, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "git_status",
		Arguments: map[string]any{
			"workflow_id":    "wf-3",
			"workspace_path": "wf-3",
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool error: %+v", r)
	}
	tc := r.Content[0].(*sdkmcp.TextContent)
	var parsed capabilities.Result
	if err := json.Unmarshal([]byte(tc.Text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(parsed.Stdout, "?? new.txt") {
		t.Errorf("expected '?? new.txt' in stdout, got %q", parsed.Stdout)
	}
}

// TestCheckpoint_RejectsEscape verifies that workspace-path escapes are
// blocked at the policy layer before any git command runs.
func TestCheckpoint_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	srv := newTestServer(t, root)
	session := connectClient(t, srv)
	defer session.Close()

	r, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "git_checkpoint",
		Arguments: map[string]any{
			"workflow_id":    "wf-x",
			"phase":          "test",
			"workspace_path": "../../../etc",
		},
	})
	// The handler returns an error via CheckpointHandler's error return,
	// which the SDK translates to a non-nil error, not IsError on result.
	if err == nil && (r == nil || !r.IsError) {
		t.Fatal("expected error for path escape")
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTestServer(t *testing.T, root string) *server.Server {
	t.Helper()
	srv := server.New(server.Options{Name: "mcp-git", Version: "test"})
	allow := policy.Allowlist{
		"git": {"init", "config", "add", "commit", "status", "rev-parse", "push", "symbolic-ref", "checkout"},
	}
	register(srv, root, "t@x", "t", allow, policy.NopAuditor{})
	return srv
}

func connectClient(t *testing.T, srv *server.Server) *sdkmcp.ClientSession {
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

func decodeCheckpoint(t *testing.T, r *sdkmcp.CallToolResult) capabilities.CheckpointResult {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("empty result content")
	}
	tc, ok := r.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	var cp capabilities.CheckpointResult
	if err := json.Unmarshal([]byte(tc.Text), &cp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return cp
}
