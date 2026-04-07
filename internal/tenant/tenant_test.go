package tenant_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tenant"
)

// ─── In-memory TenantStore ────────────────────────────────────────────────────

type memTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*state.Tenant
}

func newMemTenantStore() *memTenantStore {
	return &memTenantStore{tenants: make(map[string]*state.Tenant)}
}

func (m *memTenantStore) CreateTenant(_ context.Context, t *state.Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.tenants[t.ID] = &cp
	return nil
}

func (m *memTenantStore) GetTenant(_ context.Context, id string) (*state.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *t
	return &cp, nil
}

func (m *memTenantStore) GetTenantBySlug(_ context.Context, slug string) (*state.Tenant, error) {
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

func (m *memTenantStore) ListTenants(_ context.Context) ([]*state.Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*state.Tenant, 0, len(m.tenants))
	for _, t := range m.tenants {
		cp := *t
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memTenantStore) UpdateTenant(_ context.Context, t *state.Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenants[t.ID]; !ok {
		return errors.New("not found")
	}
	cp := *t
	m.tenants[t.ID] = &cp
	return nil
}

func (m *memTenantStore) DeleteTenant(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenants[id]; !ok {
		return errors.New("not found")
	}
	delete(m.tenants, id)
	return nil
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestCreateAndGetTenant(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	created, err := svc.Create(ctx, "acme", "Acme Corp")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if created.Slug != "acme" {
		t.Errorf("Slug: got %q, want %q", created.Slug, "acme")
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestCreateTenantRequiresSlug(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	_, err := svc.Create(context.Background(), "", "No Slug")
	if err == nil {
		t.Fatal("expected error for empty slug, got nil")
	}
}

func TestCreateTenantDefaultsNameToSlug(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	created, err := svc.Create(context.Background(), "myslug", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "myslug" {
		t.Errorf("Name defaulted to slug: got %q, want %q", created.Name, "myslug")
	}
}

func TestListTenants(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	_, _ = svc.Create(ctx, "a", "A")
	_, _ = svc.Create(ctx, "b", "B")

	all, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List: got %d, want 2", len(all))
	}
}

func TestUpdateTenant(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	created, _ := svc.Create(ctx, "orig-slug", "Orig Name")

	updated, err := svc.Update(ctx, created.ID, "", "New Name")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("Name: got %q, want %q", updated.Name, "New Name")
	}
	if updated.Slug != "orig-slug" {
		t.Errorf("Slug unchanged: got %q, want %q", updated.Slug, "orig-slug")
	}
}

func TestUpdateTenantSlug(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	created, _ := svc.Create(ctx, "old", "Old")
	updated, err := svc.Update(ctx, created.ID, "new", "")
	if err != nil {
		t.Fatalf("Update slug: %v", err)
	}
	if updated.Slug != "new" {
		t.Errorf("Slug: got %q, want %q", updated.Slug, "new")
	}
}

func TestDeleteTenant(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	created, _ := svc.Create(ctx, "bye", "Bye")

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, created.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestGetBySlug(t *testing.T) {
	svc := tenant.New(newMemTenantStore())
	ctx := context.Background()

	_, _ = svc.Create(ctx, "find-me", "Find Me")

	t2, err := svc.GetBySlug(ctx, "find-me")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if t2.Slug != "find-me" {
		t.Errorf("Slug: got %q", t2.Slug)
	}
}
