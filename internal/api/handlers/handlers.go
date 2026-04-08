// Package handlers provides Gin HTTP handler functions for the gorca API.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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

// Healthz returns 200 OK unconditionally (liveness probe).
func Healthz() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// Readyz checks the store connection and all registered providers (readiness probe).
func Readyz(store storage.Store) gin.HandlerFunc {
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
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

// ─── Workflow handlers ────────────────────────────────────────────────────────

// DeliveryRequest holds the optional delivery configuration for a workflow.
type DeliveryRequest struct {
	Action string          `json:"action"` // github-pr | webhook-dispatch | etc.
	Config json.RawMessage `json:"config"` // action-specific non-secret config
}

// CreateWorkflowRequest is the request body for POST /workflows.
type CreateWorkflowRequest struct {
	Request  string          `json:"request" binding:"required"`
	Title    string          `json:"title"`
	Mode     string          `json:"mode"`     // optional; Director will classify if omitted
	Provider string          `json:"provider"` // optional override
	Model    string          `json:"model"`    // optional override
	Delivery DeliveryRequest `json:"delivery"` // optional delivery action + config
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
			ws.Mode = state.WorkflowMode(req.Mode)
		}
		ws.ProviderName = req.Provider
		ws.ModelName = req.Model
		if req.Delivery.Action != "" {
			ws.DeliveryAction = req.Delivery.Action
		}
		if len(req.Delivery.Config) > 0 {
			ws.DeliveryConfig = req.Delivery.Config
		}

		if err := store.CreateWorkflow(c.Request.Context(), ws); err != nil {
			log.Error("create workflow", zap.Error(err))
			respondError(c, http.StatusInternalServerError, "failed to create workflow")
			return
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

// ─── SSE streaming ────────────────────────────────────────────────────────────

// StreamWorkflowEvents handles GET /workflows/:id/stream.
//
// It streams workflow events to the client as Server-Sent Events (SSE).
// The handler polls the event store on a 1-second ticker, sending only events
// that occurred after the last one delivered.  The stream closes automatically
// when:
//   - the workflow reaches a terminal state (completed, failed, cancelled), or
//   - the client disconnects, or
//   - the optional ?timeout query parameter (seconds) expires (default 300s).
//
// Each SSE event has the form:
//
//	data: <json-encoded events.Event>\n\n
//
// A special keepalive comment ": keepalive\n\n" is sent every tick if no new
// events were available, to prevent proxy timeouts.
func StreamWorkflowEvents(store storage.Store) gin.HandlerFunc {
	const defaultTimeoutSec = 300
	const pollInterval = time.Second

	return func(c *gin.Context) {
		id := c.Param("id")

		// Verify the workflow exists and belongs to this tenant before opening the stream.
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}

		// Parse optional timeout query parameter.
		timeoutSec := defaultTimeoutSec
		if raw := c.Query("timeout"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				timeoutSec = v
			}
		}

		// If the workflow is already in a terminal state, return snapshot events
		// synchronously (no streaming needed).
		terminal := func(s state.WorkflowStatus) bool {
			return s == state.WorkflowStatusCompleted ||
				s == state.WorkflowStatusFailed ||
				s == state.WorkflowStatusCancelled
		}
		if terminal(ws.Status) {
			evts, err := store.ListEvents(c.Request.Context(), id)
			if err != nil {
				respondError(c, http.StatusInternalServerError, "failed to list events")
				return
			}
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			for _, evt := range evts {
				writeSSEEvent(c, evt)
			}
			writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
			return
		}

		// Set SSE headers.
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // disable nginx buffering

		ctx := c.Request.Context()
		deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		var lastEventTime time.Time // track cursor

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if now.After(deadline) {
					writeSSEData(c, `{"type":"stream.closed","reason":"timeout"}`)
					return
				}

				evts, err := store.ListEvents(ctx, id)
				if err != nil {
					continue // transient error — keep polling
				}

				newCount := 0
				for _, evt := range evts {
					if evt.OccurredAt.After(lastEventTime) {
						writeSSEEvent(c, evt)
						lastEventTime = evt.OccurredAt
						newCount++
					}
				}

				if newCount == 0 {
					// Send keepalive to prevent proxy timeouts.
					if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
						return
					}
					c.Writer.Flush()
				}

				// Refresh workflow status to detect terminal state.
				current, err := store.GetWorkflow(ctx, id)
				if err == nil && terminal(current.Status) {
					writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
					return
				}
			}
		}
	}
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
