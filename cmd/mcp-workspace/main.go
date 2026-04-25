// Command mcp-workspace is the first-party go-orca MCP server that owns
// workflow workspace lifecycle: creating per-workflow subdirectories under
// MCP_WORKSPACE_ROOT, cloning repositories, initialising empty git repos,
// reporting workspace metadata, and destroying workspaces on demand.
//
// Long-term it is intended to replace engine-managed workspace metadata in
// go-orca-api; for now it exists as a deployable binary that exercises the
// same shared framework + policy + capability layer as the language
// toolchain MCP servers.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

func main() {
	listen := flag.String("listen", envOr("MCP_LISTEN", ":3000"), "address to listen on")
	workspaceRoot := flag.String("workspace-root", envOr("MCP_WORKSPACE_ROOT", "/var/lib/go-orca/workspaces"), "absolute workspace root")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	allow := policy.Allowlist{
		"git": {"clone", "init", "config", "rev-parse", "symbolic-ref"},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-workspace", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-workspace starting",
		zap.String("listen", *listen),
		zap.String("workspace_root", *workspaceRoot),
	)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

// ─── Argument and result types ───────────────────────────────────────────────

type createArgs struct {
	WorkflowID string `json:"workflow_id"`
}

type createResult struct {
	WorkflowID string `json:"workflow_id"`
	Path       string `json:"path"`           // absolute path on disk
	RelPath    string `json:"workspace_path"` // path relative to workspace root
	Created    bool   `json:"created"`
}

type cloneArgs struct {
	WorkflowID string `json:"workflow_id"`
	RepoURL    string `json:"repo_url"`
	Branch     string `json:"branch,omitempty"`
}

type initArgs struct {
	WorkflowID string `json:"workflow_id"`
	Branch     string `json:"branch,omitempty"`
}

type infoArgs struct {
	WorkflowID string `json:"workflow_id"`
}

type infoResult struct {
	WorkflowID string `json:"workflow_id"`
	Exists     bool   `json:"exists"`
	Path       string `json:"path,omitempty"`
	IsGitRepo  bool   `json:"is_git_repo"`
	Branch     string `json:"branch,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
}

type destroyArgs struct {
	WorkflowID string `json:"workflow_id"`
}

type destroyResult struct {
	WorkflowID string `json:"workflow_id"`
	Destroyed  bool   `json:"destroyed"`
}

// register wires the five workspace lifecycle tools onto srv.
func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor, logger *zap.Logger) {
	mcp := srv.MCPServer()

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_create", Description: "Create a per-workflow workspace directory.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a createArgs) (*sdkmcp.CallToolResult, any, error) {
		if err := validateWorkflowID(a.WorkflowID); err != nil {
			return nil, nil, err
		}
		abs, err := policy.ResolveWorkspacePath(root, a.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		created := false
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return nil, nil, err
			}
			created = true
		}
		logger.Info("workspace_create", zap.String("workflow_id", a.WorkflowID), zap.String("path", abs))
		return jsonContent(createResult{
			WorkflowID: a.WorkflowID, Path: abs, RelPath: a.WorkflowID, Created: created,
		})
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_clone", Description: "Clone a git repository into the workflow workspace.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a cloneArgs) (*sdkmcp.CallToolResult, any, error) {
		if err := validateWorkflowID(a.WorkflowID); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(a.RepoURL) == "" {
			return nil, nil, fmt.Errorf("repo_url required")
		}
		abs, err := policy.ResolveWorkspacePath(root, a.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		if entries, _ := os.ReadDir(abs); len(entries) > 0 {
			return nil, nil, fmt.Errorf("workspace %q is not empty; refusing to clone over it", a.WorkflowID)
		}
		argv := []string{"git", "clone"}
		if strings.TrimSpace(a.Branch) != "" {
			argv = append(argv, "--branch", a.Branch)
		}
		argv = append(argv, a.RepoURL, abs)

		// Clone runs from the workspace root (not abs, which is the target).
		res := policy.Run(ctx, policy.RunOptions{
			Argv:       argv,
			WorkingDir: root,
			Timeout:    10 * time.Minute,
			Capability: "workspace_clone",
			WorkflowID: a.WorkflowID,
			Allow:      allow,
			Auditor:    auditor,
			Env:        gitEnv(),
		})
		if !res.Success {
			return nil, nil, fmt.Errorf("git clone: %s", firstErr(res))
		}
		return jsonContent(createResult{
			WorkflowID: a.WorkflowID, Path: abs, RelPath: a.WorkflowID, Created: true,
		})
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_init", Description: "Initialise an empty git repo in the workspace.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a initArgs) (*sdkmcp.CallToolResult, any, error) {
		if err := validateWorkflowID(a.WorkflowID); err != nil {
			return nil, nil, err
		}
		abs, err := policy.ResolveWorkspacePath(root, a.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return nil, nil, err
		}
		argv := []string{"git", "init"}
		if strings.TrimSpace(a.Branch) != "" {
			argv = append(argv, "--initial-branch", a.Branch)
		}
		res := policy.Run(ctx, policy.RunOptions{
			Argv:       argv,
			WorkingDir: abs,
			Timeout:    30 * time.Second,
			Capability: "workspace_init",
			WorkflowID: a.WorkflowID,
			Allow:      allow,
			Auditor:    auditor,
			Env:        gitEnv(),
		})
		if !res.Success {
			return nil, nil, fmt.Errorf("git init: %s", firstErr(res))
		}
		return jsonContent(createResult{
			WorkflowID: a.WorkflowID, Path: abs, RelPath: a.WorkflowID, Created: true,
		})
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_info", Description: "Report workspace existence, branch, and head SHA.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a infoArgs) (*sdkmcp.CallToolResult, any, error) {
		if err := validateWorkflowID(a.WorkflowID); err != nil {
			return nil, nil, err
		}
		abs, err := policy.ResolveWorkspacePath(root, a.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		out := infoResult{WorkflowID: a.WorkflowID}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return jsonContent(out)
		}
		out.Exists = true
		out.Path = abs
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			out.IsGitRepo = true
			// rev-parse --abbrev-ref HEAD fails on repos with no commits; fall
			// back to symbolic-ref for the configured initial branch.
			branch := strings.TrimSpace(runQuiet(ctx, abs, allow, auditor, a.WorkflowID, "git", "rev-parse", "--abbrev-ref", "HEAD"))
			if branch == "" || branch == "HEAD" {
				branch = strings.TrimSpace(runQuiet(ctx, abs, allow, auditor, a.WorkflowID, "git", "symbolic-ref", "--short", "HEAD"))
			}
			out.Branch = branch
			out.HeadSHA = strings.TrimSpace(runQuiet(ctx, abs, allow, auditor, a.WorkflowID, "git", "rev-parse", "HEAD"))
		}
		return jsonContent(out)
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_destroy", Description: "Remove a workflow workspace from disk.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a destroyArgs) (*sdkmcp.CallToolResult, any, error) {
		if err := validateWorkflowID(a.WorkflowID); err != nil {
			return nil, nil, err
		}
		abs, err := policy.ResolveWorkspacePath(root, a.WorkflowID)
		if err != nil {
			return nil, nil, err
		}
		if abs == root {
			return nil, nil, fmt.Errorf("refusing to destroy workspace root")
		}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return jsonContent(destroyResult{WorkflowID: a.WorkflowID, Destroyed: false})
		}
		if err := os.RemoveAll(abs); err != nil {
			return nil, nil, err
		}
		logger.Info("workspace_destroy", zap.String("workflow_id", a.WorkflowID), zap.String("path", abs))
		return jsonContent(destroyResult{WorkflowID: a.WorkflowID, Destroyed: true})
	})
}

func runQuiet(ctx context.Context, dir string, allow policy.Allowlist, auditor policy.Auditor, workflowID string, argv ...string) string {
	r := policy.Run(ctx, policy.RunOptions{
		Argv:       argv,
		WorkingDir: dir,
		Timeout:    5 * time.Second,
		Capability: capabilities.GitStatus,
		WorkflowID: workflowID,
		Allow:      allow,
		Auditor:    auditor,
		Env:        gitEnv(),
	})
	if !r.Success {
		return ""
	}
	return r.Stdout
}

// validateWorkflowID rejects workflow IDs that contain path separators or
// other characters that could be used to escape the workspace root once
// joined.
func validateWorkflowID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("workflow_id required")
	}
	if strings.ContainsAny(id, "/\\") || strings.HasPrefix(id, ".") {
		return fmt.Errorf("workflow_id contains invalid characters")
	}
	return nil
}

func firstErr(r capabilities.Result) string {
	if r.Error != "" {
		return r.Error
	}
	if r.Stderr != "" {
		return r.Stderr
	}
	return r.Stdout
}

func gitEnv() []string {
	keep := []string{"PATH", "HOME", "SSH_AUTH_SOCK", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL"}
	return policy.FilterEnv(os.Environ(), keep)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func jsonContent(v any) (*sdkmcp.CallToolResult, any, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
	}, nil, nil
}

type zapAuditor struct{ logger *zap.Logger }

func (a *zapAuditor) Record(entry policy.AuditEntry) {
	a.logger.Info("policy audit",
		zap.String("capability", entry.Capability), zap.String("workflow_id", entry.WorkflowID),
		zap.Strings("argv", entry.Argv), zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()))
}
