package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// PersonaTaskRunBody is a single task target for orca_task_run.
type PersonaTaskRunBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Specialty   string `json:"specialty"`
	Tier        string `json:"tier"`
}

// CreatePersonaRunRequest is the body for POST /persona-runs.
type CreatePersonaRunRequest struct {
	Persona       string              `json:"persona" binding:"required"`
	Request       string              `json:"request"`
	Mode          string              `json:"mode"`
	Provider      string              `json:"provider"`
	Model         string              `json:"model"`
	WorkflowID    string              `json:"workflow_id"`
	Persist       bool                `json:"persist"`
	ToolsScope    string              `json:"tools_scope"`
	TimeoutSec    int                 `json:"timeout_sec"`
	WorkspacePath string              `json:"workspace_path"`
	TaskRun       *PersonaTaskRunBody `json:"task_run"`
}

// CreatePersonaRun handles POST /persona-runs (ad-hoc persona / MCP offload).
func CreatePersonaRun(eng *engine.Engine, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if eng == nil {
			respondError(c, http.StatusServiceUnavailable, "workflow engine not configured")
			return
		}
		var req CreatePersonaRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		tid := tenantID(c)
		sid := scopeID(c)
		if tid == "" || sid == "" {
			respondError(c, http.StatusBadRequest, "X-Tenant-ID and X-Scope-ID headers are required")
			return
		}

		timeout := time.Duration(req.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}

		runReq := engine.PersonaRunRequest{
			TenantID:      tid,
			ScopeID:       sid,
			Persona:       state.PersonaKind(strings.ToLower(strings.TrimSpace(req.Persona))),
			Request:       strings.TrimSpace(req.Request),
			Mode:          normalizeWorkflowMode(req.Mode),
			Provider:      strings.TrimSpace(req.Provider),
			Model:         strings.TrimSpace(req.Model),
			WorkflowID:    strings.TrimSpace(req.WorkflowID),
			Persist:       req.Persist,
			ToolsScope:    strings.TrimSpace(req.ToolsScope),
			Timeout:       timeout,
			WorkspacePath: strings.TrimSpace(req.WorkspacePath),
		}
		if req.TaskRun != nil {
			runReq.TaskRun = &engine.PersonaTaskRunInput{
				Title:       req.TaskRun.Title,
				Description: req.TaskRun.Description,
				Specialty:   req.TaskRun.Specialty,
				Tier:        state.TaskTier(strings.TrimSpace(req.TaskRun.Tier)),
			}
		}

		resp, err := eng.RunPersonaOnce(c.Request.Context(), runReq)
		if err != nil {
			log.Warn("persona run failed", zap.Error(err))
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}
