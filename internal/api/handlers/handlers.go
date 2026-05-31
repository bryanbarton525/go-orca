// Package handlers provides Gin HTTP handler functions for the gorca API.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/scope"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
	"github.com/go-orca/go-orca/internal/workflow/scheduler"
	"github.com/go-orca/go-orca/internal/workflow/templates"
)

// ─── Shared helpers ───────────────────────────────────────────────────────────

func tenantID(c *gin.Context) string {
	id, _ := c.Get("tenant_id")
	if s, ok := id.(string); ok && s != "" {
		return s
	}
	return ""
}

func scopeID(c *gin.Context) string {
	id, _ := c.Get("scope_id")
	if s, ok := id.(string); ok && s != "" {
		return s
	}
	return ""
}

func respondError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

// checkWorkflowOwnership returns (ws, true) when the workflow belongs to the
// requesting tenant.  On mismatch or lookup failure it writes the appropriate
// HTTP response and returns (nil, false) so the caller can return immediately.
//
// Scope ownership is intentionally not enforced here: a tenant may access any
// workflow that belongs to them regardless of the scope it was created in.
func checkWorkflowOwnership(c *gin.Context, store storage.Store) (*state.WorkflowState, bool) {
	id := c.Param("id")
	ws, err := store.GetWorkflow(c.Request.Context(), id)
	if err != nil {
		respondError(c, http.StatusNotFound, fmt.Sprintf("workflow not found: %s", id))
		return nil, false
	}
	tid := tenantID(c)
	if tid != "" && ws.TenantID != tid {
		respondError(c, http.StatusForbidden, "workflow does not belong to this tenant")
		return nil, false
	}
	return ws, true
}

// ─── Health ───────────────────────────────────────────────────────────────────

// ReadinessProbe allows optional subsystem readiness checks.
type ReadinessProbe interface {
	Ready(context.Context) error
}

// Healthz returns 200 OK unconditionally (liveness probe).
func Healthz() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// Readyz checks the store connection and all registered providers (readiness probe).
func Readyz(store storage.Store, probes ...ReadinessProbe) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := store.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
			return
		}
		for _, p := range common.All() {
			if err := p.HealthCheck(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status":   "not ready",
					"provider": p.Name(),
					"error":    err.Error(),
				})
				return
			}
		}
		for _, probe := range probes {
			if probe == nil {
				continue
			}
			if err := probe.Ready(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status": "not ready",
					"error":  err.Error(),
				})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

// ─── Workflow handlers ────────────────────────────────────────────────────────

// DeliveryRequest holds the optional delivery configuration for a workflow.
type DeliveryRequest struct {
	Action string          `json:"action"` // github-pr | webhook-dispatch | etc.
	Config json.RawMessage `json:"config"` // action-specific non-secret config
}

// PlanningRequest captures optional builder planning state at workflow creation.
type PlanningRequest struct {
	Mode      string   `json:"mode"` // e.g. "builder"
	Prompt    string   `json:"prompt"`
	Plan      string   `json:"plan"`
	Summary   string   `json:"summary"`
	Decisions []string `json:"decisions"`
	Questions []string `json:"questions"`
}

// CreateWorkflowRequest is the request body for POST /workflows.
type CreateWorkflowRequest struct {
	Request         string          `json:"request" binding:"required"`
	Title           string          `json:"title"`
	Mode            string          `json:"mode"`              // optional; Director will classify if omitted
	Provider        string          `json:"provider"`          // optional override
	Model           string          `json:"model"`             // optional override
	Delivery        DeliveryRequest `json:"delivery"`          // optional delivery action + config
	UploadSessionID string          `json:"upload_session_id"` // optional; links staged uploads to this workflow
	Planning        PlanningRequest `json:"planning"`          // optional builder plan seed
	AutoMode        bool            `json:"auto_mode"`         // enable dynamic auto-mode orchestration
	TemplateID      string          `json:"template_id"`       // optional workflow template
}

func normalizeWorkflowMode(raw string) state.WorkflowMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "content-creation", "content_creation", "content-pipeline", "blog", "article", "post":
		return state.WorkflowModeContent
	case "software-creation", "software_creation", "software-generation", "software_generation", "code", "coding":
		return state.WorkflowModeSoftware
	case "documentation", "docs-generation", "docs_generation":
		return state.WorkflowModeDocs
	case "auto", "auto_mode", "auto-mode":
		return state.WorkflowModeAuto
	default:
		return state.WorkflowMode(raw)
	}
}

// CreateWorkflow handles POST /workflows.
func CreateWorkflow(store storage.Store, sched *scheduler.Scheduler, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateWorkflowRequest
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

		ws := state.NewWorkflowState(tid, sid, req.Request)
		ws.Title = req.Title
		if req.Mode != "" {
			ws.Mode = normalizeWorkflowMode(req.Mode)
		}
		ws.ProviderName = req.Provider
		ws.ModelName = req.Model
		if req.Delivery.Action != "" {
			ws.DeliveryAction = req.Delivery.Action
		}
		if len(req.Delivery.Config) > 0 {
			ws.DeliveryConfig = req.Delivery.Config
		}
		if req.UploadSessionID != "" {
			ws.UploadSessionID = req.UploadSessionID
		}
		if req.AutoMode || ws.Mode == state.WorkflowModeAuto {
			ws.Mode = state.WorkflowModeAuto
			ws.Execution.AutoMode = &state.AutoModeState{Enabled: true}
		}
		if tid := strings.TrimSpace(req.TemplateID); tid != "" {
			if t, ok := templates.Get(tid); ok {
				templates.Apply(ws, t)
			} else {
				respondError(c, http.StatusBadRequest, "unknown template_id: "+tid)
				return
			}
		}
		if strings.TrimSpace(req.Planning.Mode) != "" ||
			strings.TrimSpace(req.Planning.Prompt) != "" ||
			strings.TrimSpace(req.Planning.Plan) != "" {
			ws.Execution.Planning = &state.PlanningState{
				Mode:      strings.TrimSpace(req.Planning.Mode),
				Prompt:    strings.TrimSpace(req.Planning.Prompt),
				Plan:      strings.TrimSpace(req.Planning.Plan),
				Summary:   strings.TrimSpace(req.Planning.Summary),
				Decisions: append([]string(nil), req.Planning.Decisions...),
				Questions: append([]string(nil), req.Planning.Questions...),
				UpdatedAt: time.Now().UTC(),
			}
		}

		if err := store.CreateWorkflow(c.Request.Context(), ws); err != nil {
			log.Error("create workflow", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to create workflow")
			return
		}

		// Atomically consume the upload session, binding its attachments to this workflow.
		if req.UploadSessionID != "" {
			if err := store.ConsumeUploadSession(c.Request.Context(), req.UploadSessionID, ws.ID, tid); err != nil {
				log.Error("consume upload session", zap.String("session_id", req.UploadSessionID), zap.Error(err))
				respondError(c, http.StatusBadRequest, "failed to consume upload session: "+err.Error())
				return
			}
		}

		if sched != nil {
			if err := sched.Enqueue(ws.ID); err != nil {
				log.Warn("enqueue workflow", zap.String("workflow_id", ws.ID), zap.Error(err))
				// Non-fatal: workflow is persisted, can be resumed later.
			}
		}

		c.JSON(http.StatusCreated, ws)
	}
}

// UpdateWorkflowPlanningRequest patches persisted planning state for a workflow.
type UpdateWorkflowPlanningRequest struct {
	Prompt    *string  `json:"prompt"`
	Plan      *string  `json:"plan"`
	Summary   *string  `json:"summary"`
	Decisions []string `json:"decisions"`
	Questions []string `json:"questions"`
}

// UpdateWorkflowPlanning handles PATCH /workflows/:id/planning.
func UpdateWorkflowPlanning(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}
		var req UpdateWorkflowPlanningRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if ws.Execution.Planning == nil {
			ws.Execution.Planning = &state.PlanningState{Mode: "builder"}
		}
		if req.Prompt != nil {
			ws.Execution.Planning.Prompt = strings.TrimSpace(*req.Prompt)
		}
		if req.Plan != nil {
			ws.Execution.Planning.Plan = strings.TrimSpace(*req.Plan)
		}
		if req.Summary != nil {
			ws.Execution.Planning.Summary = strings.TrimSpace(*req.Summary)
		}
		if req.Decisions != nil {
			ws.Execution.Planning.Decisions = append([]string(nil), req.Decisions...)
		}
		if req.Questions != nil {
			ws.Execution.Planning.Questions = append([]string(nil), req.Questions...)
		}
		ws.Execution.Planning.UpdatedAt = time.Now().UTC()
		ws.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorkflow(c.Request.Context(), ws); err != nil {
			log.Error("update workflow planning", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to update planning state")
			return
		}
		c.JSON(http.StatusOK, ws)
	}
}

// GetWorkflow handles GET /workflows/:id.
func GetWorkflow(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, ws)
	}
}

// ListWorkflows handles GET /workflows.
func ListWorkflows(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := tenantID(c)
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limit > 100 {
			limit = 100
		}

		wss, err := store.ListWorkflows(c.Request.Context(), tid, limit, offset)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to list workflows")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"workflows": wss,
			"limit":     limit,
			"offset":    offset,
		})
	}
}

// GetWorkflowEvents handles GET /workflows/:id/events.
func GetWorkflowEvents(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := checkWorkflowOwnership(c, store); !ok {
			return
		}
		evts, err := store.ListEvents(c.Request.Context(), c.Param("id"))
		if err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("events not found for workflow: %s", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, gin.H{"events": evts, "count": len(evts)})
	}
}

// CancelWorkflow handles POST /workflows/:id/cancel.
func CancelWorkflow(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}
		if ws.Status == state.WorkflowStatusCompleted ||
			ws.Status == state.WorkflowStatusCancelled {
			respondError(c, http.StatusConflict, "workflow is already terminal")
			return
		}
		if err := store.UpdateWorkflowStatus(c.Request.Context(), id,
			state.WorkflowStatusCancelled, "cancelled by API"); err != nil {
			log.Error("cancel workflow", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to cancel workflow")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
	}
}

// ResumeWorkflow handles POST /workflows/:id/resume.
// Re-enqueues a paused or failed workflow.
func ResumeWorkflow(store storage.Store, sched *scheduler.Scheduler, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}
		if ws.Status != state.WorkflowStatusPaused && ws.Status != state.WorkflowStatusFailed {
			respondError(c, http.StatusConflict,
				fmt.Sprintf("workflow status %q cannot be resumed", ws.Status))
			return
		}
		if err := store.UpdateWorkflowStatus(c.Request.Context(), id,
			state.WorkflowStatusPending, ""); err != nil {
			log.Error("resume workflow", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to resume workflow")
			return
		}
		if sched != nil {
			if err := sched.Enqueue(id); err != nil {
				respondError(c, http.StatusServiceUnavailable, err.Error())
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "pending"})
	}
}

// ─── Provider handlers ────────────────────────────────────────────────────────

// ListProviders handles GET /providers.
func ListProviders() gin.HandlerFunc {
	return func(c *gin.Context) {
		providers := common.All()
		type providerInfo struct {
			Name         string              `json:"name"`
			Capabilities []common.Capability `json:"capabilities"`
		}
		out := make([]providerInfo, 0, len(providers))
		for _, p := range providers {
			out = append(out, providerInfo{
				Name:         p.Name(),
				Capabilities: p.Capabilities(),
			})
		}
		c.JSON(http.StatusOK, gin.H{"providers": out})
	}
}

// TestProvider handles POST /providers/:name/test.
func TestProvider(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		p, ok := common.Get(name)
		if !ok {
			respondError(c, http.StatusNotFound, fmt.Sprintf("provider %q not found", name))
			return
		}
		if err := p.HealthCheck(c.Request.Context()); err != nil {
			c.JSON(http.StatusOK, gin.H{"healthy": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"healthy": true})
	}
}

// ListProviderModels handles GET /providers/:name/models.
func ListProviderModels() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		p, ok := common.Get(name)
		if !ok {
			respondError(c, http.StatusNotFound, fmt.Sprintf("provider %q not found", name))
			return
		}
		models, err := p.Models(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, fmt.Sprintf("failed to list models for %q: %v", name, err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"provider": name, "models": models})
	}
}

// ─── Scope/Tenant handlers ────────────────────────────────────────────────────

// GetEffectiveConfig handles GET /scopes/:id/effective-config.
// Walks the ancestor chain from the requested scope up to global and returns
// the ordered list of scopes representing the resolution chain.
func GetEffectiveConfig(store storage.Store) gin.HandlerFunc {
	svc := scope.New(store)
	return func(c *gin.Context) {
		chain, err := svc.ResolveChain(c.Request.Context(), c.Param("id"))
		if err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("scope not found or chain error: %v", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"scope_id":         c.Param("id"),
			"resolution_chain": chain,
			"depth":            len(chain),
		})
	}
}

// ResolveCustomizations handles GET /customizations/resolve.
// Returns the resolved customization snapshot for the requesting scope.
func ResolveCustomizations(reg *customization.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		sid := scopeID(c)
		if reg == nil {
			c.JSON(http.StatusOK, gin.H{
				"skills":  []interface{}{},
				"agents":  []interface{}{},
				"prompts": []interface{}{},
				"note":    "no customization registry configured",
			})
			return
		}

		snap, err := reg.Snapshot(sid)
		if err != nil {
			respondError(c, http.StatusInternalServerError,
				fmt.Sprintf("customization snapshot error: %v", err))
			return
		}

		type itemInfo struct {
			Name       string `json:"name"`
			SourceName string `json:"source"`
			Path       string `json:"path"`
		}
		toInfo := func(items []*customization.Item) []itemInfo {
			out := make([]itemInfo, 0, len(items))
			for _, it := range items {
				out = append(out, itemInfo{Name: it.Name, SourceName: it.SourceName, Path: it.Path})
			}
			return out
		}

		c.JSON(http.StatusOK, gin.H{
			"scope_id": sid,
			"skills":   toInfo(snap.Skills),
			"agents":   toInfo(snap.Agents),
			"prompts":  toInfo(snap.Prompts),
		})
	}
}

// ─── Tenant handlers ──────────────────────────────────────────────────────────

// CreateTenantRequest is the body for POST /tenants.
type CreateTenantRequest struct {
	Slug string `json:"slug" binding:"required"`
	Name string `json:"name" binding:"required"`
}

// CreateTenant handles POST /tenants.
func CreateTenant(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateTenantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		now := time.Now().UTC()
		t := &state.Tenant{
			ID:        uuid.New().String(),
			Slug:      req.Slug,
			Name:      req.Name,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.CreateTenant(c.Request.Context(), t); err != nil {
			log.Error("create tenant", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to create tenant")
			return
		}
		c.JSON(http.StatusCreated, t)
	}
}

// ListTenants handles GET /tenants.
func ListTenants(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenants, err := store.ListTenants(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to list tenants")
			return
		}
		c.JSON(http.StatusOK, gin.H{"tenants": tenants})
	}
}

// GetTenant handles GET /tenants/:id.
func GetTenant(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := store.GetTenant(c.Request.Context(), c.Param("id"))
		if err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("tenant not found: %s", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, t)
	}
}

// UpdateTenantRequest is the body for PATCH /tenants/:id.
type UpdateTenantRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// UpdateTenant handles PATCH /tenants/:id.
func UpdateTenant(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req UpdateTenantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		t, err := store.GetTenant(c.Request.Context(), id)
		if err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("tenant not found: %s", id))
			return
		}
		if req.Slug != "" {
			t.Slug = req.Slug
		}
		if req.Name != "" {
			t.Name = req.Name
		}
		t.UpdatedAt = time.Now().UTC()
		if err := store.UpdateTenant(c.Request.Context(), t); err != nil {
			log.Error("update tenant", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to update tenant")
			return
		}
		c.JSON(http.StatusOK, t)
	}
}

// DeleteTenant handles DELETE /tenants/:id.
func DeleteTenant(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if _, err := store.GetTenant(c.Request.Context(), id); err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("tenant not found: %s", id))
			return
		}
		if err := store.DeleteTenant(c.Request.Context(), id); err != nil {
			log.Error("delete tenant", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to delete tenant")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// ─── Scope handlers ───────────────────────────────────────────────────────────

// CreateScopeRequest is the body for POST /tenants/:id/scopes.
type CreateScopeRequest struct {
	Kind          string `json:"kind" binding:"required"` // global | org | team
	Name          string `json:"name" binding:"required"`
	Slug          string `json:"slug" binding:"required"`
	ParentScopeID string `json:"parent_scope_id"`
}

// CreateScope handles POST /tenants/:id/scopes.
// Hierarchy constraints (team must have org parent, org must have global
// parent, global must have no parent) are enforced by scope.Service.Create.
func CreateScope(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	svc := scope.New(store)
	return func(c *gin.Context) {
		tenantID := c.Param("id")
		var req CreateScopeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		sc, err := svc.Create(c.Request.Context(), tenantID,
			state.ScopeKind(req.Kind), req.Name, req.Slug, req.ParentScopeID)
		if err != nil {
			log.Error("create scope", zap.Error(err))
			// Hierarchy violations surface as 400 Bad Request.
			status := http.StatusInternalServerError
			msg := "failed to create scope"
			if isHierarchyError(err) {
				status = http.StatusBadRequest
				msg = err.Error()
			}
			respondError(c, status, msg)
			return
		}
		c.JSON(http.StatusCreated, sc)
	}
}

// ListScopesForTenant handles GET /tenants/:id/scopes.
func ListScopesForTenant(store storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		scopes, err := store.ListScopes(c.Request.Context(), c.Param("id"))
		if err != nil {
			respondError(c, http.StatusInternalServerError, "failed to list scopes")
			return
		}
		c.JSON(http.StatusOK, gin.H{"scopes": scopes})
	}
}

// UpdateScopeRequest is the body for PATCH /tenants/:tenantId/scopes/:id.
type UpdateScopeRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// UpdateScope handles PATCH /tenants/:tenantId/scopes/:id.
func UpdateScope(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("scopeId")
		var req UpdateScopeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		sc, err := store.GetScope(c.Request.Context(), id)
		if err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("scope not found: %s", id))
			return
		}
		if req.Name != "" {
			sc.Name = req.Name
		}
		if req.Slug != "" {
			sc.Slug = req.Slug
		}
		sc.UpdatedAt = time.Now().UTC()
		if err := store.UpdateScope(c.Request.Context(), sc); err != nil {
			log.Error("update scope", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to update scope")
			return
		}
		c.JSON(http.StatusOK, sc)
	}
}

// DeleteScope handles DELETE /tenants/:tenantId/scopes/:id.
func DeleteScope(store storage.Store, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("scopeId")
		if _, err := store.GetScope(c.Request.Context(), id); err != nil {
			respondError(c, http.StatusNotFound, fmt.Sprintf("scope not found: %s", id))
			return
		}
		if err := store.DeleteScope(c.Request.Context(), id); err != nil {
			log.Error("delete scope", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to delete scope")
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// ─── Workflow creation helpers (used by tests) ────────────────────────────────

// NewWorkflowID generates a random workflow ID for use in tests.
func NewWorkflowID() string {
	return uuid.New().String()
}

// NewTimestamp returns current UTC time.
func NewTimestamp() time.Time { return time.Now().UTC() }

// isHierarchyError returns true when the error came from scope hierarchy
// validation (parent kind mismatch, missing parent, etc.).  These are
// surfaced as 400 Bad Request rather than 500 Internal Server Error.
func isHierarchyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return len(msg) > 6 && msg[:6] == "scope:"
}

// writeSSEEvent serialises a single events.Event as an SSE data line.
func writeSSEEvent(c *gin.Context, evt interface{}) {
	b, err := json.Marshal(evt)
	if err != nil {
		return
	}
	writeSSEData(c, string(b))
}

// writeSSEData writes a single SSE data frame and flushes.
func writeSSEData(c *gin.Context, data string) {
	_, _ = c.Writer.WriteString("data: " + data + "\n\n")
	c.Writer.Flush()
}
