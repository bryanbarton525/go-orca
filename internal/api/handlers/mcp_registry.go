package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	mcpregistry "github.com/go-orca/go-orca/internal/mcp/registry"
)

// GetMCPRegistry returns the live snapshot of the MCP server + toolchain
// registry: server health, advertised tools, and capability bindings.  The
// snapshot is a point-in-time view; servers that recover after this call are
// not reflected until the next periodic probe.
//
// Returns an empty snapshot when the registry is nil (deployments that have
// no MCP servers configured).
func GetMCPRegistry(reg *mcpregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reg == nil {
			c.JSON(http.StatusOK, mcpregistry.Snapshot{
				Servers:    []mcpregistry.ServerStatus{},
				Toolchains: []mcpregistry.ToolchainStatus{},
			})
			return
		}
		c.JSON(http.StatusOK, reg.SnapshotJSON())
	}
}
