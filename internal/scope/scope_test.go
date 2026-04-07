package scope_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/scope"
	"github.com/go-orca/go-orca/internal/state"
)

// ─── In-memory ScopeStore ─────────────────────────────────────────────────────

type memScopeStore struct {
	mu     sync.RWMutex
	scopes map[string]*state.Scope
}

func newMemScopeStore() *memScopeStore {
	return &memScopeStore{scopes: make(map[string]*state.Scope)}
}

func (m *memScopeStore) CreateScope(_ context.Context, s *state.Scope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.scopes[s.ID] = &cp
	return nil
}

func (m *memScopeStore) GetScope(_ context.Context, id string) (*state.Scope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.scopes[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *s
	return &cp, nil
}

func (m *memScopeStore) ListScopes(_ context.Context, tenantID string) ([]*state.Scope, error) {
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

func (m *memScopeStore) UpdateScope(_ context.Context, s *state.Scope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scopes[s.ID]; !ok {
		return errors.New("not found")
	}
	cp := *s
	m.scopes[s.ID] = &cp
	return nil
}

func (m *memScopeStore) DeleteScope(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scopes[id]; !ok {
		return errors.New("not found")
	}
	delete(m.scopes, id)
	return nil
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestCreateAndGetScope(t *testing.T) {
	svc := scope.New(newMemScopeStore())
	ctx := context.Background()

	sc, err := svc.Create(ctx, "t1", state.ScopeKindGlobal, "Global", "global", "")
	if err != nil {
		t.Fatalf("Create global: %v", err)
	}
	if sc.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := svc.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != sc.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, sc.ID)
	}
}

func TestCreateScopeHierarchy(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	global, err := svc.Create(ctx, "t1", state.ScopeKindGlobal, "Global", "global", "")
	if err != nil {
		t.Fatalf("Create global: %v", err)
	}

	org, err := svc.Create(ctx, "t1", state.ScopeKindOrg, "Eng", "eng", global.ID)
	if err != nil {
		t.Fatalf("Create org under global: %v", err)
	}

	_, err = svc.Create(ctx, "t1", state.ScopeKindTeam, "Backend", "backend", org.ID)
	if err != nil {
		t.Fatalf("Create team under org: %v", err)
	}
}

func TestValidateHierarchyRejectsWrongParentKind(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	// Create a global scope.
	global, _ := svc.Create(ctx, "t1", state.ScopeKindGlobal, "G", "global", "")

	// Team directly under global (should fail — team needs org parent).
	_, err := svc.Create(ctx, "t1", state.ScopeKindTeam, "Dev", "dev", global.ID)
	if err == nil {
		t.Fatal("expected error creating team under global, got nil")
	}
}

func TestValidateHierarchyGlobalNoParent(t *testing.T) {
	svc := scope.New(newMemScopeStore())
	ctx := context.Background()

	// Global with non-empty parentID is invalid.
	_, err := svc.Create(ctx, "t1", state.ScopeKindGlobal, "G", "global", "some-id")
	if err == nil {
		t.Fatal("expected error for global with parent, got nil")
	}
}

func TestListScopes(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	global, _ := svc.Create(ctx, "t1", state.ScopeKindGlobal, "G", "global", "")
	_, _ = svc.Create(ctx, "t1", state.ScopeKindOrg, "Org", "org", global.ID)

	scopes, err := svc.List(ctx, "t1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(scopes) != 2 {
		t.Errorf("got %d scopes, want 2", len(scopes))
	}
}

func TestUpdateScope(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	sc, _ := svc.Create(ctx, "t1", state.ScopeKindGlobal, "Orig", "orig", "")

	updated, err := svc.Update(ctx, sc.ID, "Renamed", "renamed")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("Name: got %q, want %q", updated.Name, "Renamed")
	}
	if updated.Slug != "renamed" {
		t.Errorf("Slug: got %q, want %q", updated.Slug, "renamed")
	}
}

func TestDeleteScope(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	sc, _ := svc.Create(ctx, "t1", state.ScopeKindGlobal, "G", "global", "")

	if err := svc.Delete(ctx, sc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, sc.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestResolveChain(t *testing.T) {
	st := newMemScopeStore()
	svc := scope.New(st)
	ctx := context.Background()

	global, _ := svc.Create(ctx, "t1", state.ScopeKindGlobal, "G", "global", "")
	org, _ := svc.Create(ctx, "t1", state.ScopeKindOrg, "Org", "org", global.ID)
	team, _ := svc.Create(ctx, "t1", state.ScopeKindTeam, "Team", "team", org.ID)

	chain, err := svc.ResolveChain(ctx, team.ID)
	if err != nil {
		t.Fatalf("ResolveChain: %v", err)
	}
	if len(chain) != 3 {
		t.Errorf("chain length: got %d, want 3", len(chain))
	}
	if chain[0].ID != team.ID {
		t.Errorf("chain[0]: got %q, want team %q", chain[0].ID, team.ID)
	}
	if chain[2].ID != global.ID {
		t.Errorf("chain[2]: got %q, want global %q", chain[2].ID, global.ID)
	}
}

// Ensure memScopeStore satisfies the interface at compile time.
var _ interface {
	CreateScope(context.Context, *state.Scope) error
	GetScope(context.Context, string) (*state.Scope, error)
	ListScopes(context.Context, string) ([]*state.Scope, error)
	UpdateScope(context.Context, *state.Scope) error
	DeleteScope(context.Context, string) error
} = (*memScopeStore)(nil)

// keep time import used
var _ = time.Now
