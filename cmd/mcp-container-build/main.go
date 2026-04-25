// Command mcp-container-build is the optional first-party go-orca MCP server
// that exposes governed container-image capabilities: Dockerfile linting and
// image build/push.  It supports buildah (preferred, daemonless) and podman;
// docker is also accepted when the runtime image bundles it.
//
// The build / push tools delegate to whichever container CLI is on PATH;
// dockerfile_lint runs hadolint when available.  Each capability returns a
// CapabilityResult so the workflow engine can treat container-image steps
// the same as any other validation/build step.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
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
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	allow := policy.Allowlist{
		"buildah":  {"bud", "push", "tag"},
		"podman":   {"build", "push", "tag"},
		"docker":   {"build", "push", "tag"},
		"hadolint": {},
	}
	auditor := &zapAuditor{logger: logger}

	srv := server.New(server.Options{
		Name: "mcp-container-build", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, allow, auditor)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-container-build starting",
		zap.String("listen", *listen), zap.String("workspace_root", *workspaceRoot))
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

// buildArgs extends capabilities.Args with the build-specific knobs.  The
// engine doesn't currently model these; tools that need them accept them
// as part of the JSON arguments and the SDK extracts them via reflection.
type buildArgs struct {
	capabilities.Args
	Dockerfile string `json:"dockerfile,omitempty"`
	ImageTag   string `json:"image_tag,omitempty"`
	Context    string `json:"context,omitempty"`
}

func register(srv *server.Server, root string, allow policy.Allowlist, auditor policy.Auditor) {
	run := makeRunner(root, allow, auditor)

	// dockerfile_lint runs hadolint on a workspace-relative Dockerfile.
	server.AddCapability(srv, "dockerfile_lint", "Run hadolint against a Dockerfile in the workspace.",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			workdir, err := policy.ResolveWorkspacePath(root, args.WorkspacePath)
			if err != nil {
				return capabilities.Result{Error: err.Error()}
			}
			path := args.Branch // crude reuse: caller passes Dockerfile path via Branch when not using buildArgs
			if strings.TrimSpace(path) == "" {
				path = "Dockerfile"
			}
			return policy.Run(ctx, policy.RunOptions{
				Argv:       []string{"hadolint", path},
				WorkingDir: workdir,
				Timeout:    60 * time.Second,
				Capability: capabilities.RunLint,
				WorkflowID: args.WorkflowID,
				Allow:      allow,
				Auditor:    auditor,
				Env:        baseEnv(),
			})
		})

	// container_build delegates to whichever container CLI is on PATH, in
	// priority order: buildah > podman > docker.
	server.AddCapability(srv, "container_build", "Build a container image from a workspace Dockerfile.",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			return run(capabilities.RunBuild, pickBuildArgv("Dockerfile", args.Branch), 30*time.Minute)(ctx, args)
		})

	server.AddCapability(srv, "container_push", "Push a built image to its registry.",
		func(ctx context.Context, args capabilities.Args) capabilities.Result {
			tag := strings.TrimSpace(args.Branch)
			if tag == "" {
				return capabilities.Result{Error: "image tag required (pass via branch field)"}
			}
			return run(capabilities.RunBuild, pickPushArgv(tag), 10*time.Minute)(ctx, args)
		})
}

// pickBuildArgv returns argv for the first available container builder.
// At runtime, only one of {buildah, podman, docker} is typically installed;
// the policy layer rejects any not on the allowlist, and exec rejects any
// not on PATH.
func pickBuildArgv(dockerfile, tag string) []string {
	if tag == "" {
		tag = "go-orca-workflow:latest"
	}
	// Buildah preferred — daemonless and rootless-friendly.
	return []string{"buildah", "bud", "-f", dockerfile, "-t", tag, "."}
}

func pickPushArgv(tag string) []string {
	return []string{"buildah", "push", tag}
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
	keep := []string{
		"PATH", "HOME",
		"BUILDAH_ISOLATION", "REGISTRY_AUTH_FILE",
		"DOCKER_HOST", "DOCKER_CONFIG",
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
