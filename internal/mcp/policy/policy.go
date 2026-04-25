// Package policy provides the runtime policy primitives every first-party
// go-orca MCP server uses: workspace-confined path resolution, an explicit
// command allowlist, output truncation, per-call timeouts, environment
// scrubbing, and a structured audit trail.
//
// MCP servers do NOT expose unrestricted shell access.  They expose a small
// set of governed tools (run_tests, run_build, …) that internally call
// [Run] with a fixed argv.  The policy layer is what makes that safe.
package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
)

// DefaultTimeout caps any single command invocation when the caller does not
// provide one. Servers can lower this for fast capabilities.
const DefaultTimeout = 10 * time.Minute

// DefaultMaxOutputBytes is the default cap on captured stdout+stderr per call.
// Output beyond this is dropped and a "[output truncated]" marker is appended.
const DefaultMaxOutputBytes = 256 * 1024

// ErrNotAllowed is returned by [CheckCommand] when a command is not on the
// server's allowlist.
var ErrNotAllowed = errors.New("policy: command not allowlisted")

// ErrEscapesWorkspace is returned by [ResolveWorkspacePath] when the requested
// path would resolve outside the workspace root.
var ErrEscapesWorkspace = errors.New("policy: path escapes workspace")

// Allowlist is the set of permitted argv[0] values plus optional argument-prefix
// rules. The zero value allows nothing.
//
// Key = argv[0] (e.g. "go", "git").
// Value = a list of allowed argv[1] values.  Empty list means "any args".
type Allowlist map[string][]string

// CheckCommand returns nil if argv is permitted by the allowlist, otherwise
// [ErrNotAllowed].
func (a Allowlist) CheckCommand(argv []string) error {
	if len(argv) == 0 {
		return ErrNotAllowed
	}
	subs, ok := a[argv[0]]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotAllowed, argv[0])
	}
	if len(subs) == 0 {
		return nil
	}
	if len(argv) < 2 {
		return fmt.Errorf("%w: %q requires a subcommand", ErrNotAllowed, argv[0])
	}
	for _, s := range subs {
		if argv[1] == s {
			return nil
		}
	}
	return fmt.Errorf("%w: %q %q", ErrNotAllowed, argv[0], argv[1])
}

// ResolveWorkspacePath joins root and rel, then verifies the resulting absolute
// path is still under root.  rel may be empty (or "."), in which case root is
// returned.  Absolute or escaping `rel` values are rejected with
// [ErrEscapesWorkspace].
func ResolveWorkspacePath(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if root == "" || root == "." {
		return "", fmt.Errorf("policy: workspace root is empty")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("policy: workspace root must be absolute: %q", root)
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q", ErrEscapesWorkspace, rel)
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrEscapesWorkspace, rel)
	}
	if cleaned == "." || cleaned == "" {
		return root, nil
	}
	return filepath.Join(root, cleaned), nil
}

// FilterEnv returns a copy of env with only the keys listed in allow retained.
// Unset entries are skipped.  Use this to keep secrets out of subprocesses.
func FilterEnv(env []string, allow []string) []string {
	keep := make(map[string]struct{}, len(allow))
	for _, k := range allow {
		keep[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		if _, ok := keep[kv[:i]]; ok {
			out = append(out, kv)
		}
	}
	return out
}

// AuditEntry is a single record appended to the server's audit trail when a
// governed command runs.
type AuditEntry struct {
	Timestamp  time.Time     `json:"timestamp"`
	Capability string        `json:"capability,omitempty"`
	WorkflowID string        `json:"workflow_id,omitempty"`
	Argv       []string      `json:"argv"`
	WorkingDir string        `json:"working_dir"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration_ms"`
	Truncated  bool          `json:"truncated,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// Auditor receives audit entries.  Implementations should be non-blocking and
// safe for concurrent use.  The default [NopAuditor] discards entries.
type Auditor interface {
	Record(entry AuditEntry)
}

// NopAuditor is an [Auditor] that discards all entries.
type NopAuditor struct{}

// Record implements [Auditor].
func (NopAuditor) Record(AuditEntry) {}

// RunOptions configures a governed command execution.
type RunOptions struct {
	// Argv is the command to run, including argv[0].
	Argv []string
	// WorkingDir is the absolute path to run the command in.  Must already be
	// resolved through [ResolveWorkspacePath] by the caller.
	WorkingDir string
	// Timeout caps the wall-clock duration of the command.  Defaults to
	// [DefaultTimeout] when zero or negative.
	Timeout time.Duration
	// MaxOutputBytes caps the combined stdout+stderr captured.  Defaults to
	// [DefaultMaxOutputBytes] when zero or negative.
	MaxOutputBytes int
	// Env is the explicit environment passed to the subprocess.  When empty
	// the subprocess receives an empty environment (recommended).
	Env []string
	// Capability and WorkflowID are recorded in the audit entry.
	Capability string
	WorkflowID string
	// Allow is the allowlist Argv must satisfy.  Required.
	Allow Allowlist
	// Auditor receives the AuditEntry for this run.  When nil, [NopAuditor]
	// is used.
	Auditor Auditor
}

// Run executes a governed command.  The returned [capabilities.Result]
// captures success, stdout, stderr, output summary, error, and structured
// metadata (command, duration_ms, exit_code, truncated).
//
// Run never panics on policy violations; it returns a non-zero exit code with
// the violation in Result.Error.
func Run(ctx context.Context, opts RunOptions) capabilities.Result {
	auditor := opts.Auditor
	if auditor == nil {
		auditor = NopAuditor{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}

	if err := opts.Allow.CheckCommand(opts.Argv); err != nil {
		auditor.Record(AuditEntry{
			Timestamp: time.Now().UTC(), Capability: opts.Capability, WorkflowID: opts.WorkflowID,
			Argv: opts.Argv, WorkingDir: opts.WorkingDir, ExitCode: -1, Error: err.Error(),
		})
		return capabilities.Result{Passed: false, Success: false, Error: err.Error()}
	}

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, opts.Argv[0], opts.Argv[1:]...) //nolint:gosec // argv is allowlisted above
	cmd.Dir = opts.WorkingDir
	cmd.Env = opts.Env

	var stdoutBuf, stderrBuf cappedBuffer
	stdoutBuf.cap = opts.MaxOutputBytes
	stderrBuf.cap = opts.MaxOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	exitCode := 0
	errStr := ""
	if err != nil {
		exitCode = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
		errStr = err.Error()
	}
	truncated := stdoutBuf.truncated || stderrBuf.truncated

	auditor.Record(AuditEntry{
		Timestamp: start.UTC(), Capability: opts.Capability, WorkflowID: opts.WorkflowID,
		Argv: opts.Argv, WorkingDir: opts.WorkingDir, ExitCode: exitCode, Duration: dur,
		Truncated: truncated, Error: errStr,
	})

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()
	output := summarize(stdout, stderr)

	return capabilities.Result{
		Passed:  err == nil,
		Success: err == nil,
		Stdout:  stdout,
		Stderr:  stderr,
		Output:  output,
		Error:   errStr,
		Metadata: map[string]any{
			"command":     strings.Join(opts.Argv, " "),
			"duration_ms": dur.Milliseconds(),
			"exit_code":   exitCode,
			"truncated":   truncated,
		},
	}
}

func summarize(stdout, stderr string) string {
	if stderr != "" {
		return strings.TrimSpace(lastLines(stderr, 6))
	}
	return strings.TrimSpace(lastLines(stdout, 6))
}

func lastLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.cap <= 0 || c.buf.Len() >= c.cap {
		c.truncated = c.truncated || len(p) > 0
		return len(p), nil
	}
	remaining := c.cap - c.buf.Len()
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	c.truncated = true
	c.buf.Write(p[:remaining])
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	if c.truncated {
		return c.buf.String() + "\n[output truncated]"
	}
	return c.buf.String()
}
