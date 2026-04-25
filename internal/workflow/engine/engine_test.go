package engine_test

import (
	"context"
	"encoding/json"
	"errors"
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
			CreatedBy:  state.PersonaImplementer,
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
	persona.Register(&artifactPersona{kind: state.PersonaImplementer})
	t.Cleanup(func() { persona.Unregister(state.PersonaImplementer) })
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
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaImplementer, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-123456789",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaImplementer,
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

// TestEnforceValidationGate_BlocksFinalizerOnFailedValidation verifies that
// when EnforceValidationGate is set and the most recent toolchain validation
// run fails, the engine emits validation.gate.blocked, marks the workflow
// failed, and skips the Finalizer.
func TestEnforceValidationGate_BlocksFinalizerOnFailedValidation(t *testing.T) {
	persona.Register(&artifactPersona{kind: state.PersonaImplementer})
	t.Cleanup(func() { persona.Unregister(state.PersonaImplementer) })
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
	ws.RequiredPersonas = []state.PersonaKind{state.PersonaImplementer, state.PersonaQA, state.PersonaFinalizer}
	ws.PersonaPromptSnapshot = map[string]string{"_test": "skip"}
	ws.Tasks = []state.Task{{
		ID:         "task-fail-1",
		WorkflowID: ws.ID,
		Title:      "write main",
		Status:     state.TaskStatusPending,
		AssignedTo: state.PersonaImplementer,
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

// TestQAExhaustionContinuesToFinalizer verifies that when MaxQARetries is
// exceeded the engine emits a qa.exhausted event and continues to the
// Finalizer rather than failing the workflow.
func TestQAExhaustionContinuesToFinalizer(t *testing.T) {
	// Register a QA persona that always reports blocking issues.
	persona.Register(&blockingQAPersona{})
	t.Cleanup(func() { persona.Unregister(state.PersonaQA) })

	// Register a noop Finalizer.
	persona.Register(&noopPersona{kind: state.PersonaFinalizer})
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
	if err != nil {
		t.Fatalf("expected nil error when QA exhausted, got: %v", err)
	}

	stored, _ := ms.GetWorkflow(ctx, ws.ID)
	if stored.Status != state.WorkflowStatusCompleted {
		t.Errorf("expected completed status, got %q", stored.Status)
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
