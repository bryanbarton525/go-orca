// Package storage defines the Store interface and shared persistence helpers
// used by the workflow engine and API handlers.
package storage

import (
	"context"
	"time"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

// Store is the unified persistence interface for gorca.
// Both the Postgres and SQLite implementations satisfy this interface.
type Store interface {
	WorkflowStore
	EventStore
	TenantStore
	ScopeStore

	// Ping verifies the connection to the backing store.
	Ping(ctx context.Context) error
	// Close releases any held resources (connections, file handles).
	Close() error
}

// WorkflowStore manages workflow state persistence.
type WorkflowStore interface {
	// CreateWorkflow inserts a new workflow (must not already exist).
	CreateWorkflow(ctx context.Context, ws *state.WorkflowState) error
	// GetWorkflow retrieves a workflow by ID.
	GetWorkflow(ctx context.Context, id string) (*state.WorkflowState, error)
	// SaveWorkflow upserts the full workflow state.
	SaveWorkflow(ctx context.Context, ws *state.WorkflowState) error
	// ListWorkflows returns workflows for a tenant, newest first.
	ListWorkflows(ctx context.Context, tenantID string, limit, offset int) ([]*state.WorkflowState, error)
	// UpdateWorkflowStatus performs a targeted status update (avoids full upsert contention).
	UpdateWorkflowStatus(ctx context.Context, id string, status state.WorkflowStatus, errMsg string) error
	// AppendEvents atomically appends events to the journal.
	AppendEvents(ctx context.Context, evts ...*events.Event) error
}

// EventStore manages workflow event journal queries.
type EventStore interface {
	// ListEvents returns all events for a workflow in chronological order.
	ListEvents(ctx context.Context, workflowID string) ([]*events.Event, error)
	// ListEventsByType returns events of a specific type for a workflow.
	ListEventsByType(ctx context.Context, workflowID string, evtType events.EventType) ([]*events.Event, error)
	// EventsSince returns events across all workflows for a tenant after the given time.
	EventsSince(ctx context.Context, tenantID string, after time.Time) ([]*events.Event, error)
}

// TenantStore manages tenant persistence.
type TenantStore interface {
	// CreateTenant inserts a new tenant.
	CreateTenant(ctx context.Context, t *state.Tenant) error
	// GetTenant retrieves a tenant by ID.
	GetTenant(ctx context.Context, id string) (*state.Tenant, error)
	// GetTenantBySlug retrieves a tenant by slug.
	GetTenantBySlug(ctx context.Context, slug string) (*state.Tenant, error)
	// ListTenants returns all tenants.
	ListTenants(ctx context.Context) ([]*state.Tenant, error)
	// UpdateTenant replaces mutable fields (name, slug) on an existing tenant.
	UpdateTenant(ctx context.Context, t *state.Tenant) error
	// DeleteTenant removes a tenant by ID.
	DeleteTenant(ctx context.Context, id string) error
}

// ScopeStore manages scope persistence.
type ScopeStore interface {
	// CreateScope inserts a new scope.
	CreateScope(ctx context.Context, s *state.Scope) error
	// GetScope retrieves a scope by ID.
	GetScope(ctx context.Context, id string) (*state.Scope, error)
	// ListScopes returns all scopes for a tenant.
	ListScopes(ctx context.Context, tenantID string) ([]*state.Scope, error)
	// UpdateScope replaces mutable fields (name, slug) on an existing scope.
	UpdateScope(ctx context.Context, s *state.Scope) error
	// DeleteScope removes a scope by ID.
	DeleteScope(ctx context.Context, id string) error
}
