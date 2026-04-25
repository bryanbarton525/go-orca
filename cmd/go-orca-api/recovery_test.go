package main

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

func TestReconcileInterruptedWorkflowsMarksRunningWorkflowsFailed(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	activeTaskID := "task-running"
	store := &recoveryTestStore{
		tenants: []*state.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", CreatedAt: now, UpdatedAt: now}},
		workflowsByTenant: map[string][]*state.WorkflowState{
			"tenant-a": {
				{
					ID:        "workflow-running",
					TenantID:  "tenant-a",
					ScopeID:   "scope-a",
					Status:    state.WorkflowStatusRunning,
					CreatedAt: now.Add(-2 * time.Minute),
					UpdatedAt: now.Add(-1 * time.Minute),
					Execution: state.Execution{CurrentPersona: state.PersonaPod, ActiveTaskID: activeTaskID, ActiveTaskTitle: "Write summary", WorkflowKind: "standard"},
					Tasks: []state.Task{{
						ID:         activeTaskID,
						WorkflowID: "workflow-running",
						Title:      "Write summary",
						AssignedTo: state.PersonaPod,
						Status:     state.TaskStatusRunning,
						CreatedAt:  now.Add(-90 * time.Second),
					}},
				},
				{
					ID:        "workflow-completed",
					TenantID:  "tenant-a",
					ScopeID:   "scope-a",
					Status:    state.WorkflowStatusCompleted,
					CreatedAt: now.Add(-5 * time.Minute),
					UpdatedAt: now.Add(-4 * time.Minute),
				},
			},
		},
	}

	recovered, err := reconcileInterruptedWorkflows(context.Background(), store, zap.NewNop())
	if err != nil {
		t.Fatalf("reconcileInterruptedWorkflows returned error: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered workflow, got %d", recovered)
	}

	workflow := store.workflows["workflow-running"]
	if workflow.Status != state.WorkflowStatusFailed {
		t.Fatalf("expected running workflow to be failed, got %s", workflow.Status)
	}
	if workflow.ErrorMessage != interruptedWorkflowError {
		t.Fatalf("expected interrupted error message %q, got %q", interruptedWorkflowError, workflow.ErrorMessage)
	}
	if workflow.Execution.CurrentPersona != "" || workflow.Execution.ActiveTaskID != "" || workflow.Execution.ActiveTaskTitle != "" {
		t.Fatalf("expected execution state to be cleared, got %+v", workflow.Execution)
	}
	if workflow.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
	if workflow.Tasks[0].Status != state.TaskStatusFailed {
		t.Fatalf("expected active task to be failed, got %s", workflow.Tasks[0].Status)
	}
	if workflow.Tasks[0].CompletedAt == nil {
		t.Fatal("expected interrupted task completed_at to be set")
	}

	var sawTaskFailed bool
	var sawPersonaFailed bool
	var sawTransition bool
	for _, evt := range store.appendedEvents {
		switch evt.Type {
		case events.EventTaskFailed:
			sawTaskFailed = true
		case events.EventPersonaFailed:
			sawPersonaFailed = true
		case events.EventStateTransition:
			sawTransition = true
		}
	}
	if !sawTaskFailed || !sawPersonaFailed || !sawTransition {
		t.Fatalf("expected recovery events taskFailed=%t personaFailed=%t transition=%t", sawTaskFailed, sawPersonaFailed, sawTransition)
	}

	if got := store.workflows["workflow-completed"].Status; got != state.WorkflowStatusCompleted {
		t.Fatalf("expected completed workflow to remain completed, got %s", got)
	}
}

type recoveryTestStore struct {
	tenants           []*state.Tenant
	workflowsByTenant map[string][]*state.WorkflowState
	workflows         map[string]*state.WorkflowState
	appendedEvents    []*events.Event
	loaded            bool
}

func (s *recoveryTestStore) load() {
	if s.loaded {
		return
	}
	s.workflows = make(map[string]*state.WorkflowState)
	for tenantID, workflows := range s.workflowsByTenant {
		copied := make([]*state.WorkflowState, 0, len(workflows))
		for _, workflow := range workflows {
			clone := *workflow
			clone.Tasks = append([]state.Task(nil), workflow.Tasks...)
			copied = append(copied, &clone)
			s.workflows[clone.ID] = &clone
		}
		s.workflowsByTenant[tenantID] = copied
	}
	s.loaded = true
}

func (s *recoveryTestStore) CreateWorkflow(context.Context, *state.WorkflowState) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) GetWorkflow(_ context.Context, id string) (*state.WorkflowState, error) {
	s.load()
	workflow, ok := s.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow %s not found", id)
	}
	return workflow, nil
}

func (s *recoveryTestStore) SaveWorkflow(_ context.Context, ws *state.WorkflowState) error {
	s.load()
	s.workflows[ws.ID] = ws
	workflows := s.workflowsByTenant[ws.TenantID]
	for index, workflow := range workflows {
		if workflow.ID == ws.ID {
			workflows[index] = ws
			return nil
		}
	}
	s.workflowsByTenant[ws.TenantID] = append(s.workflowsByTenant[ws.TenantID], ws)
	return nil
}

func (s *recoveryTestStore) ListWorkflows(_ context.Context, tenantID string, limit, offset int) ([]*state.WorkflowState, error) {
	s.load()
	workflows := append([]*state.WorkflowState(nil), s.workflowsByTenant[tenantID]...)
	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].CreatedAt.After(workflows[j].CreatedAt)
	})
	if offset >= len(workflows) {
		return []*state.WorkflowState{}, nil
	}
	end := offset + limit
	if end > len(workflows) {
		end = len(workflows)
	}
	return workflows[offset:end], nil
}

func (s *recoveryTestStore) UpdateWorkflowStatus(context.Context, string, state.WorkflowStatus, string) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) AppendEvents(_ context.Context, evts ...*events.Event) error {
	s.appendedEvents = append(s.appendedEvents, evts...)
	return nil
}

func (s *recoveryTestStore) ListEvents(context.Context, string) ([]*events.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) ListEventsByType(context.Context, string, events.EventType) ([]*events.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) EventsSince(context.Context, string, time.Time) ([]*events.Event, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) CreateTenant(context.Context, *state.Tenant) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) GetTenant(context.Context, string) (*state.Tenant, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) GetTenantBySlug(context.Context, string) (*state.Tenant, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) ListTenants(context.Context) ([]*state.Tenant, error) {
	return s.tenants, nil
}

func (s *recoveryTestStore) UpdateTenant(context.Context, *state.Tenant) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) DeleteTenant(context.Context, string) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) CreateScope(context.Context, *state.Scope) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) GetScope(context.Context, string) (*state.Scope, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) ListScopes(context.Context, string) ([]*state.Scope, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) UpdateScope(context.Context, *state.Scope) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) DeleteScope(context.Context, string) error {
	return fmt.Errorf("not implemented")
}

func (s *recoveryTestStore) Ping(context.Context) error {
	return nil
}

func (s *recoveryTestStore) Close() error {
	return nil
}

// ── AttachmentStore stubs ────────────────────────────────────────────────────

func (s *recoveryTestStore) CreateUploadSession(context.Context, *state.UploadSession) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) GetUploadSession(context.Context, string) (*state.UploadSession, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) ConsumeUploadSession(context.Context, string, string, string) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) AbortUploadSession(context.Context, string, string) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) CreateAttachment(context.Context, *state.Attachment) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) GetAttachment(context.Context, string) (*state.Attachment, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) ListAttachmentsBySession(context.Context, string) ([]*state.Attachment, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) ListAttachmentsByWorkflow(context.Context, string) ([]*state.Attachment, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) UpdateAttachmentStatus(context.Context, string, state.AttachmentStatus, string, int, string) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) CreateAttachmentChunks(context.Context, []state.AttachmentChunk) error {
	return fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) GetAttachmentChunk(context.Context, string, int) (*state.AttachmentChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *recoveryTestStore) ListAttachmentChunks(context.Context, string) ([]state.AttachmentChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
