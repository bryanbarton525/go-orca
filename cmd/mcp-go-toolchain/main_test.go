package main

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

func TestRegister_AdvertisesExpectedTools(t *testing.T) {
	srv := server.New(server.Options{Name: "test"})
	register(srv, "/tmp/ws", policy.Allowlist{
		"go": {"mod", "build", "test", "vet"}, "gofmt": {},
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
		"go_mod_init", "go_mod_tidy", "go_fmt",
		"go_test", "go_build", "go_vet",
	}
	for _, name := range expected {
		if _, ok := advertised[name]; !ok {
			t.Errorf("tool %q not advertised", name)
		}
	}
}
