// Command mcp-rust-toolchain is the first-party go-orca MCP server that
// exposes governed Rust capabilities backed by cargo and rustfmt.
package main

import (
	"context"
	"flag"
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
		"cargo":   {"update", "fmt", "test", "build", "clippy", "check"},
		"rustfmt": {},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-rust-toolchain", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-rust-toolchain starting",
		zap.String("listen", *listen), zap.String("workspace_root", *workspaceRoot))
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	run := makeRunner(root, allow, auditor)

	server.AddCapability(srv, "cargo_update", "Run `cargo update --workspace`.",
		run(capabilities.TidyDependencies, []string{"cargo", "update", "--workspace"}, 5*time.Minute))
	server.AddCapability(srv, "cargo_fmt", "Run `cargo fmt --all`.",
		run(capabilities.FormatCode, []string{"cargo", "fmt", "--all"}, 60*time.Second))
	server.AddCapability(srv, "cargo_test", "Run `cargo test --all`.",
		run(capabilities.RunTests, []string{"cargo", "test", "--all"}, 20*time.Minute))
	server.AddCapability(srv, "cargo_build", "Run `cargo build --all`.",
		run(capabilities.RunBuild, []string{"cargo", "build", "--all"}, 15*time.Minute))
	server.AddCapability(srv, "cargo_clippy", "Run `cargo clippy --all -- -D warnings`.",
		run(capabilities.RunLint, []string{"cargo", "clippy", "--all", "--", "-D", "warnings"}, 10*time.Minute))
	server.AddCapability(srv, "cargo_check", "Run `cargo check --all`.",
		run(capabilities.Typecheck, []string{"cargo", "check", "--all"}, 5*time.Minute))
}

func makeRunner(root string, allow policy.Allowlist, auditor policy.Auditor) func(string, []string, time.Duration) server.CapabilityHandler {
	return func(capability string, argv []string, timeout time.Duration) server.CapabilityHandler {
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
}

func baseEnv() []string {
	keep := []string{"PATH", "HOME", "CARGO_HOME", "RUSTUP_HOME", "CARGO_TARGET_DIR"}
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
		zap.String("capability", entry.Capability), zap.String("workflow_id", entry.WorkflowID),
		zap.Strings("argv", entry.Argv), zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()))
}
