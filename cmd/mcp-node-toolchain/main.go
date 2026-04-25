// Command mcp-node-toolchain is the first-party go-orca MCP server that
// exposes governed Node.js / TypeScript capabilities backed by npm, pnpm,
// and a small set of CLI tools (prettier, eslint, tsc) invoked through npx.
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
		"npm":  {"ci", "install", "test", "run", "exec"},
		"pnpm": {"install", "test", "run", "exec"},
		"yarn": {"install", "test", "run"},
		"npx":  {"prettier", "eslint", "tsc"},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-node-toolchain", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-node-toolchain starting",
		zap.String("listen", *listen), zap.String("workspace_root", *workspaceRoot))
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	run := makeRunner(root, allow, auditor)

	server.AddCapability(srv, "npm_ci", "Run `npm ci` against the workspace lockfile.",
		run(capabilities.InstallDependencies, []string{"npm", "ci"}, 10*time.Minute))
	server.AddCapability(srv, "pnpm_install", "Run `pnpm install --frozen-lockfile`.",
		run(capabilities.InstallDependencies, []string{"pnpm", "install", "--frozen-lockfile"}, 10*time.Minute))
	server.AddCapability(srv, "prettier_format", "Run `npx prettier --write .`.",
		run(capabilities.FormatCode, []string{"npx", "prettier", "--write", "."}, 5*time.Minute))
	server.AddCapability(srv, "npm_test", "Run `npm test`.",
		run(capabilities.RunTests, []string{"npm", "test"}, 15*time.Minute))
	server.AddCapability(srv, "npm_build", "Run `npm run build`.",
		run(capabilities.RunBuild, []string{"npm", "run", "build"}, 10*time.Minute))
	server.AddCapability(srv, "npm_lint", "Run `npm run lint`.",
		run(capabilities.RunLint, []string{"npm", "run", "lint"}, 5*time.Minute))
	server.AddCapability(srv, "npm_typecheck", "Run `npm run typecheck` (defers to project script).",
		run(capabilities.Typecheck, []string{"npm", "run", "typecheck"}, 10*time.Minute))
	server.AddCapability(srv, "tsc_check", "Run `npx tsc --noEmit`.",
		run(capabilities.Typecheck, []string{"npx", "tsc", "--noEmit"}, 10*time.Minute))
}

// makeRunner returns a capability-handler factory bound to the workspace
// root, allowlist, and auditor.  Centralising this here avoids duplicating
// the resolve+run boilerplate across every capability.
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

// baseEnv keeps PATH, HOME, and the npm/pnpm/yarn cache vars that allow the
// package managers to find their on-disk caches without leaking secrets.
func baseEnv() []string {
	keep := []string{
		"PATH", "HOME",
		"NPM_CONFIG_CACHE", "PNPM_HOME", "YARN_CACHE_FOLDER",
		"NODE_ENV", "CI",
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
		zap.String("capability", entry.Capability), zap.String("workflow_id", entry.WorkflowID),
		zap.Strings("argv", entry.Argv), zap.Int("exit_code", entry.ExitCode),
		zap.Int64("duration_ms", entry.Duration.Milliseconds()))
}
