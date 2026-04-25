// Command mcp-git is the first-party go-orca MCP server that exposes the
// governed git capabilities used by the workflow engine: git_status,
// git_checkpoint, and git_push_checkpoint.
//
// Operations are confined to MCP_WORKSPACE_ROOT and the only allowlisted
// binary is `git` with a fixed list of subcommands.  No interactive prompts,
// no remote fetches except for the explicit push during git_push_checkpoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

func main() {
	listen := flag.String("listen", envOr("MCP_LISTEN", ":3000"), "address to listen on")
	workspaceRoot := flag.String("workspace-root", envOr("MCP_WORKSPACE_ROOT", "/var/lib/go-orca/workspaces"), "absolute workspace root")
	authorEmail := flag.String("author-email", envOr("MCP_GIT_AUTHOR_EMAIL", "checkpoint@go-orca.local"), "author email used for checkpoint commits")
	authorName := flag.String("author-name", envOr("MCP_GIT_AUTHOR_NAME", "go-orca"), "author name used for checkpoint commits")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	allow := policy.Allowlist{
		"git": {"init", "config", "add", "commit", "status", "rev-parse", "push", "symbolic-ref", "checkout", "remote"},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name:    "mcp-git",
		Version: "0.1.0",
		Listen:  *listen,
		Logger:  logger,
	})

	register(srv, *workspaceRoot, *authorEmail, *authorName, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-git starting",
		zap.String("listen", *listen),
		zap.String("workspace_root", *workspaceRoot),
	)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

// register wires the three capability tools onto srv.
func register(srv *server.Server, root, authorEmail, authorName string, allow policy.Allowlist, auditor policy.Auditor) {
	server.AddCapability(srv, "git_status", "Run `git status --porcelain` in the workspace.",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			workdir, err := policy.ResolveWorkspacePath(root, args.WorkspacePath)
			if err != nil {
				return capabilities.Result{Error: err.Error()}
			}
			return policy.Run(ctx, policy.RunOptions{
				Argv:       []string{"git", "status", "--porcelain"},
				WorkingDir: workdir,
				Timeout:    30 * time.Second,
				Capability: capabilities.GitStatus,
				WorkflowID: args.WorkflowID,
				Allow:      allow,
				Auditor:    auditor,
				Env:        gitEnv(),
			})
		})

	server.AddCheckpointCapability(srv, "git_checkpoint", "Stage all changes and create a checkpoint commit.",
		func(ctx context.Context, args capabilities.Args) (capabilities.CheckpointResult, error) {
			return doCheckpoint(ctx, root, args, false, authorEmail, authorName, allow, auditor)
		})

	server.AddCheckpointCapability(srv, "git_push_checkpoint", "Stage, commit, and push the checkpoint to origin.",
		func(ctx context.Context, args capabilities.Args) (capabilities.CheckpointResult, error) {
			args.Push = true
			return doCheckpoint(ctx, root, args, true, authorEmail, authorName, allow, auditor)
		})
}

// doCheckpoint implements both git_checkpoint and git_push_checkpoint.
//
//   - Initialises the repo if .git is missing (with the configured author).
//   - Stages everything, commits with a deterministic message, and reports
//     the resulting SHA + branch.  An empty workspace produces an empty
//     CheckpointResult (no SHA, Pushed=false) rather than an error.
//   - When push=true, runs `git push origin <branch>` and reports Pushed
//     accordingly; a push failure is returned as an error so the engine can
//     surface it.
func doCheckpoint(ctx context.Context, root string, args capabilities.Args, push bool, authorEmail, authorName string, allow policy.Allowlist, auditor policy.Auditor) (capabilities.CheckpointResult, error) {
	workdir, err := policy.ResolveWorkspacePath(root, args.WorkspacePath)
	if err != nil {
		return capabilities.CheckpointResult{}, err
	}

	cap := capabilities.GitCheckpoint
	if push {
		cap = capabilities.GitPushCheckpoint
	}
	askpassPath := ""

	run := func(timeout time.Duration, argv ...string) capabilities.Result {
		env := gitEnv()
		if askpassPath != "" {
			env = append(env,
				"GIT_ASKPASS="+askpassPath,
				"GIT_TERMINAL_PROMPT=0",
				"GOORCA_GITHUB_TOKEN="+os.Getenv("GOORCA_GITHUB_TOKEN"),
			)
		}
		return policy.Run(ctx, policy.RunOptions{
			Argv:       argv,
			WorkingDir: workdir,
			Timeout:    timeout,
			Capability: cap,
			WorkflowID: args.WorkflowID,
			Allow:      allow,
			Auditor:    auditor,
			Env:        env,
		})
	}

	// Initialise repo if absent.
	if _, statErr := os.Stat(filepath.Join(workdir, ".git")); os.IsNotExist(statErr) {
		if r := run(15*time.Second, "git", "init"); !r.Success {
			return capabilities.CheckpointResult{}, fmt.Errorf("git init: %s", firstErr(r))
		}
		_ = run(5*time.Second, "git", "config", "user.email", authorEmail)
		_ = run(5*time.Second, "git", "config", "user.name", authorName)
	}
	if targetBranch := strings.TrimSpace(args.Branch); targetBranch != "" {
		if r := run(15*time.Second, "git", "checkout", "-B", targetBranch); !r.Success {
			return capabilities.CheckpointResult{}, fmt.Errorf("git checkout: %s", firstErr(r))
		}
	}
	if repoURL := strings.TrimSpace(args.RepoURL); repoURL != "" {
		if r := run(5*time.Second, "git", "remote", "get-url", "origin"); r.Success {
			if strings.TrimSpace(r.Stdout) != repoURL {
				if sr := run(5*time.Second, "git", "remote", "set-url", "origin", repoURL); !sr.Success {
					return capabilities.CheckpointResult{}, fmt.Errorf("git remote set-url: %s", firstErr(sr))
				}
			}
		} else if ar := run(5*time.Second, "git", "remote", "add", "origin", repoURL); !ar.Success {
			return capabilities.CheckpointResult{}, fmt.Errorf("git remote add: %s", firstErr(ar))
		}
	}

	// Stage all.
	if r := run(60*time.Second, "git", "add", "-A"); !r.Success {
		return capabilities.CheckpointResult{}, fmt.Errorf("git add: %s", firstErr(r))
	}

	// Commit.  An empty staging area is not an error — we report a no-op
	// CheckpointResult so the engine can record "nothing to checkpoint".
	message := strings.TrimSpace(args.Phase)
	if message == "" {
		message = "checkpoint"
	}
	if args.WorkflowID != "" {
		message = fmt.Sprintf("checkpoint %s (workflow %s)", message, args.WorkflowID)
	} else {
		message = "checkpoint " + message
	}
	commit := run(60*time.Second, "git", "commit", "-m", message)
	nothingToCommit := !commit.Success && (strings.Contains(commit.Stdout, "nothing to commit") ||
		strings.Contains(commit.Stderr, "nothing to commit") ||
		strings.Contains(commit.Stdout, "no changes added to commit"))
	if !commit.Success && !nothingToCommit {
		return capabilities.CheckpointResult{}, fmt.Errorf("git commit: %s", firstErr(commit))
	}

	// Resolve current SHA + branch (may be empty if the very first commit
	// failed because the workspace was empty).
	sha := strings.TrimSpace(run(5*time.Second, "git", "rev-parse", "HEAD").Stdout)
	branch := strings.TrimSpace(run(5*time.Second, "git", "rev-parse", "--abbrev-ref", "HEAD").Stdout)
	if branch == "HEAD" {
		// Detached or pre-first-commit; fall back to symbolic-ref.
		alt := strings.TrimSpace(run(5*time.Second, "git", "symbolic-ref", "--short", "HEAD").Stdout)
		if alt != "" {
			branch = alt
		}
	}

	res := capabilities.CheckpointResult{
		CommitSHA: sha,
		Branch:    branch,
		Message:   message,
	}

	if push && sha != "" {
		if os.Getenv("GOORCA_GITHUB_TOKEN") != "" {
			var askErr error
			askpassPath, askErr = ensureGitHubAskPass(workdir)
			if askErr != nil {
				return res, fmt.Errorf("git push auth: %w", askErr)
			}
		}
		// Effective branch fallback: explicit Branch from args wins.
		target := strings.TrimSpace(args.Branch)
		if target == "" {
			target = branch
		}
		if target == "" {
			return res, fmt.Errorf("git push: no branch to push")
		}
		pr := run(2*time.Minute, "git", "push", "origin", target)
		if !pr.Success {
			return res, fmt.Errorf("git push: %s", firstErr(pr))
		}
		res.Pushed = true
	}
	return res, nil
}

func ensureGitHubAskPass(workdir string) (string, error) {
	path := filepath.Join(workdir, ".git", "go-orca-askpass.sh")
	script := `#!/bin/sh
case "$1" in
  *Username*) printf '%s\n' 'x-access-token' ;;
  *) printf '%s\n' "$GOORCA_GITHUB_TOKEN" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", err
	}
	return path, nil
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

// gitEnv returns the minimal environment git needs.  Push auth is added only
// for git_push_checkpoint calls after an askpass helper has been prepared.
func gitEnv() []string {
	keep := []string{"PATH", "HOME", "SSH_AUTH_SOCK", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"}
	return policy.FilterEnv(os.Environ(), keep)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type zapAuditor struct{ logger *zap.Logger }

func (a *zapAuditor) Record(entry policy.AuditEntry) {
	a.logger.Info("policy audit",
		zap.String("capability", entry.Capability),
		zap.String("workflow_id", entry.WorkflowID),
		zap.Strings("argv", entry.Argv),
		zap.String("workdir", entry.WorkingDir),
		zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()),
	)
}
