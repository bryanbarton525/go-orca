package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/go-orca/go-orca/internal/workflow/templates"
)

// ListWorkflowTemplates handles GET /workflow-templates.
func ListWorkflowTemplates() gin.HandlerFunc {
	return func(c *gin.Context) {
		list := templates.List()
		c.JSON(http.StatusOK, gin.H{"templates": list})
	}
}

// GetWorkflowTemplate handles GET /workflow-templates/:id.
func GetWorkflowTemplate() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		t, ok := templates.Get(id)
		if !ok {
			respondError(c, http.StatusNotFound, "template not found")
			return
		}
		c.JSON(http.StatusOK, t)
	}
}
