package main

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// TestRegister_AdvertisesExpectedTools is a smoke test: it verifies that the
// node toolchain server registers every capability tool referenced by the
// plan, so the registry's "tool not advertised" check will not trip on a
// correctly-configured toolchain entry.
func TestRegister_AdvertisesExpectedTools(t *testing.T) {
	srv := server.New(server.Options{Name: "test"})
	register(srv, "/tmp/ws", policy.Allowlist{
		"npm": {"ci", "install", "test", "run"}, "pnpm": {"install"}, "npx": {"prettier", "tsc"},
	}, policy.NopAuditor{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientT, serverT := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.MCPServer().Run(ctx, serverT) }()
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	session, err := c.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	advertised := make(map[string]struct{}, len(listed.Tools))
	for _, tool := range listed.Tools {
		advertised[tool.Name] = struct{}{}
	}
	expected := []string{
		"npm_ci", "pnpm_install", "prettier_format", "npm_test",
		"npm_build", "npm_lint", "npm_typecheck", "tsc_check",
	}
	for _, name := range expected {
		if _, ok := advertised[name]; !ok {
			t.Errorf("tool %q not advertised", name)
		}
	}
}
