// Package tenant provides CRUD operations and bootstrap helpers for tenants.
package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
	"github.com/google/uuid"
)

// Service handles tenant lifecycle operations.
type Service struct {
	store storage.TenantStore
}

// New returns a new tenant Service.
func New(store storage.TenantStore) *Service {
	return &Service{store: store}
}

// Create creates a new tenant with a generated ID.
func (s *Service) Create(ctx context.Context, slug, name string) (*state.Tenant, error) {
	if slug == "" {
		return nil, fmt.Errorf("tenant: slug is required")
	}
	if name == "" {
		name = slug
	}
	now := time.Now().UTC()
	t := &state.Tenant{
		ID:        uuid.New().String(),
		Slug:      slug,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateTenant(ctx, t); err != nil {
		return nil, fmt.Errorf("tenant: create: %w", err)
	}
	return t, nil
}

// Get retrieves a tenant by ID.
func (s *Service) Get(ctx context.Context, id string) (*state.Tenant, error) {
	t, err := s.store.GetTenant(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("tenant: get %s: %w", id, err)
	}
	return t, nil
}

// GetBySlug retrieves a tenant by slug.
func (s *Service) GetBySlug(ctx context.Context, slug string) (*state.Tenant, error) {
	t, err := s.store.GetTenantBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("tenant: get by slug %s: %w", slug, err)
	}
	return t, nil
}

// List returns all tenants.
func (s *Service) List(ctx context.Context) ([]*state.Tenant, error) {
	ts, err := s.store.ListTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("tenant: list: %w", err)
	}
	return ts, nil
}

// Update updates the name and slug of an existing tenant.
func (s *Service) Update(ctx context.Context, id, slug, name string) (*state.Tenant, error) {
	t, err := s.store.GetTenant(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("tenant: update %s: not found: %w", id, err)
	}
	if slug != "" {
		t.Slug = slug
	}
	if name != "" {
		t.Name = name
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateTenant(ctx, t); err != nil {
		return nil, fmt.Errorf("tenant: update %s: %w", id, err)
	}
	return t, nil
}

// Delete removes a tenant by ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.DeleteTenant(ctx, id); err != nil {
		return fmt.Errorf("tenant: delete %s: %w", id, err)
	}
	return nil
}

// EnsureDefault ensures a default tenant + global scope exist for homelab mode.
// Returns the tenant and its global scope ID.
func EnsureDefault(ctx context.Context, store storage.Store) (*state.Tenant, *state.Scope, error) {
	svc := New(store)

	t, err := svc.GetBySlug(ctx, "default")
	if err != nil {
		// Doesn't exist yet — create it.
		t, err = svc.Create(ctx, "default", "Default Tenant")
		if err != nil {
			return nil, nil, fmt.Errorf("ensureDefault tenant: %w", err)
		}
	}

	// Ensure global scope.
	scopes, err := store.ListScopes(ctx, t.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureDefault scopes: %w", err)
	}
	for _, sc := range scopes {
		if sc.Kind == state.ScopeKindGlobal {
			return t, sc, nil
		}
	}

	// Create the global scope.
	now := time.Now().UTC()
	sc := &state.Scope{
		ID:        uuid.New().String(),
		TenantID:  t.ID,
		Kind:      state.ScopeKindGlobal,
		Name:      "global",
		Slug:      "global",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateScope(ctx, sc); err != nil {
		return nil, nil, fmt.Errorf("ensureDefault global scope: %w", err)
	}
	return t, sc, nil
}
