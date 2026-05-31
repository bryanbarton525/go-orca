package orcabridge

import (
	"github.com/go-orca/go-orca/internal/mcp/guidance"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// RegisterGuidance adds prompts to the bridge MCP server.
func RegisterGuidance(s *server.Server) {
	guidance.RegisterBridge(s)
}
