package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
	sqStore "github.com/go-orca/go-orca/internal/storage/sqlite"
)

// newTestStore creates an in-memory SQLite store and runs migrations.
func newTestStore(t *testing.T) *sqStore.Store {
	t.Helper()
	s, err := sqStore.New(":memory:")
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("sqlite.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPing(t *testing.T) {
	s := newTestStore(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// ─── Tenant CRUD ──────────────────────────────────────────────────────────────

func TestTenantCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	tenant := &state.Tenant{
		ID:        "tenant-001",
		Slug:      "acme",
		Name:      "ACME Corp",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create
	if err := s.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Get by ID
	got, err := s.GetTenant(ctx, "tenant-001")
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if got.Slug != "acme" {
		t.Errorf("Slug: got %q, want %q", got.Slug, "acme")
	}

	// Get by slug
	bySlug, err := s.GetTenantBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("GetTenantBySlug: %v", err)
	}
	if bySlug.ID != "tenant-001" {
		t.Errorf("ID: got %q, want %q", bySlug.ID, "tenant-001")
	}

	// List
	all, err := s.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ListTenants len: got %d, want 1", len(all))
	}
}

// ─── Scope CRUD ───────────────────────────────────────────────────────────────

func TestScopeCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	tenant := &state.Tenant{ID: "t1", Slug: "t1", Name: "T1", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	sc := &state.Scope{
		ID:        "scope-001",
		TenantID:  "t1",
		Kind:      state.ScopeKindGlobal,
		Name:      "Global",
		Slug:      "global",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateScope(ctx, sc); err != nil {
		t.Fatalf("CreateScope: %v", err)
	}

	got, err := s.GetScope(ctx, "scope-001")
	if err != nil {
		t.Fatalf("GetScope: %v", err)
	}
	if got.Kind != state.ScopeKindGlobal {
		t.Errorf("Kind: got %q, want %q", got.Kind, state.ScopeKindGlobal)
	}

	scopes, err := s.ListScopes(ctx, "t1")
	if err != nil {
		t.Fatalf("ListScopes: %v", err)
	}
	if len(scopes) != 1 {
		t.Errorf("ListScopes len: got %d, want 1", len(scopes))
	}
}

// ─── Workflow CRUD ────────────────────────────────────────────────────────────

func setupTenantScope(t *testing.T, s *sqStore.Store) (tenantID, scopeID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tid := "tenant-wf"
	sid := "scope-wf"
	_ = s.CreateTenant(ctx, &state.Tenant{ID: tid, Slug: "wf", Name: "WF", CreatedAt: now, UpdatedAt: now})
	_ = s.CreateScope(ctx, &state.Scope{ID: sid, TenantID: tid, Kind: state.ScopeKindGlobal, Name: "G", Slug: "global", CreatedAt: now, UpdatedAt: now})
	return tid, sid
}

func TestWorkflowCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tid, sid := setupTenantScope(t, s)

	ws := state.NewWorkflowState(tid, sid, "build a REST API")
	ws.Title = "Test Workflow"

	// Create
	if err := s.CreateWorkflow(ctx, ws); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Get
	got, err := s.GetWorkflow(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Request != ws.Request {
		t.Errorf("Request: got %q, want %q", got.Request, ws.Request)
	}
	if got.Status != state.WorkflowStatusPending {
		t.Errorf("Status: got %q, want pending", got.Status)
	}

	// Save (upsert)
	ws.Status = state.WorkflowStatusRunning
	ws.Title = "Updated Title"
	if err := s.SaveWorkflow(ctx, ws); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}

	got2, _ := s.GetWorkflow(ctx, ws.ID)
	if got2.Status != state.WorkflowStatusRunning {
		t.Errorf("Status after save: got %q, want running", got2.Status)
	}

	// UpdateWorkflowStatus
	if err := s.UpdateWorkflowStatus(ctx, ws.ID, state.WorkflowStatusCompleted, ""); err != nil {
		t.Fatalf("UpdateWorkflowStatus: %v", err)
	}
	got3, _ := s.GetWorkflow(ctx, ws.ID)
	if got3.Status != state.WorkflowStatusCompleted {
		t.Errorf("Status after update: got %q, want completed", got3.Status)
	}

	// List
	list, err := s.ListWorkflows(ctx, tid, 10, 0)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListWorkflows len: got %d, want 1", len(list))
	}
}

// ─── Event journal ────────────────────────────────────────────────────────────

func TestEventJournal(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tid, sid := setupTenantScope(t, s)

	ws := state.NewWorkflowState(tid, sid, "event test")
	if err := s.CreateWorkflow(ctx, ws); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	evt1, _ := events.NewEvent(ws.ID, tid, sid, events.EventWorkflowStarted, "", nil)
	evt2, _ := events.NewEvent(ws.ID, tid, sid, events.EventPersonaStarted, state.PersonaDirector,
		events.PersonaStartedPayload{Persona: state.PersonaDirector, ProviderName: "openai", ModelName: "gpt-4o"})

	if err := s.AppendEvents(ctx, evt1, evt2); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	evts, err := s.ListEvents(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evts) != 2 {
		t.Errorf("ListEvents len: got %d, want 2", len(evts))
	}

	byType, err := s.ListEventsByType(ctx, ws.ID, events.EventPersonaStarted)
	if err != nil {
		t.Fatalf("ListEventsByType: %v", err)
	}
	if len(byType) != 1 {
		t.Errorf("ListEventsByType len: got %d, want 1", len(byType))
	}

	since, err := s.EventsSince(ctx, tid, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(since) < 2 {
		t.Errorf("EventsSince len: got %d, want >=2", len(since))
	}
}
