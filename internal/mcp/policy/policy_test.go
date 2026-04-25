package policy_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/mcp/policy"
)

func TestResolveWorkspacePath_AcceptsValidRel(t *testing.T) {
	root := t.TempDir()
	got, err := policy.ResolveWorkspacePath(root, "src/main.go")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := filepath.Join(root, "src", "main.go")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveWorkspacePath_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := policy.ResolveWorkspacePath(root, "../etc/passwd"); err == nil {
		t.Fatal("expected ErrEscapesWorkspace")
	}
}

func TestResolveWorkspacePath_AcceptsEmpty(t *testing.T) {
	root := t.TempDir()
	got, err := policy.ResolveWorkspacePath(root, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != root {
		t.Fatalf("got %q want %q", got, root)
	}
}

func TestAllowlist_CheckCommand(t *testing.T) {
	a := policy.Allowlist{
		"go":    {"test", "build"},
		"gofmt": {},
	}
	if err := a.CheckCommand([]string{"go", "test", "./..."}); err != nil {
		t.Errorf("go test should be allowed: %v", err)
	}
	if err := a.CheckCommand([]string{"gofmt", "-w", "."}); err != nil {
		t.Errorf("gofmt should be allowed: %v", err)
	}
	if err := a.CheckCommand([]string{"go", "run", "main.go"}); err == nil {
		t.Error("go run should be rejected")
	}
	if err := a.CheckCommand([]string{"rm", "-rf", "/"}); err == nil {
		t.Error("rm should be rejected")
	}
	if err := a.CheckCommand([]string{}); err == nil {
		t.Error("empty argv should be rejected")
	}
}

func TestFilterEnv(t *testing.T) {
	in := []string{"PATH=/usr/bin", "SECRET=abc", "HOME=/home/x", "GOCACHE=/tmp/c"}
	got := policy.FilterEnv(in, []string{"PATH", "HOME", "GOCACHE"})
	want := map[string]bool{"PATH=/usr/bin": true, "HOME=/home/x": true, "GOCACHE=/tmp/c": true}
	if len(got) != len(want) {
		t.Fatalf("len got=%d want=%d, got=%v", len(got), len(want), got)
	}
	for _, kv := range got {
		if !want[kv] {
			t.Errorf("unexpected entry: %q", kv)
		}
	}
}

func TestRun_RejectedByAllowlist(t *testing.T) {
	root := t.TempDir()
	res := policy.Run(context.Background(), policy.RunOptions{
		Argv:       []string{"rm", "-rf", "/"},
		WorkingDir: root,
		Allow:      policy.Allowlist{"echo": {}},
	})
	if res.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Error, "command not allowlisted") {
		t.Errorf("unexpected error: %q", res.Error)
	}
}

func TestRun_HappyPath_Echo(t *testing.T) {
	root := t.TempDir()
	res := policy.Run(context.Background(), policy.RunOptions{
		Argv:       []string{"echo", "hello"},
		WorkingDir: root,
		Allow:      policy.Allowlist{"echo": {}},
		Timeout:    5 * time.Second,
	})
	if !res.Success {
		t.Fatalf("expected success, got error=%q stderr=%q", res.Error, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Errorf("stdout=%q does not contain 'hello'", res.Stdout)
	}
	if md, ok := res.Metadata["command"].(string); !ok || md != "echo hello" {
		t.Errorf("metadata.command=%v want 'echo hello'", res.Metadata["command"])
	}
}

func TestRun_OutputCap(t *testing.T) {
	root := t.TempDir()
	// `yes` would loop forever; cap output and timeout to verify truncation.
	res := policy.Run(context.Background(), policy.RunOptions{
		Argv:           []string{"sh", "-c", "for i in $(seq 1 1000); do echo line$i; done"},
		WorkingDir:     root,
		Allow:          policy.Allowlist{"sh": {"-c"}},
		Timeout:        2 * time.Second,
		MaxOutputBytes: 64,
	})
	if res.Stdout == "" {
		t.Fatal("expected some stdout captured")
	}
	if !strings.Contains(res.Stdout, "[output truncated]") {
		t.Errorf("expected truncation marker, got: %q", res.Stdout)
	}
}

type recordingAuditor struct {
	mu      sync.Mutex
	entries []policy.AuditEntry
}

func (r *recordingAuditor) Record(e policy.AuditEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
}

func TestRun_RecordsAudit(t *testing.T) {
	root := t.TempDir()
	rec := &recordingAuditor{}
	policy.Run(context.Background(), policy.RunOptions{
		Argv:       []string{"echo", "x"},
		WorkingDir: root,
		Allow:      policy.Allowlist{"echo": {}},
		Capability: "format_code",
		WorkflowID: "wf-1",
		Auditor:    rec,
	})
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.Capability != "format_code" || e.WorkflowID != "wf-1" {
		t.Errorf("audit entry mismatch: %+v", e)
	}
	if e.ExitCode != 0 {
		t.Errorf("expected exit_code=0, got %d", e.ExitCode)
	}
}
