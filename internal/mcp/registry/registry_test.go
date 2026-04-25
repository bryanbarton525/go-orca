package registry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/tools"
	mcpclient "github.com/go-orca/go-orca/internal/tools/mcp"
)

// startInMemoryServer starts an MCP server with the given tools over an
// in-memory transport pair and registers the client side into reg.  It
// returns the connected session for cleanup.
func startInMemoryServer(t *testing.T, ctx context.Context, toolReg *tools.Registry, register func(*sdkmcp.Server)) *sdkmcp.ClientSession {
	t.Helper()
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	register(srv)

	clientT, serverT := sdkmcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(ctx, serverT)
	}()
	session, err := mcpclient.LoadTransport(ctx, toolReg, clientT)
	if err != nil {
		t.Fatalf("load transport: %v", err)
	}
	return session
}

// installInMemoryServer is a test-only helper that simulates LoadServers'
// effect: registers a server entry with a known set of advertised tool names.
func (r *Registry) installInMemoryServer(name string, advertised []string, connected bool, required bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := &serverEntry{
		cfg:        config.MCPServerConfig{Name: name, Required: required},
		advertised: make(map[string]struct{}, len(advertised)),
		connected:  connected,
	}
	for _, t := range advertised {
		entry.advertised[t] = struct{}{}
	}
	r.servers[name] = entry
}

func TestResolve_HappyPath(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test", "go_build"}, true, false)
	r.LoadToolchains([]Toolchain{{
		ID:                   "go",
		MCPServer:            "go-toolchain",
		Capabilities:         []string{"run_tests", "run_build"},
		CapabilityTools:      map[string]string{"run_tests": "go_test", "run_build": "go_build"},
		ValidationProfiles:   map[string][]string{"default": {"run_tests", "run_build"}},
		CheckpointCapability: "",
	}})

	tool, err := r.Resolve("go", "run_tests")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tool != "go_test" {
		t.Errorf("got %q want go_test", tool)
	}
}

func TestResolve_UnknownToolchain(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	if _, err := r.Resolve("python", "run_tests"); err == nil {
		t.Fatal("expected ResolveError for unknown toolchain")
	}
}

func TestResolve_ServerUnreachable(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test"}, false, false)
	r.LoadToolchains([]Toolchain{{
		ID:              "go",
		MCPServer:       "go-toolchain",
		Capabilities:    []string{"run_tests"},
		CapabilityTools: map[string]string{"run_tests": "go_test"},
	}})

	_, err := r.Resolve("go", "run_tests")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error %q should mention unreachable", err.Error())
	}
}

func TestResolve_ToolNotAdvertised(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test"}, true, false)
	r.LoadToolchains([]Toolchain{{
		ID:              "go",
		MCPServer:       "go-toolchain",
		Capabilities:    []string{"run_lint"},
		CapabilityTools: map[string]string{"run_lint": "go_vet"},
	}})

	_, err := r.Resolve("go", "run_lint")
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
	if !strings.Contains(err.Error(), "not advertised") {
		t.Errorf("error %q should mention not advertised", err.Error())
	}
}

func TestToolchainReachable(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test"}, true, true)
	r.installInMemoryServer("offline", nil, false, true)
	r.installInMemoryServer("optional-down", nil, false, false)

	r.LoadToolchains([]Toolchain{
		{ID: "go", MCPServer: "go-toolchain", Capabilities: []string{"run_tests"}, CapabilityTools: map[string]string{"run_tests": "go_test"}},
		{ID: "down", MCPServer: "offline"},
		{ID: "soft-down", MCPServer: "optional-down"},
	})

	if err := r.ToolchainReachable("go"); err != nil {
		t.Errorf("expected go toolchain reachable, got %v", err)
	}
	if err := r.ToolchainReachable("down"); err == nil {
		t.Error("expected error for required-but-disconnected toolchain")
	}
	if err := r.ToolchainReachable("soft-down"); err != nil {
		t.Errorf("optional/non-required server should not block start: %v", err)
	}
	if err := r.ToolchainReachable("missing"); err == nil {
		t.Error("expected error for unknown toolchain")
	}
}

func TestRequiredServersReachable(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", nil, false, true)
	if err := r.RequiredServersReachable(); err == nil {
		t.Fatal("expected error when required server unreachable")
	}

	r.installInMemoryServer("git", nil, true, true)
	r2 := New(tools.NewRegistry(), nil)
	r2.installInMemoryServer("git", nil, true, true)
	if err := r2.RequiredServersReachable(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSnapshot_Shape(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test", "go_build"}, true, false)
	r.LoadToolchains([]Toolchain{{
		ID: "go", MCPServer: "go-toolchain",
		Capabilities:    []string{"run_tests", "run_lint"},
		CapabilityTools: map[string]string{"run_tests": "go_test", "run_lint": "go_vet"},
	}})

	snap := r.SnapshotJSON()
	if len(snap.Servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(snap.Servers))
	}
	if len(snap.Toolchains) != 1 {
		t.Fatalf("want 1 toolchain, got %d", len(snap.Toolchains))
	}
	tc := snap.Toolchains[0]
	if !tc.ServerReachable {
		t.Error("expected ServerReachable=true")
	}
	if len(tc.MissingCapabilities) != 1 || tc.MissingCapabilities[0] != "run_lint" {
		t.Errorf("expected MissingCapabilities=[run_lint], got %v", tc.MissingCapabilities)
	}
}

func TestCallCapability_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	toolReg := tools.NewRegistry()

	session := startInMemoryServer(t, ctx, toolReg, func(s *sdkmcp.Server) {
		type args struct {
			Phase string `json:"phase"`
		}
		sdkmcp.AddTool(s, &sdkmcp.Tool{Name: "go_test", Description: "test"},
			func(ctx context.Context, req *sdkmcp.CallToolRequest, a args) (*sdkmcp.CallToolResult, any, error) {
				body, _ := json.Marshal(map[string]any{
					"passed": true, "success": true, "stdout": "ok phase=" + a.Phase,
				})
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
				}, nil, nil
			})
	})
	defer session.Close()

	r := New(toolReg, nil)
	r.installInMemoryServer("go-toolchain", []string{"go_test"}, true, false)
	r.LoadToolchains([]Toolchain{{
		ID: "go", MCPServer: "go-toolchain",
		Capabilities:    []string{"run_tests"},
		CapabilityTools: map[string]string{"run_tests": "go_test"},
	}})

	args, _ := json.Marshal(map[string]string{"phase": "implementation"})
	cr, err := r.CallCapability(ctx, "go", "run_tests", args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !cr.Passed {
		t.Errorf("expected Passed=true, got %+v", cr)
	}
	if !strings.Contains(cr.Stdout, "phase=implementation") {
		t.Errorf("stdout=%q does not echo phase", cr.Stdout)
	}
}

func TestCallCapability_ResolveError(t *testing.T) {
	r := New(tools.NewRegistry(), nil)
	_, err := r.CallCapability(context.Background(), "go", "run_tests", nil)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	var rerr *ResolveError
	if !errorsAs(err, &rerr) {
		t.Errorf("expected *ResolveError, got %T", err)
	}
}

// errorsAs is a tiny inline shim so the test file doesn't need to import
// "errors" just for one function.
func errorsAs(err error, target any) bool {
	type aser interface{ As(any) bool }
	_ = aser(nil)
	return errorsAsImpl(err, target)
}

func errorsAsImpl(err error, target any) bool {
	if rerr, ok := err.(*ResolveError); ok {
		if t, ok := target.(**ResolveError); ok {
			*t = rerr
			return true
		}
	}
	return false
}
