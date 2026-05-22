package mcpagent_test

import (
	"context"
	"testing"

	mcpregistry "github.com/go-orca/go-orca/internal/mcp/registry"
	"github.com/go-orca/go-orca/internal/persona/mcpagent"
	"github.com/go-orca/go-orca/internal/tools"
)

func TestRunUnknownServer(t *testing.T) {
	reg := tools.NewRegistry()
	mcpReg := mcpregistry.New(reg, nil)

	res, err := mcpagent.Run(context.Background(), mcpagent.Config{
		ProviderName: "ollama",
		ModelName:    "test",
	}, mcpReg, reg, mcpagent.Request{
		Server: "nonexistent",
		Task:   "do something",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatalf("expected failure, got %+v", res)
	}
}

func TestRegistryAdvertisedToolsUnknown(t *testing.T) {
	reg := tools.NewRegistry()
	mcpReg := mcpregistry.New(reg, nil)
	_, err := mcpReg.AdvertisedTools("missing")
	if err == nil {
		t.Fatal("expected error for missing server")
	}
}

func TestRegistryServerNamesEmpty(t *testing.T) {
	reg := tools.NewRegistry()
	mcpReg := mcpregistry.New(reg, nil)
	names := mcpReg.ServerNames()
	if len(names) != 0 {
		t.Fatalf("expected no connected servers, got %v", names)
	}
}
