package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// StreamingCapabilities describes how live workflow events are delivered.
type StreamingCapabilities struct {
	Enabled        bool   `json:"enabled"`
	WorkflowStream string `json:"workflow_stream"`
	WorkflowTopic  string `json:"workflow_topic,omitempty"`
}

// StreamingCapabilities returns UI-facing streaming transport metadata.
func GetStreamingCapabilities(enabled bool, topic string) gin.HandlerFunc {
	transport := "database"
	if enabled {
		transport = "redpanda"
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, StreamingCapabilities{
			Enabled:        enabled,
			WorkflowStream: transport,
			WorkflowTopic:  topic,
		})
	}
}
