// Package scope provides CRUD and resolution logic for the go-orca scope hierarchy.
//
// Resolution order (highest to lowest precedence):
//
//	workflow/repo → team → org → global → builtin
//
// Constraints enforced by this package:
//   - team MUST have an org parent
//   - org MUST have a global parent
//   - global has no parent
package scope

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
	"github.com/google/uuid"
)

// Service handles scope lifecycle and resolution.
type Service struct {
	store storage.ScopeStore
}

// New returns a new scope Service.
func New(store storage.ScopeStore) *Service {
	return &Service{store: store}
}

// Create creates a new scope, enforcing hierarchy constraints.
func (s *Service) Create(ctx context.Context, tenantID string, kind state.ScopeKind, name, slug, parentID string) (*state.Scope, error) {
	if err := s.validateHierarchy(ctx, kind, parentID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sc := &state.Scope{
		ID:            uuid.New().String(),
		TenantID:      tenantID,
		Kind:          kind,
		Name:          name,
		Slug:          slug,
		ParentScopeID: parentID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateScope(ctx, sc); err != nil {
		return nil, fmt.Errorf("scope: create: %w", err)
	}
	return sc, nil
}

// Get retrieves a scope by ID.
func (s *Service) Get(ctx context.Context, id string) (*state.Scope, error) {
	sc, err := s.store.GetScope(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scope: get %s: %w", id, err)
	}
	return sc, nil
}

// List returns all scopes for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]*state.Scope, error) {
	scs, err := s.store.ListScopes(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("scope: list: %w", err)
	}
	return scs, nil
}

// Update updates the name and slug of an existing scope.
func (s *Service) Update(ctx context.Context, id, name, slug string) (*state.Scope, error) {
	sc, err := s.store.GetScope(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scope: update %s: not found: %w", id, err)
	}
	if name != "" {
		sc.Name = name
	}
	if slug != "" {
		sc.Slug = slug
	}
	sc.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateScope(ctx, sc); err != nil {
		return nil, fmt.Errorf("scope: update %s: %w", id, err)
	}
	return sc, nil
}

// Delete removes a scope by ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.DeleteScope(ctx, id); err != nil {
		return fmt.Errorf("scope: delete %s: %w", id, err)
	}
	return nil
}

// ResolveChain returns the ancestor chain for a scope in resolution order
// (scope itself → parent → ... → global).  Used by the customization registry
// to merge settings with correct precedence.
func (s *Service) ResolveChain(ctx context.Context, scopeID string) ([]*state.Scope, error) {
	var chain []*state.Scope
	id := scopeID
	seen := map[string]bool{}

	for id != "" {
		if seen[id] {
			return nil, fmt.Errorf("scope: cycle detected at %s", id)
		}
		seen[id] = true

		sc, err := s.store.GetScope(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("scope: resolve chain: %w", err)
		}
		chain = append(chain, sc)
		id = sc.ParentScopeID
	}
	return chain, nil
}

// validateHierarchy enforces scope parentage rules, including verifying that
// the parent exists and has the correct kind.
func (s *Service) validateHierarchy(ctx context.Context, kind state.ScopeKind, parentID string) error {
	switch kind {
	case state.ScopeKindGlobal:
		if parentID != "" {
			return fmt.Errorf("scope: global scope must not have a parent")
		}
	case state.ScopeKindOrg:
		if parentID == "" {
			return fmt.Errorf("scope: org scope requires a global parent")
		}
		parent, err := s.store.GetScope(ctx, parentID)
		if err != nil {
			return fmt.Errorf("scope: org parent not found: %w", err)
		}
		if parent.Kind != state.ScopeKindGlobal {
			return fmt.Errorf("scope: org parent must be kind %q, got %q", state.ScopeKindGlobal, parent.Kind)
		}
	case state.ScopeKindTeam:
		if parentID == "" {
			return fmt.Errorf("scope: team scope requires an org parent")
		}
		parent, err := s.store.GetScope(ctx, parentID)
		if err != nil {
			return fmt.Errorf("scope: team parent not found: %w", err)
		}
		if parent.Kind != state.ScopeKindOrg {
			return fmt.Errorf("scope: team parent must be kind %q, got %q", state.ScopeKindOrg, parent.Kind)
		}
	default:
		return fmt.Errorf("scope: unknown kind %q", kind)
	}
	return nil
}
