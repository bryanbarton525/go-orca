// Command mcp-nextjs-toolchain is the first-party go-orca MCP server that
// exposes governed Next.js capabilities: pnpm install, next build, next lint,
// tsc type-checking, prettier formatting, and test execution.
//
// Unlike the generic node toolchain, this server invokes Next.js tooling
// directly through pnpm scripts and npx so callers don't need to know which
// package manager or script name the project uses.
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
		"pnpm": {"install", "test", "run", "exec", "dlx"},
		"npx":  {"next", "tsc", "prettier", "eslint", "vitest", "jest"},
		"node": {},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-nextjs-toolchain", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-nextjs-toolchain starting",
		zap.String("listen", *listen), zap.String("workspace_root", *workspaceRoot))
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	run := makeRunner(root, allow, auditor)

	// Install — frozen lockfile ensures reproducible builds in CI.
	server.AddCapability(srv, "next_install",
		"Run `pnpm install --frozen-lockfile` in the Next.js workspace.",
		run(capabilities.InstallDependencies, []string{"pnpm", "install", "--frozen-lockfile"}, 10*time.Minute))

	// Build — invokes the project's build script which calls `next build`.
	// 15 min ceiling: large Next.js apps with many static pages can be slow.
	server.AddCapability(srv, "next_build",
		"Run `pnpm run build` (executes `next build` via project script).",
		run(capabilities.RunBuild, []string{"pnpm", "run", "build"}, 15*time.Minute))

	// Lint — invokes the project's lint script which calls `next lint`.
	// next lint wraps ESLint with Next.js-specific rules and config discovery.
	server.AddCapability(srv, "next_lint",
		"Run `pnpm run lint` (executes `next lint` via project script).",
		run(capabilities.RunLint, []string{"pnpm", "run", "lint"}, 5*time.Minute))

	// Type-check — `tsc --noEmit` is reliable even when the project uses
	// `next build` for transpilation, because tsconfig.json is always present.
	server.AddCapability(srv, "next_typecheck",
		"Run `npx tsc --noEmit` for full TypeScript type checking.",
		run(capabilities.Typecheck, []string{"npx", "tsc", "--noEmit"}, 10*time.Minute))

	// Format — prettier writes in-place; the workspace-path policy bounds it.
	server.AddCapability(srv, "next_format",
		"Run `npx prettier --write .` to format all source files.",
		run(capabilities.FormatCode, []string{"npx", "prettier", "--write", "."}, 5*time.Minute))

	// Test — covers both Jest and Vitest project setups since both surface
	// through the project's `test` script.
	server.AddCapability(srv, "next_test",
		"Run `pnpm test` (Jest or Vitest, depending on project setup).",
		run(capabilities.RunTests, []string{"pnpm", "test"}, 15*time.Minute))
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

// baseEnv keeps PATH, HOME, and the npm/pnpm cache vars needed for package
// resolution without leaking secrets from the host environment.
func baseEnv() []string {
	keep := []string{
		"PATH", "HOME",
		"NPM_CONFIG_CACHE", "PNPM_HOME",
		"NODE_ENV", "CI",
		// Next.js reads NEXT_TELEMETRY_DISABLED to suppress telemetry pings.
		"NEXT_TELEMETRY_DISABLED",
	}
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
		zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()))
}
