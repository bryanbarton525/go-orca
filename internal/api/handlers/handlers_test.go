package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/api/handlers"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ─── Minimal in-memory store for HTTP tests ───────────────────────────────────

type memStore struct {
	mu        sync.RWMutex
	tenants   map[string]*state.Tenant
	scopes    map[string]*state.Scope
	workflows map[string]*state.WorkflowState
	events    []*events.Event
}

func newMemStore() *memStore {
	return &memStore{
		tenants:   make(map[string]*state.Tenant),
		scopes:    make(map[string]*state.Scope),
		workflows: make(map[string]*state.WorkflowState),
	}
}

func (m *memStore) Ping(_ context.Context) error { return nil }
func (m *memStore) Close() error                 { return nil }

func (m *memStore) CreateTenant(_ context.Context, t *state.Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.tenants[t.ID] = &cp
	return nil
}
func (m *memStore) GetTenant(_ context.Context, id string) (*state.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *t
	return &cp, nil
}
func (m *memStore) GetTenantBySlug(_ context.Context, slug string) (*state.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tenants {
		if t.Slug == slug {
			cp := *t
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}
func (m *memStore) ListTenants(_ context.Context) ([]*state.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*state.Tenant, 0, len(m.tenants))
	for _, t := range m.tenants {
		cp := *t
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memStore) UpdateTenant(_ context.Context, t *state.Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenants[t.ID]; !ok {
		return errors.New("not found")
	}
	cp := *t
	m.tenants[t.ID] = &cp
	return nil
}

func (m *memStore) DeleteTenant(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenants[id]; !ok {
		return errors.New("not found")
	}
	delete(m.tenants, id)
	return nil
}

func (m *memStore) CreateScope(_ context.Context, s *state.Scope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.scopes[s.ID] = &cp
	return nil
}
func (m *memStore) GetScope(_ context.Context, id string) (*state.Scope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.scopes[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *s
	return &cp, nil
}
func (m *memStore) ListScopes(_ context.Context, tenantID string) ([]*state.Scope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*state.Scope
	for _, s := range m.scopes {
		if s.TenantID == tenantID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memStore) UpdateScope(_ context.Context, s *state.Scope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scopes[s.ID]; !ok {
		return errors.New("not found")
	}
	cp := *s
	m.scopes[s.ID] = &cp
	return nil
}

func (m *memStore) DeleteScope(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scopes[id]; !ok {
		return errors.New("not found")
	}
	delete(m.scopes, id)
	return nil
}

func (m *memStore) CreateWorkflow(_ context.Context, ws *state.WorkflowState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *ws
	m.workflows[ws.ID] = &cp
	return nil
}
func (m *memStore) GetWorkflow(_ context.Context, id string) (*state.WorkflowState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws, ok := m.workflows[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *ws
	return &cp, nil
}
func (m *memStore) SaveWorkflow(_ context.Context, ws *state.WorkflowState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *ws
	m.workflows[ws.ID] = &cp
	return nil
}
func (m *memStore) ListWorkflows(_ context.Context, tenantID string, limit, offset int) ([]*state.WorkflowState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []*state.WorkflowState
	for _, ws := range m.workflows {
		if ws.TenantID == tenantID {
			cp := *ws
			all = append(all, &cp)
		}
	}
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}
func (m *memStore) UpdateWorkflowStatus(_ context.Context, id string, status state.WorkflowStatus, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws, ok := m.workflows[id]
	if !ok {
		return errors.New("not found")
	}
	ws.Status = status
	ws.ErrorMessage = errMsg
	return nil
}
func (m *memStore) AppendEvents(_ context.Context, evts ...*events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evts...)
	return nil
}
func (m *memStore) ListEvents(_ context.Context, workflowID string) ([]*events.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*events.Event
	for _, e := range m.events {
		if e.WorkflowID == workflowID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (m *memStore) ListEventsByType(_ context.Context, workflowID string, evtType events.EventType) ([]*events.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*events.Event
	for _, e := range m.events {
		if e.WorkflowID == workflowID && e.Type == evtType {
			out = append(out, e)
		}
	}
	return out, nil
}
func (m *memStore) EventsSince(_ context.Context, tenantID string, after time.Time) ([]*events.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*events.Event
	for _, e := range m.events {
		if e.TenantID == tenantID && e.OccurredAt.After(after) {
			out = append(out, e)
		}
	}
	return out, nil
}

// ─── HTTP test helpers ────────────────────────────────────────────────────────

func newRouter(store *memStore) *gin.Engine {
	log := zap.NewNop()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenant_id", "t1")
		c.Set("scope_id", "s1")
		c.Next()
	})

	wf := r.Group("/workflows")
	wf.GET("", handlers.ListWorkflows(store))
	wf.POST("", handlers.CreateWorkflow(store, nil, log))
	wf.GET("/:id", handlers.GetWorkflow(store))
	wf.GET("/:id/events", handlers.GetWorkflowEvents(store))
	wf.POST("/:id/cancel", handlers.CancelWorkflow(store, log))
	wf.POST("/:id/resume", handlers.ResumeWorkflow(store, nil, log))
	wf.GET("/:id/stream", handlers.StreamWorkflowEvents(store))

	r.GET("/healthz", handlers.Healthz())
	r.GET("/readyz", handlers.Readyz(store))

	prov := r.Group("/providers")
	prov.GET("", handlers.ListProviders())

	tenants := r.Group("/tenants")
	tenants.GET("", handlers.ListTenants(store))
	tenants.POST("", handlers.CreateTenant(store, log))
	tenant := tenants.Group("/:id")
	tenant.GET("", handlers.GetTenant(store))
	tenant.PATCH("", handlers.UpdateTenant(store, log))
	tenant.DELETE("", handlers.DeleteTenant(store, log))
	tenant.POST("/scopes", handlers.CreateScope(store, log))
	tenant.GET("/scopes", handlers.ListScopesForTenant(store))
	tenant.PATCH("/scopes/:scopeId", handlers.UpdateScope(store, log))
	tenant.DELETE("/scopes/:scopeId", handlers.DeleteScope(store, log))
	return r
}

func doRequest(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	r := newRouter(newMemStore())
	w := doRequest(r, http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz: got %d, want 200", w.Code)
	}
}

func TestReadyz(t *testing.T) {
	r := newRouter(newMemStore())
	w := doRequest(r, http.MethodGet, "/readyz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /readyz: got %d, want 200", w.Code)
	}
}

func TestCreateAndGetWorkflow(t *testing.T) {
	ms := newMemStore()
	// Seed tenant+scope so we pass the header middleware.
	now := time.Now().UTC()
	_ = ms.CreateTenant(context.Background(), &state.Tenant{ID: "t1", Slug: "t1", Name: "T1", CreatedAt: now, UpdatedAt: now})
	_ = ms.CreateScope(context.Background(), &state.Scope{ID: "s1", TenantID: "t1", Kind: state.ScopeKindGlobal, Name: "G", Slug: "global", CreatedAt: now, UpdatedAt: now})

	r := newRouter(ms)

	// POST /workflows — scheduler is nil so we expect a 201 but log warning.
	body := map[string]string{"request": "build a thing", "title": "Smoke test"}
	w := doRequest(r, http.MethodPost, "/workflows", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /workflows: got %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var created state.WorkflowState
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created workflow has empty ID")
	}
	if created.Request != "build a thing" {
		t.Errorf("Request: got %q, want %q", created.Request, "build a thing")
	}

	// GET /workflows/:id
	w2 := doRequest(r, http.MethodGet, "/workflows/"+created.ID, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /workflows/:id: got %d, want 200", w2.Code)
	}
	var fetched state.WorkflowState
	_ = json.NewDecoder(w2.Body).Decode(&fetched)
	if fetched.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", fetched.ID, created.ID)
	}
}

func TestGetWorkflowNotFound(t *testing.T) {
	r := newRouter(newMemStore())
	w := doRequest(r, http.MethodGet, "/workflows/does-not-exist", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /workflows/nope: got %d, want 404", w.Code)
	}
}

func TestListWorkflows(t *testing.T) {
	ms := newMemStore()
	now := time.Now().UTC()
	_ = ms.CreateTenant(context.Background(), &state.Tenant{ID: "t1", Slug: "t1", Name: "T1", CreatedAt: now, UpdatedAt: now})
	_ = ms.CreateScope(context.Background(), &state.Scope{ID: "s1", TenantID: "t1", Kind: state.ScopeKindGlobal, Name: "G", Slug: "global", CreatedAt: now, UpdatedAt: now})

	ctx := context.Background()
	ws := state.NewWorkflowState("t1", "s1", "list me")
	_ = ms.CreateWorkflow(ctx, ws)

	r := newRouter(ms)
	w := doRequest(r, http.MethodGet, "/workflows?limit=10&offset=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /workflows: got %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	wfs, _ := resp["workflows"].([]interface{})
	if len(wfs) < 1 {
		t.Errorf("expected at least 1 workflow in list, got %d", len(wfs))
	}
}

func TestCancelWorkflow(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	ws := state.NewWorkflowState("t1", "s1", "cancel me")
	_ = ms.CreateWorkflow(ctx, ws)

	r := newRouter(ms)
	w := doRequest(r, http.MethodPost, "/workflows/"+ws.ID+"/cancel", nil)
	if w.Code != http.StatusOK {
		t.Errorf("POST /cancel: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Second cancel should 409 (already terminal).
	w2 := doRequest(r, http.MethodPost, "/workflows/"+ws.ID+"/cancel", nil)
	if w2.Code != http.StatusConflict {
		t.Errorf("second cancel: got %d, want 409", w2.Code)
	}
}

func TestGetWorkflowEvents(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	ws := state.NewWorkflowState("t1", "s1", "events test")
	_ = ms.CreateWorkflow(ctx, ws)

	evt, _ := events.NewEvent(ws.ID, "t1", "s1", events.EventWorkflowStarted, "", nil)
	_ = ms.AppendEvents(ctx, evt)

	r := newRouter(ms)
	w := doRequest(r, http.MethodGet, "/workflows/"+ws.ID+"/events", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /events: got %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	count, _ := resp["count"].(float64)
	if count < 1 {
		t.Errorf("expected at least 1 event, got %v", count)
	}
}

func TestCreateAndGetTenant(t *testing.T) {
	r := newRouter(newMemStore())

	body := map[string]string{"slug": "myorg", "name": "My Org"}
	w := doRequest(r, http.MethodPost, "/tenants", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /tenants: got %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var tenant state.Tenant
	_ = json.NewDecoder(w.Body).Decode(&tenant)
	if tenant.Slug != "myorg" {
		t.Errorf("Slug: got %q, want myorg", tenant.Slug)
	}

	w2 := doRequest(r, http.MethodGet, "/tenants/"+tenant.ID, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /tenants/:id: got %d, want 200", w2.Code)
	}
}

func TestUpdateTenant(t *testing.T) {
	ms := newMemStore()
	r := newRouter(ms)

	// Create
	w := doRequest(r, http.MethodPost, "/tenants", map[string]string{"slug": "orig", "name": "Original"})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /tenants: got %d", w.Code)
	}
	var created state.Tenant
	_ = json.NewDecoder(w.Body).Decode(&created)

	// Update
	w2 := doRequest(r, http.MethodPatch, "/tenants/"+created.ID,
		map[string]string{"name": "Updated Name"})
	if w2.Code != http.StatusOK {
		t.Fatalf("PATCH /tenants/:id: got %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
	var updated state.Tenant
	_ = json.NewDecoder(w2.Body).Decode(&updated)
	if updated.Name != "Updated Name" {
		t.Errorf("Name: got %q, want %q", updated.Name, "Updated Name")
	}

	// Slug unchanged
	if updated.Slug != "orig" {
		t.Errorf("Slug: got %q, want %q", updated.Slug, "orig")
	}
}

func TestDeleteTenant(t *testing.T) {
	ms := newMemStore()
	r := newRouter(ms)

	w := doRequest(r, http.MethodPost, "/tenants", map[string]string{"slug": "todel", "name": "To Delete"})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /tenants: got %d", w.Code)
	}
	var created state.Tenant
	_ = json.NewDecoder(w.Body).Decode(&created)

	w2 := doRequest(r, http.MethodDelete, "/tenants/"+created.ID, nil)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("DELETE /tenants/:id: got %d, want 204; body: %s", w2.Code, w2.Body.String())
	}

	// Should be gone.
	w3 := doRequest(r, http.MethodGet, "/tenants/"+created.ID, nil)
	if w3.Code != http.StatusNotFound {
		t.Errorf("GET after delete: got %d, want 404", w3.Code)
	}
}

func TestCreateAndListScopes(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = ms.CreateTenant(ctx, &state.Tenant{ID: "t1", Slug: "t1", Name: "T1", CreatedAt: now, UpdatedAt: now})

	r := newRouter(ms)

	body := map[string]string{"kind": "global", "name": "Global", "slug": "global"}
	w := doRequest(r, http.MethodPost, "/tenants/t1/scopes", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /tenants/t1/scopes: got %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var sc state.Scope
	_ = json.NewDecoder(w.Body).Decode(&sc)
	if sc.Kind != state.ScopeKindGlobal {
		t.Errorf("Kind: got %q, want %q", sc.Kind, state.ScopeKindGlobal)
	}

	w2 := doRequest(r, http.MethodGet, "/tenants/t1/scopes", nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /tenants/t1/scopes: got %d, want 200", w2.Code)
	}
	var resp map[string]interface{}
	_ = json.NewDecoder(w2.Body).Decode(&resp)
	scopes, _ := resp["scopes"].([]interface{})
	if len(scopes) < 1 {
		t.Errorf("expected at least 1 scope, got %d", len(scopes))
	}
}

func TestUpdateScope(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = ms.CreateTenant(ctx, &state.Tenant{ID: "t2", Slug: "t2", Name: "T2", CreatedAt: now, UpdatedAt: now})

	r := newRouter(ms)

	w := doRequest(r, http.MethodPost, "/tenants/t2/scopes",
		map[string]string{"kind": "global", "name": "G", "slug": "global"})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST scope: got %d", w.Code)
	}
	var sc state.Scope
	_ = json.NewDecoder(w.Body).Decode(&sc)

	w2 := doRequest(r, http.MethodPatch, "/tenants/t2/scopes/"+sc.ID,
		map[string]string{"name": "Global Renamed"})
	if w2.Code != http.StatusOK {
		t.Fatalf("PATCH scope: got %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
	var updated state.Scope
	_ = json.NewDecoder(w2.Body).Decode(&updated)
	if updated.Name != "Global Renamed" {
		t.Errorf("Name: got %q, want %q", updated.Name, "Global Renamed")
	}
}

func TestDeleteScope(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = ms.CreateTenant(ctx, &state.Tenant{ID: "t3", Slug: "t3", Name: "T3", CreatedAt: now, UpdatedAt: now})

	r := newRouter(ms)

	w := doRequest(r, http.MethodPost, "/tenants/t3/scopes",
		map[string]string{"kind": "global", "name": "G", "slug": "global"})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST scope: got %d", w.Code)
	}
	var sc state.Scope
	_ = json.NewDecoder(w.Body).Decode(&sc)

	w2 := doRequest(r, http.MethodDelete, "/tenants/t3/scopes/"+sc.ID, nil)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("DELETE scope: got %d, want 204; body: %s", w2.Code, w2.Body.String())
	}
}

func TestResumeWorkflow(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	ws := state.NewWorkflowState("t1", "s1", "resume me")
	_ = ms.CreateWorkflow(ctx, ws)

	// Put it in failed state.
	_ = ms.UpdateWorkflowStatus(ctx, ws.ID, state.WorkflowStatusFailed, "oops")

	r := newRouter(ms)
	w := doRequest(r, http.MethodPost, "/workflows/"+ws.ID+"/resume", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /resume: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Resuming a non-paused/non-failed workflow should 409.
	_ = ms.UpdateWorkflowStatus(ctx, ws.ID, state.WorkflowStatusCompleted, "")
	w2 := doRequest(r, http.MethodPost, "/workflows/"+ws.ID+"/resume", nil)
	if w2.Code != http.StatusConflict {
		t.Errorf("resume completed: got %d, want 409", w2.Code)
	}
}

func TestListProviders(t *testing.T) {
	r := newRouter(newMemStore())
	w := doRequest(r, http.MethodGet, "/providers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /providers: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["providers"]; !ok {
		t.Error("response missing 'providers' key")
	}
}

// TestStreamSurvivesShortWriteTimeout verifies that the SSE handler disables
// the server-level write deadline so a stream is not killed by a very short
// write_timeout configuration.
//
// Strategy: start a real httptest.Server with a 200ms WriteTimeout, open a
// live SSE stream to a pending workflow, wait 400ms (two write-timeout cycles),
// then verify the connection is still open and receiving keepalive frames.
func TestStreamSurvivesShortWriteTimeout(t *testing.T) {
	ms := newMemStore()
	ctx := context.Background()
	now := time.Now().UTC()
	_ = ms.CreateTenant(ctx, &state.Tenant{ID: "t1", Slug: "t1", Name: "T1", CreatedAt: now, UpdatedAt: now})
	_ = ms.CreateScope(ctx, &state.Scope{ID: "s1", TenantID: "t1", Kind: state.ScopeKindGlobal, Name: "G", Slug: "global", CreatedAt: now, UpdatedAt: now})

	// Seed a pending (non-terminal) workflow.
	ws := state.NewWorkflowState("t1", "s1", "sse timeout test")
	_ = ms.CreateWorkflow(ctx, ws)

	// Use the full gin router (same as all other tests) so middleware, param
	// parsing, and the response writer chain are exactly as they are in prod.
	r := newRouter(ms)

	// Wrap the gin engine in a real httptest.Server so we can set WriteTimeout.
	srv := httptest.NewUnstartedServer(r)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	// Open the SSE stream with a ?timeout=5 so the handler-side deadline is
	// well beyond our observation window.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/workflows/"+ws.ID+"/stream?timeout=5", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	// Read bytes from the stream for 400ms — two WriteTimeout cycles.  If the
	// server-level deadline is not cleared we would see an EOF here within 200ms.
	buf := make([]byte, 256)
	deadline := time.Now().Add(400 * time.Millisecond)
	bytesRead := 0
	for time.Now().Before(deadline) {
		n, readErr := resp.Body.Read(buf)
		bytesRead += n
		if readErr != nil {
			t.Errorf("stream closed prematurely after %d bytes: %v", bytesRead, readErr)
			return
		}
	}
	if bytesRead == 0 {
		t.Error("received no bytes from SSE stream within 400ms")
	}
}
