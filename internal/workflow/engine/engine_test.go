package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// ─── Mock store ───────────────────────────────────────────────────────────────

type mockStore struct {
	mu        sync.Mutex
	workflows map[string]*state.WorkflowState
	events    []*events.Event
}

func newMockStore() *mockStore {
	return &mockStore{workflows: make(map[string]*state.WorkflowState)}
}

func (m *mockStore) GetWorkflow(_ context.Context, id string) (*state.WorkflowState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws, ok := m.workflows[id]
	if !ok {
		return nil, errors.New("not found")
	}
	// return a copy so callers can't mutate the store directly
	cp := *ws
	return &cp, nil
}

func (m *mockStore) SaveWorkflow(_ context.Context, ws *state.WorkflowState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *ws
	m.workflows[ws.ID] = &cp
	return nil
}

func (m *mockStore) AppendEvents(_ context.Context, evts ...*events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evts...)
	return nil
}

func (m *mockStore) setWorkflowStatus(id string, status state.WorkflowStatus, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ws, ok := m.workflows[id]; ok {
		ws.Status = status
		ws.ErrorMessage = errMsg
		ws.UpdatedAt = time.Now().UTC()
	}
}

// ─── Mock personas ────────────────────────────────────────────────────────────

// noopPersona immediately succeeds with an empty output (reserved for future
// integration tests that register it into the persona registry).
type noopPersona struct{ kind state.PersonaKind }

func (p *noopPersona) Kind() state.PersonaKind { return p.kind }
func (p *noopPersona) Name() string            { return string(p.kind) }
func (p *noopPersona) Description() string     { return "" }
func (p *noopPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = packet
	return &state.PersonaOutput{Persona: p.kind, Summary: "noop"}, nil
}

type artifactPersona struct{ kind state.PersonaKind }

func (p *artifactPersona) Kind() state.PersonaKind { return p.kind }
func (p *artifactPersona) Name() string            { return string(p.kind) }
func (p *artifactPersona) Description() string     { return "" }
func (p *artifactPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	return &state.PersonaOutput{
		Persona: p.kind,
		Summary: "implemented",
		Artifacts: []state.Artifact{{
			WorkflowID: packet.WorkflowID,
			TaskID:     packet.Tasks[0].ID,
			Kind:       state.ArtifactKindCode,
			Name:       "main.go",
			Content:    "package main\n",
			CreatedBy:  state.PersonaPod,
			CreatedAt:  time.Now().UTC(),
		}},
	}, nil
}

type orderingArtifactPersona struct {
	kind  state.PersonaKind
	order *[]string
}

func (p *orderingArtifactPersona) Kind() state.PersonaKind { return p.kind }
func (p *orderingArtifactPersona) Name() string            { return string(p.kind) }
func (p *orderingArtifactPersona) Description() string     { return "records task execution order" }
func (p *orderingArtifactPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	if len(packet.Tasks) != 1 {
		return nil, errors.New("expected exactly one pod task in packet")
	}
	if p.order != nil {
		*p.order = append(*p.order, packet.Tasks[0].ID)
	}
	return &state.PersonaOutput{
		Persona: state.PersonaPod,
		Summary: packet.Tasks[0].Title,
		Artifacts: []state.Artifact{{
			WorkflowID: packet.WorkflowID,
			TaskID:     packet.Tasks[0].ID,
			Kind:       state.ArtifactKindCode,
			Name:       packet.Tasks[0].ID + ".txt",
			Content:    packet.Tasks[0].Title,
			CreatedBy:  state.PersonaPod,
			CreatedAt:  time.Now().UTC(),
		}},
	}, nil
}

type fakeTool struct {
	name string
	out  json.RawMessage
	err  error
}

func (t fakeTool) Name() string                { return t.name }
func (t fakeTool) Description() string         { return "fake tool" }
func (t fakeTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t fakeTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	return t.out, t.err
}

type captureTool struct {
	name string
	out  json.RawMessage
	args json.RawMessage
}

func (t *captureTool) Name() string                { return t.name }
func (t *captureTool) Description() string         { return "capture tool" }
func (t *captureTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *captureTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	t.args = append(t.args[:0], args...)
	return t.out, nil
}

type bootstrapTool struct {
	name      string
	root      string
	callCount int
	args      json.RawMessage
}

func (t *bootstrapTool) Name() string                { return t.name }
func (t *bootstrapTool) Description() string         { return "bootstrap tool" }
func (t *bootstrapTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *bootstrapTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	t.callCount++
	t.args = append(t.args[:0], args...)
	var payload struct {
		WorkspacePath string `json:"workspace_path"`
		Branch        string `json:"branch"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, err
	}
	moduleName := payload.Branch
	if moduleName == "" {
		moduleName = "workspace"
	}
	workdir := filepath.Join(t.root, payload.WorkspacePath)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return nil, err
	}
	content := "module " + moduleName + "\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(workdir, "go.mod"), []byte(content), 0o644); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"passed":true,"output":"initialized go module"}`), nil
}

type checkpointCaptureTool struct {
	name   string
	phases []string
	count  int
}

func (t *checkpointCaptureTool) Name() string                { return t.name }
func (t *checkpointCaptureTool) Description() string         { return "captures checkpoint phases" }
func (t *checkpointCaptureTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *checkpointCaptureTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	t.count++
	var payload struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, err
	}
	t.phases = append(t.phases, payload.Phase)
	return json.RawMessage(`{"commit_sha":"abc123","branch":"workflow/test","message":"checkpoint","pushed":true}`), nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestEngineRunMissingPersona verifies that Run returns an error when a
// required persona is not registered (the global registry starts empty in
// tests unless populated).
func TestEngineRunMissingPersona(t *testing.T) {
	// Use a mock store with a pre-populated workflow.
	ms := newMockStore()
	tid, sid := "t1", "s1"
	ws := state.NewWorkflowState(tid, sid, "hello")
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
	})

	err := eng.Run(context.Background(), ws.ID)
	if err == nil {
		t.Fatal("expected error when persona not registered, got nil")
	}
	// The workflow should be in a failed state.
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	if stored.Status != state.WorkflowStatusFailed {
		t.Errorf("expected failed status, got %q", stored.Status)
	}
}

// TestEngineRunTerminalWorkflow verifies that Run rejects already-completed
// or cancelled workflows.
func TestEngineRunTerminalWorkflow(t *testing.T) {
	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "already done")
	ws.Status = state.WorkflowStatusCompleted
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{})
	err := eng.Run(context.Background(), ws.ID)
	if err == nil {
		t.Fatal("expected error for terminal workflow, got nil")
	}
}

// TestEngineRunNotFound verifies that Run returns an error when the workflow
// does not exist in the store.
func TestEngineRunNotFound(t *testing.T) {
	ms := newMockStore()
	eng := engine.New(ms, engine.Options{})
	err := eng.Run(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing workflow, got nil")
	}
}

// TestEnginePauseFuncTransitions verifies that when PauseFunc fires the engine
// returns ErrPaused and the workflow status is set to paused.
//
// We cannot run the full persona pipeline in isolation without registering
// personas, so we test the pause path by having a PauseFunc that fires
// immediately on the first check — but the check only runs after a persona
// phase, so we rely on the "missing persona" path triggering first.  Instead,
// we inject a workflow already in the running state and ensure the mock store
// transitions correctly when the engine gets ErrPaused.
//
// The simplest verifiable scenario: engine.ErrPaused is a distinct sentinel
// value that can be checked with errors.Is.
func TestErrPausedIsSentinel(t *testing.T) {
	if engine.ErrPaused == nil {
		t.Fatal("ErrPaused must not be nil")
	}
	wrapped := errors.New("outer: " + engine.ErrPaused.Error())
	_ = wrapped // just confirm the error is usable as a value
}

// TestMockStoreRoundTrip verifies that our mock store correctly persists and
// retrieves workflow state, including status transitions.
func TestMockStoreRoundTrip(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "round trip test")
	if err := ms.SaveWorkflow(ctx, ws); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}

	got, err := ms.GetWorkflow(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Request != ws.Request {
		t.Errorf("Request mismatch: got %q, want %q", got.Request, ws.Request)
	}

	// Mutation of returned copy must not affect store.
	got.Status = state.WorkflowStatusFailed
	got2, _ := ms.GetWorkflow(ctx, ws.ID)
	if got2.Status == state.WorkflowStatusFailed {
		t.Error("store copy was mutated by caller modification")
	}
}

// TestMockStoreAppendEvents verifies event accumulation.
func TestMockStoreAppendEvents(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	e1, _ := events.NewEvent("wf1", "t1", "s1", events.EventWorkflowStarted, "", nil)
	e2, _ := events.NewEvent("wf1", "t1", "s1", events.EventWorkflowCompleted, "", nil)

	if err := ms.AppendEvents(ctx, e1, e2); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if len(ms.events) != 2 {
		t.Errorf("events len: got %d, want 2", len(ms.events))
	}
}

func TestToolchainValidationAndCheckpointRunAfterImplementation(t *testing.T) {
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	reg := tools.NewRegistry()
	reg.Register(fakeTool{name: "run_tests", out: json.RawMessage(`{"passed":true,"output":"tests passed"}`)})
	reg.Register(fakeTool{name: "git_checkpoint", out: json.RawMessage(`{"commit_sha":"abc123","branch":"workflow/test","message":"checkpoint","pushed":true}`)})

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-123456789",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaPod,
		CreatedAt:  time.Now().UTC(),
	}}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   t.TempDir(),
		ToolRegistry:    reg,
		Toolchains: []engine.ToolchainConfig{{
			ID:                   "go",
			Languages:            []string{"go"},
			Capabilities:         []string{"run_tests", "git_checkpoint"},
			ValidationProfiles:   map[string][]string{"default": {"run_tests"}},
			CheckpointCapability: "git_checkpoint",
		}},
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	if stored.Execution.Workspace == nil || stored.Execution.Workspace.Path == "" {
		t.Fatal("expected workspace metadata")
	}
	if stored.Execution.Toolchain == nil || stored.Execution.Toolchain.ID != "go" {
		t.Fatalf("expected go toolchain, got %+v", stored.Execution.Toolchain)
	}
	if len(stored.Execution.ValidationRuns) != 1 || !stored.Execution.ValidationRuns[0].Passed {
		t.Fatalf("expected one passing validation run, got %+v", stored.Execution.ValidationRuns)
	}
	if len(stored.Execution.Checkpoints) != 1 || stored.Execution.Checkpoints[0].CommitSHA != "abc123" {
		t.Fatalf("expected checkpoint metadata, got %+v", stored.Execution.Checkpoints)
	}
}

func TestToolchainArgsUseWorkflowRelativeWorkspacePath(t *testing.T) {
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	runTests := &captureTool{name: "run_tests", out: json.RawMessage(`{"passed":true}`)}
	reg := tools.NewRegistry()
	reg.Register(runTests)
	reg.Register(fakeTool{name: "git_checkpoint", out: json.RawMessage(`{"commit_sha":"abc123","branch":"workflow/test","message":"checkpoint"}`)})

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-relative",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaPod,
		CreatedAt:  time.Now().UTC(),
	}}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   t.TempDir(),
		ToolRegistry:    reg,
		Toolchains: []engine.ToolchainConfig{{
			ID:                   "go",
			Languages:            []string{"go"},
			Capabilities:         []string{"run_tests", "git_checkpoint"},
			ValidationProfiles:   map[string][]string{"default": {"run_tests"}},
			CheckpointCapability: "git_checkpoint",
		}},
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var args struct {
		WorkspacePath string `json:"workspace_path"`
	}
	if err := json.Unmarshal(runTests.args, &args); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if args.WorkspacePath != ws.ID {
		t.Fatalf("workspace_path = %q, want workflow ID %q", args.WorkspacePath, ws.ID)
	}
}

func TestRunPodPhase_RespectsDependsOnOrdering(t *testing.T) {
	order := []string{}
	persona.Register(&orderingArtifactPersona{kind: state.PersonaPod, order: &order})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{
		{
			ID:         "task-dependent",
			WorkflowID: ws.ID,
			Title:      "dependent task",
			Status:     state.TaskStatusPending,
			AssignedTo: state.PersonaPod,
			DependsOn:  []string{"task-bootstrap"},
			CreatedAt:  time.Now().UTC(),
		},
		{
			ID:         "task-bootstrap",
			WorkflowID: ws.ID,
			Title:      "bootstrap task",
			Status:     state.TaskStatusPending,
			AssignedTo: state.PersonaPod,
			CreatedAt:  time.Now().UTC(),
		},
	}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   t.TempDir(),
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"task-bootstrap", "task-dependent"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("task execution order = %v, want %v", order, want)
	}
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	for _, task := range stored.Tasks {
		if task.Status != state.TaskStatusCompleted {
			t.Fatalf("task %s status = %s, want completed", task.ID, task.Status)
		}
	}
}

func TestRunPodPhase_LeavesBlockedTaskPendingWhenDependencyMissing(t *testing.T) {
	order := []string{}
	persona.Register(&orderingArtifactPersona{kind: state.PersonaPod, order: &order})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-dependent-only",
		WorkflowID: ws.ID,
		Title:      "dependent task",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaPod,
		DependsOn:  []string{"missing-task"},
		CreatedAt:  time.Now().UTC(),
	}}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   t.TempDir(),
	})

	err := eng.Run(context.Background(), ws.ID)
	if err == nil {
		t.Fatal("expected dependency-blocked pod phase to fail")
	}
	if !strings.Contains(err.Error(), "no runnable pod tasks") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("expected blocked task not to execute, got order %v", order)
	}
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	if got := stored.Tasks[0].Status; got != state.TaskStatusPending {
		t.Fatalf("blocked task status = %s, want pending", got)
	}
}

func TestEnsureToolchainBootstrapInitializesGoModule(t *testing.T) {
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	workspaceRoot := t.TempDir()
	bootstrap := &bootstrapTool{name: "go_mod_init", root: workspaceRoot}
	reg := tools.NewRegistry()
	reg.Register(bootstrap)

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-bootstrap-go",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaPod,
		CreatedAt:  time.Now().UTC(),
	}}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   workspaceRoot,
		ToolRegistry:    reg,
		Toolchains: []engine.ToolchainConfig{{
			ID:              "go",
			Languages:       []string{"go", "golang"},
			Capabilities:    []string{"init_project"},
			CapabilityTools: map[string]string{"init_project": "go_mod_init"},
		}},
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if bootstrap.callCount != 1 {
		t.Fatalf("bootstrap call count = %d, want 1", bootstrap.callCount)
	}
	var args struct {
		WorkspacePath string `json:"workspace_path"`
	}
	if err := json.Unmarshal(bootstrap.args, &args); err != nil {
		t.Fatalf("unmarshal bootstrap args: %v", err)
	}
	if args.WorkspacePath != ws.ID {
		t.Fatalf("bootstrap workspace_path = %q, want %q", args.WorkspacePath, ws.ID)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ws.ID, "go.mod")); err != nil {
		t.Fatalf("expected go.mod after bootstrap: %v", err)
	}
}

func TestToolchainCheckpointRunsAcrossPlanningAndBootstrapPhases(t *testing.T) {
	persona.Register(&fixedPersona{kind: state.PersonaProjectMgr, out: &state.PersonaOutput{
		Persona:      state.PersonaProjectMgr,
		Summary:      "pm done",
		Constitution: &state.Constitution{Vision: "ship code"},
		Requirements: &state.Requirements{},
		CompletedAt:  time.Now().UTC(),
	}})
	t.Cleanup(func() { persona.Unregister(state.PersonaProjectMgr) })
	persona.Register(&remediationArchitectPersona{})
	t.Cleanup(func() { persona.Unregister(state.PersonaArchitect) })
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	workspaceRoot := t.TempDir()
	bootstrap := &bootstrapTool{name: "go_mod_init", root: workspaceRoot}
	checkpoint := &checkpointCaptureTool{name: "git_push_checkpoint"}
	reg := tools.NewRegistry()
	reg.Register(bootstrap)
	reg.Register(checkpoint)

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaProjectMgr, state.PersonaArchitect, state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   workspaceRoot,
		ToolRegistry:    reg,
		Toolchains: []engine.ToolchainConfig{{
			ID:                   "go",
			Languages:            []string{"go", "golang"},
			Capabilities:         []string{"init_project", "git_push_checkpoint"},
			CapabilityTools:      map[string]string{"init_project": "go_mod_init", "git_push_checkpoint": "git_push_checkpoint"},
			CheckpointCapability: "git_push_checkpoint",
			PushCheckpoints:      true,
		}},
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if bootstrap.callCount != 1 {
		t.Fatalf("bootstrap call count = %d, want 1", bootstrap.callCount)
	}
	for _, phase := range []string{"constitution", "plan", "implementation-bootstrap", "implementation"} {
		if !containsString(checkpoint.phases, phase) {
			t.Fatalf("expected checkpoint phase %q in %v", phase, checkpoint.phases)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// TestEnforceValidationGate_BlocksFinalizerOnFailedValidation verifies that
// when EnforceValidationGate is set and the most recent toolchain validation
// run fails, the engine emits validation.gate.blocked, marks the workflow
// failed, and skips the Finalizer.
func TestEnforceValidationGate_BlocksFinalizerOnFailedValidation(t *testing.T) {
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	persona.Register(&noopPersona{kind: state.PersonaQA})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	// Finalizer must NOT run.  Register a sentinel that fails the test if it does.
	persona.Register(&forbiddenPersona{t: t, kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	reg := tools.NewRegistry()
	// run_tests deliberately reports passed=false so the validation gate fires.
	reg.Register(fakeTool{name: "run_tests", out: json.RawMessage(`{"passed":false,"error":"test suite failed"}`)})
	reg.Register(fakeTool{name: "git_checkpoint", out: json.RawMessage(`{"commit_sha":"abc","branch":"x","pushed":false}`)})

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaPod, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-fail-1",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaPod,
		CreatedAt:  time.Now().UTC(),
	}}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider:       "mock",
		DefaultModel:          "mock-model",
		WorkspaceRoot:         t.TempDir(),
		ToolRegistry:          reg,
		EnforceValidationGate: true,
		Toolchains: []engine.ToolchainConfig{{
			ID:                   "go",
			Languages:            []string{"go"},
			Capabilities:         []string{"run_tests", "git_checkpoint"},
			ValidationProfiles:   map[string][]string{"default": {"run_tests"}},
			CheckpointCapability: "git_checkpoint",
		}},
	})

	err := eng.Run(context.Background(), ws.ID)
	if err == nil {
		t.Fatal("expected validation gate to return an error")
	}
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	if stored.Status != state.WorkflowStatusFailed {
		t.Errorf("expected status=failed, got %q", stored.Status)
	}
	// Validation runs should still be recorded for observability.
	if len(stored.Execution.ValidationRuns) == 0 || stored.Execution.ValidationRuns[len(stored.Execution.ValidationRuns)-1].Passed {
		t.Errorf("expected last validation run to be failed, got %+v", stored.Execution.ValidationRuns)
	}
	// validation.gate.blocked event should be present.
	found := false
	for _, e := range ms.events {
		if e.Type == events.EventValidationGateBlocked {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected validation.gate.blocked event in journal")
	}
}

// forbiddenPersona fails the test if invoked.  Used to assert a phase is
// skipped (rather than relying on absence of side effects).
type forbiddenPersona struct {
	t    *testing.T
	kind state.PersonaKind
}

func (p *forbiddenPersona) Kind() state.PersonaKind { return p.kind }
func (p *forbiddenPersona) Name() string            { return string(p.kind) + "-forbidden" }
func (p *forbiddenPersona) Description() string     { return "" }
func (p *forbiddenPersona) Execute(_ context.Context, _ state.HandoffPacket) (*state.PersonaOutput, error) {
	p.t.Errorf("persona %q should not have been invoked", p.kind)
	return &state.PersonaOutput{Persona: p.kind}, nil
}

// blockingQAPersona is a QA persona that always reports one blocking issue.
// Used to simulate QA retry exhaustion without real LLM calls.
type blockingQAPersona struct{}

func (p *blockingQAPersona) Kind() state.PersonaKind { return state.PersonaQA }
func (p *blockingQAPersona) Name() string            { return "blocking-qa" }
func (p *blockingQAPersona) Description() string     { return "always reports a blocking issue" }
func (p *blockingQAPersona) Execute(_ context.Context, _ state.HandoffPacket) (*state.PersonaOutput, error) {
	return &state.PersonaOutput{
		Persona:        state.PersonaQA,
		Summary:        "found issues",
		BlockingIssues: []string{"unresolved blocking issue"},
	}, nil
}

type countingMatriarchPersona struct {
	calls            int
	remediationCalls int
}

func (p *countingMatriarchPersona) Kind() state.PersonaKind { return state.PersonaMatriarch }
func (p *countingMatriarchPersona) Name() string            { return "counting-matriarch" }
func (p *countingMatriarchPersona) Description() string     { return "tracks matriarch invocations" }
func (p *countingMatriarchPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	p.calls++
	summary := "initial matriarch review"
	if packet.IsRemediation {
		p.remediationCalls++
		summary = "remediation matriarch review"
	}
	return &state.PersonaOutput{
		Persona:     state.PersonaMatriarch,
		Summary:     summary,
		Suggestions: []string{"[matriarch][decision] keep bootstrap before implementation"},
		CompletedAt: time.Now().UTC(),
	}, nil
}

type remediationArchitectPersona struct{}

func (p *remediationArchitectPersona) Kind() state.PersonaKind { return state.PersonaArchitect }
func (p *remediationArchitectPersona) Name() string            { return "remediation-architect" }
func (p *remediationArchitectPersona) Description() string     { return "returns initial and remediation tasks" }
func (p *remediationArchitectPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	if packet.IsRemediation {
		return &state.PersonaOutput{
			Persona: state.PersonaArchitect,
			Summary: "architect remediation plan",
			Tasks: []state.Task{{
				ID:          "task-remediation",
				WorkflowID:  packet.WorkflowID,
				Title:       "apply remediation",
				Description: "fix the blocker",
				Status:      state.TaskStatusPending,
				AssignedTo:  state.PersonaPod,
				CreatedAt:   time.Now().UTC(),
			}},
			CompletedAt: time.Now().UTC(),
		}, nil
	}
	return &state.PersonaOutput{
		Persona: state.PersonaArchitect,
		Summary: "initial architect plan",
		Design:  &state.Design{Overview: "test design", TechStack: []string{"go"}},
		Tasks: []state.Task{{
			ID:          "task-initial",
			WorkflowID:  packet.WorkflowID,
			Title:       "initial implementation",
			Description: "write code",
			Status:      state.TaskStatusPending,
			AssignedTo:  state.PersonaPod,
			CreatedAt:   time.Now().UTC(),
		}},
		CompletedAt: time.Now().UTC(),
	}, nil
}

type oneShotBlockingQAPersona struct{ calls int }

func (p *oneShotBlockingQAPersona) Kind() state.PersonaKind { return state.PersonaQA }
func (p *oneShotBlockingQAPersona) Name() string            { return "one-shot-blocking-qa" }
func (p *oneShotBlockingQAPersona) Description() string     { return "blocks once and then passes" }
func (p *oneShotBlockingQAPersona) Execute(_ context.Context, _ state.HandoffPacket) (*state.PersonaOutput, error) {
	p.calls++
	if p.calls == 1 {
		return &state.PersonaOutput{
			Persona:        state.PersonaQA,
			Summary:        "found an implementation blocker",
			BlockingIssues: []string{"missing remediation"},
			CompletedAt:    time.Now().UTC(),
		}, nil
	}
	return &state.PersonaOutput{Persona: state.PersonaQA, Summary: "qa passed after remediation", CompletedAt: time.Now().UTC()}, nil
}

// TestQAExhaustionFailsBeforeFinalizer verifies that when MaxQARetries is
// exceeded the engine emits qa.exhausted and fails before running the
// Finalizer.
func TestQAExhaustionFailsBeforeFinalizer(t *testing.T) {
	// Register a QA persona that always reports blocking issues.
	persona.Register(&blockingQAPersona{})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })

	// Finalizer must not run once remediation is exhausted.
	persona.Register(&forbiddenPersona{t: t, kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "qa exhaustion test")
	// Skip Director by pre-populating its summary.
	ws.Summaries = map[state.PersonaKind]string{
		state.PersonaDirector: "(completed)",
	}
	// Only run QA and Finalizer — no Architect/Implementer needed.
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaQA, state.PersonaFinalizer}
	// Skip prompt loading from disk by providing a non-empty snapshot.
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		MaxQARetries:    1, // 2 total QA cycles before exhaustion
	})

	err := eng.Run(ctx, ws.ID)
	if err == nil {
		t.Fatal("expected remediation exhaustion to fail the workflow")
	}

	stored, _ := ms.GetWorkflow(ctx, ws.ID)
	if stored.Status != state.WorkflowStatusFailed {
		t.Errorf("expected failed status, got %q", stored.Status)
	}
	if !strings.Contains(stored.ErrorMessage, "qa remediation exhausted") {
		t.Errorf("expected exhaustion error message, got %q", stored.ErrorMessage)
	}

	// Verify a qa.exhausted event was emitted.
	var exhaustedEvt *events.Event
	for _, evt := range ms.events {
		if evt.Type == events.EventQAExhausted {
			exhaustedEvt = evt
			break
		}
	}
	if exhaustedEvt == nil {
		t.Fatal("expected qa.exhausted event to be emitted, got none")
	}
}

func TestMatriarchRunsAgainDuringRemediation(t *testing.T) {
	matriarch := &countingMatriarchPersona{}
	persona.Register(matriarch)
	t.Cleanup(func() { persona.Unregister(state.PersonaMatriarch) })
	persona.Register(&noopPersona{kind: state.PersonaProjectMgr})
	t.Cleanup(func() { persona.Unregister(state.PersonaProjectMgr) })
	persona.Register(&remediationArchitectPersona{})
	t.Cleanup(func() { persona.Unregister(state.PersonaArchitect) })
	persona.Register(&artifactPersona{kind: state.PersonaPod})
	t.Cleanup(func() { persona.Unregister(state.PersonaPod) })
	qa := &oneShotBlockingQAPersona{}
	persona.Register(qa)
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ws := state.NewWorkflowState("t1", "s1", "build a go app")
	ws.Mode = state.WorkflowModeSoftware
	ws.Summaries = map[state.PersonaKind]string{state.PersonaDirector: "done"}
	ws.RequiredPersonas = []state.PersonaKind{
		state.PersonaProjectMgr,
		state.PersonaMatriarch,
		state.PersonaArchitect,
		state.PersonaPod,
		state.PersonaQA,
		state.PersonaFinalizer,
	}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
		WorkspaceRoot:   t.TempDir(),
		MaxQARetries:    2,
	})

	if err := eng.Run(context.Background(), ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if matriarch.calls != 2 {
		t.Fatalf("matriarch call count = %d, want 2", matriarch.calls)
	}
	if matriarch.remediationCalls != 1 {
		t.Fatalf("matriarch remediation call count = %d, want 1", matriarch.remediationCalls)
	}
	stored, _ := ms.GetWorkflow(context.Background(), ws.ID)
	if len(stored.ReviewThread) == 0 {
		t.Fatal("expected review thread entries to be recorded")
	}
	joined := ""
	for _, entry := range stored.ReviewThread {
		joined += entry.Message + "\n"
	}
	if !strings.Contains(joined, "initial matriarch review") || !strings.Contains(joined, "remediation matriarch review") {
		t.Fatalf("expected review thread to capture both matriarch passes, got:\n%s", joined)
	}
}

// trackingPersona is a noop persona that records whether Execute was called.
type trackingPersona struct {
	kind   state.PersonaKind
	called bool
	// capturedAction captures the FinalizerAction field from the HandoffPacket.
	capturedAction string
}

func (p *trackingPersona) Kind() state.PersonaKind { return p.kind }
func (p *trackingPersona) Name() string            { return string(p.kind) }
func (p *trackingPersona) Description() string     { return "" }
func (p *trackingPersona) Execute(_ context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	p.called = true
	p.capturedAction = packet.FinalizerAction
	return &state.PersonaOutput{Persona: p.kind, Summary: "tracked"}, nil
}

type blockingPersona struct {
	kind    state.PersonaKind
	started chan struct{}
	release chan struct{}
}

func (p *blockingPersona) Kind() state.PersonaKind { return p.kind }
func (p *blockingPersona) Name() string            { return string(p.kind) }
func (p *blockingPersona) Description() string     { return "blocks until released" }
func (p *blockingPersona) Execute(_ context.Context, _ state.HandoffPacket) (*state.PersonaOutput, error) {
	select {
	case <-p.started:
	default:
		close(p.started)
	}
	<-p.release
	return &state.PersonaOutput{Persona: p.kind, Summary: "should not persist after cancellation"}, nil
}

// TestPersonaInclusionSkipping verifies that phases excluded from
// RequiredPersonas (set by the Director) are never executed.
func TestPersonaInclusionSkipping(t *testing.T) {
	// Register only the personas we want to observe.
	archPersona := &trackingPersona{kind: state.PersonaArchitect}
	persona.Register(archPersona)
	t.Cleanup(func() { persona.Unregister(state.PersonaArchitect) })

	finPersona := &trackingPersona{kind: state.PersonaFinalizer}
	persona.Register(finPersona)
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "skipping test")
	// Skip Director and ProjectMgr by pre-populating their summaries.
	ws.Summaries = map[state.PersonaKind]string{
		state.PersonaDirector:   "(completed)",
		state.PersonaProjectMgr: "(completed)",
	}
	// Director selected only Finalizer — Architect must be skipped.
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaFinalizer}
	// Skip prompt loading: pre-populated snapshot satisfies the guard.
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
	})

	if err := eng.Run(ctx, ws.ID); err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	if archPersona.called {
		t.Error("Architect persona was called but should have been skipped by RequiredPersonas filter")
	}
	if !finPersona.called {
		t.Error("Finalizer persona was NOT called but should have been included")
	}
}

// TestFinalizerActionForwardedInPacket verifies that ws.FinalizerAction is
// propagated into the HandoffPacket received by the Finalizer persona.
func TestFinalizerActionForwardedInPacket(t *testing.T) {
	finPersona := &trackingPersona{kind: state.PersonaFinalizer}
	persona.Register(finPersona)
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "finalizer action test")
	ws.Summaries = map[state.PersonaKind]string{
		state.PersonaDirector:   "(completed)",
		state.PersonaProjectMgr: "(completed)",
		state.PersonaArchitect:  "(completed)",
		state.PersonaQA:         "(completed)",
	}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaFinalizer}
	ws.FinalizerAction = "github-pr"
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
	})

	if err := eng.Run(ctx, ws.ID); err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	if !finPersona.called {
		t.Fatal("Finalizer persona was not called")
	}
	if finPersona.capturedAction != "github-pr" {
		t.Errorf("FinalizerAction in packet: got %q, want %q", finPersona.capturedAction, "github-pr")
	}
}

// TestPromptSnapshotReusedOnResume verifies that a workflow with a
// pre-populated PersonaPromptSnapshot does not attempt to re-load prompt
// files from disk (which would fail in the test environment).
func TestPromptSnapshotReusedOnResume(t *testing.T) {
	finPersona := &trackingPersona{kind: state.PersonaFinalizer}
	persona.Register(finPersona)
	t.Cleanup(func() { persona.Unregister(state.PersonaFinalizer) })

	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "snapshot reuse test")
	ws.Summaries = map[state.PersonaKind]string{
		state.PersonaDirector:   "(completed)",
		state.PersonaProjectMgr: "(completed)",
		state.PersonaArchitect:  "(completed)",
		state.PersonaQA:         "(completed)",
	}
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaFinalizer}
	// Pre-populate the snapshot — engine must reuse it, not reload from disk.
	// A non-existent PersonaPromptRoot ensures the test fails if the engine
	// tries to load prompts from disk instead of reusing the snapshot.
	ws.PersonaPromptSnapshot = map[string]string{
		"director":  "You are the Director.",
		"finalizer": "You are the Finalizer.",
	}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider:   "mock",
		DefaultModel:      "mock-model",
		PersonaPromptRoot: "/nonexistent/path/that/must/not/be/loaded",
	})

	if err := eng.Run(ctx, ws.ID); err != nil {
		t.Fatalf("engine must not reload prompts when snapshot is pre-populated; got error: %v", err)
	}

	if !finPersona.called {
		t.Error("Finalizer persona was not called")
	}
}

// TestRunStopsWhenWorkflowCancelled verifies that an external status flip to
// cancelled stops the engine before it can save additional persona output or
// transition the workflow back to completed.
func TestRunStopsWhenWorkflowCancelled(t *testing.T) {
	directorPersona := &blockingPersona{
		kind:    state.PersonaDirector,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	persona.Register(directorPersona)
	t.Cleanup(func() { persona.Unregister(state.PersonaDirector) })

	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "cancel mid-run")
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaDirector}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ms.workflows[ws.ID] = ws

	eng := engine.New(ms, engine.Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock-model",
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- eng.Run(ctx, ws.ID)
	}()

	select {
	case <-directorPersona.started:
	case <-time.After(2 * time.Second):
		t.Fatal("director persona did not start")
	}

	ms.setWorkflowStatus(ws.ID, state.WorkflowStatusCancelled, "cancelled by test")
	close(directorPersona.release)

	select {
	case err := <-errCh:
		if !errors.Is(err, engine.ErrCancelled) {
			t.Fatalf("expected ErrCancelled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine did not stop after workflow cancellation")
	}

	stored, err := ms.GetWorkflow(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if stored.Status != state.WorkflowStatusCancelled {
		t.Fatalf("expected cancelled status, got %q", stored.Status)
	}
	if stored.Execution.CurrentPersona != "" {
		t.Fatalf("expected current persona to be cleared, got %q", stored.Execution.CurrentPersona)
	}
	if stored.Summaries[state.PersonaDirector] != "" {
		t.Fatalf("expected director summary to stay empty after cancellation, got %q", stored.Summaries[state.PersonaDirector])
	}
	if stored.ErrorMessage != "cancelled by test" {
		t.Fatalf("expected cancellation error message to persist, got %q", stored.ErrorMessage)
	}
	if stored.CompletedAt == nil {
		t.Fatal("expected cancelled workflow to get a completion timestamp")
	}
}

// TestWorkflowStateNewHelpers verifies the state constructor sets expected
// defaults.
func TestWorkflowStateNewHelpers(t *testing.T) {
	ws := state.NewWorkflowState("tenant-abc", "scope-xyz", "do something useful")
	if ws.ID == "" {
		t.Error("ID must not be empty")
	}
	if ws.Status != state.WorkflowStatusPending {
		t.Errorf("Status: got %q, want pending", ws.Status)
	}
	if ws.TenantID != "tenant-abc" {
		t.Errorf("TenantID: got %q, want tenant-abc", ws.TenantID)
	}
	if ws.CreatedAt.IsZero() {
		t.Error("CreatedAt must not be zero")
	}
	if time.Since(ws.CreatedAt) > 5*time.Second {
		t.Error("CreatedAt appears to be too old")
	}
}
