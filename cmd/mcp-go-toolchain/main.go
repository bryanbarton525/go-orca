// Command mcp-go-toolchain is the first-party go-orca MCP server that exposes
// governed Go-language toolchain capabilities (init_project, tidy_dependencies,
// format_code, run_tests, run_build, run_lint).
//
// It binds workspace operations to a single root (MCP_WORKSPACE_ROOT) and
// allows only a small allowlist of `go` subcommands plus `gofmt`.  Every
// invocation passes through the policy package so timeouts, output caps,
// allowlists, and the audit trail are enforced consistently.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
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
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	allow := policy.Allowlist{
		"go":    {"mod", "build", "test", "vet"},
		"gofmt": {},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name:    "mcp-go-toolchain",
		Version: "0.1.0",
		Listen:  *listen,
		Logger:  logger,
	})

	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-go-toolchain starting",
		zap.String("listen", *listen),
		zap.String("workspace_root", *workspaceRoot),
	)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	runCmd := func(capability string, argv []string, timeout time.Duration) server.CapabilityHandler {
		return func(ctx context.Context, args capabilities.Args) capabilities.Result {
			workdir, err := policy.ResolveWorkspacePath(root, args.WorkspacePath)
			if err != nil {
				return capabilities.Result{Error: err.Error()}
			}
			return policy.Run(ctx, policy.RunOptions{
				Argv:       argv,
				WorkingDir: workdir,
				Timeout:    timeout,
				Capability: capability,
				WorkflowID: args.WorkflowID,
				Allow:      allow,
				Auditor:    auditor,
				Env:        baseEnv(),
			})
		}
	}

	server.AddCapability(srv, "go_mod_init", "Initialise a new Go module in the workspace.",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			workdir, err := policy.ResolveWorkspacePath(root, args.WorkspacePath)
			if err != nil {
				return capabilities.Result{Error: err.Error()}
			}
			modName := args.Branch // crude default; the engine doesn't model module names directly yet
			if modName == "" {
				modName = "workspace"
			}
			return policy.Run(ctx, policy.RunOptions{
				Argv:       []string{"go", "mod", "init", modName},
				WorkingDir: workdir,
				Timeout:    30 * time.Second,
				Capability: capabilities.InitProject,
				WorkflowID: args.WorkflowID,
				Allow:      allow,
				Auditor:    auditor,
				Env:        baseEnv(),
			})
		})

	server.AddCapability(srv, "go_mod_tidy", "Run `go mod tidy` in the workspace.",
		runCmd(capabilities.TidyDependencies, []string{"go", "mod", "tidy"}, 5*time.Minute))

	server.AddCapability(srv, "go_fmt", "Format all Go files via `gofmt -w .`.",
		runCmd(capabilities.FormatCode, []string{"gofmt", "-w", "."}, 60*time.Second))

	server.AddCapability(srv, "go_test", "Run `go test ./...` in the workspace.",
		runCmd(capabilities.RunTests, []string{"go", "test", "./..."}, 10*time.Minute))

	server.AddCapability(srv, "go_build", "Run `go build ./...` in the workspace.",
		runCmd(capabilities.RunBuild, []string{"go", "build", "./..."}, 5*time.Minute))

	server.AddCapability(srv, "go_vet", "Run `go vet ./...` in the workspace.",
		runCmd(capabilities.RunLint, []string{"go", "vet", "./..."}, 2*time.Minute))
}

// baseEnv returns the minimal env the Go toolchain needs.  PATH is required
// to locate `go` and `gofmt`; HOME is required by the build cache; GOCACHE /
// GOMODCACHE are forwarded if set.  No secrets or arbitrary env are passed.
func baseEnv() []string {
	keep := []string{"PATH", "HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOFLAGS"}
	return policy.FilterEnv(os.Environ(), keep)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// zapAuditor logs each AuditEntry as a structured INFO log so operators can
// scrape the container logs for the audit trail.
type zapAuditor struct{ logger *zap.Logger }

func (a *zapAuditor) Record(entry policy.AuditEntry) {
	a.logger.Info("policy audit",
		zap.String("capability", entry.Capability),
		zap.String("workflow_id", entry.WorkflowID),
		zap.Strings("argv", entry.Argv),
		zap.String("workdir", entry.WorkingDir),
		zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()),
		zap.Bool("truncated", entry.Truncated),
		zap.String("error", entry.Error),
	)
	if entry.Error != "" {
		fmt.Fprintf(os.Stderr, "audit: %s argv=%v err=%s\n", entry.Capability, entry.Argv, entry.Error)
	}
}
