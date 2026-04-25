// Command mcp-java-toolchain is the first-party go-orca MCP server that
// exposes governed Java capabilities backed by maven and gradle.
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
		"mvn":    {"install", "test", "package", "compile", "verify"},
		"gradle": {"assemble", "test", "build", "check"},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-java-toolchain", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-java-toolchain starting",
		zap.String("listen", *listen), zap.String("workspace_root", *workspaceRoot))
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	run := makeRunner(root, allow, auditor)

	server.AddCapability(srv, "mvn_install", "Run `mvn install -DskipTests`.",
		run(capabilities.InstallDependencies, []string{"mvn", "install", "-DskipTests"}, 20*time.Minute))
	server.AddCapability(srv, "mvn_test", "Run `mvn test`.",
		run(capabilities.RunTests, []string{"mvn", "test"}, 30*time.Minute))
	server.AddCapability(srv, "mvn_package", "Run `mvn package -DskipTests`.",
		run(capabilities.RunBuild, []string{"mvn", "package", "-DskipTests"}, 20*time.Minute))
	server.AddCapability(srv, "mvn_verify", "Run `mvn verify`.",
		run(capabilities.RunBuild, []string{"mvn", "verify"}, 30*time.Minute))
	server.AddCapability(srv, "gradle_assemble", "Run `gradle assemble`.",
		run(capabilities.InstallDependencies, []string{"gradle", "assemble"}, 20*time.Minute))
	server.AddCapability(srv, "gradle_test", "Run `gradle test`.",
		run(capabilities.RunTests, []string{"gradle", "test"}, 30*time.Minute))
	server.AddCapability(srv, "gradle_build", "Run `gradle build`.",
		run(capabilities.RunBuild, []string{"gradle", "build"}, 30*time.Minute))
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
	keep := []string{"PATH", "HOME", "JAVA_HOME", "MAVEN_OPTS", "GRADLE_USER_HOME", "M2_HOME"}
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
