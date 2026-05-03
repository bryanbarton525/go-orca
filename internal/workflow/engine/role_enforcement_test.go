package engine_test

// role_enforcement_test.go validates the per-persona output contracts and
// task-ownership rules introduced to fix the QA role-drift bug:
//
//  1. applyOutput discards Artifacts produced by non-Pod personas.
//  2. applyOutput discards Tasks/Design produced by non-Architect personas.
//  3. runPodPhase skips tasks whose AssignedTo is not "pod".
//  4. BlockingIssues appear in BuildHandoffContext so Pod knows what QA rejected.
//  5. Execution.CurrentPersona is populated after a persona runs.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// ─── stub persona ─────────────────────────────────────────────────────────────

type fixedPersona struct {
	kind state.PersonaKind
	out  *state.PersonaOutput
}

func (f *fixedPersona) Kind() state.PersonaKind { return f.kind }
func (f *fixedPersona) Name() string            { return string(f.kind) }
func (f *fixedPersona) Description() string     { return "" }
func (f *fixedPersona) Execute(_ context.Context, _ state.HandoffPacket) (*state.PersonaOutput, error) {
	return f.out, nil
}

// registerPersonas registers personas and returns a cleanup func.
func registerPersonas(t *testing.T, ps ...persona.Persona) func() {
	t.Helper()
	for _, p := range ps {
		persona.Register(p)
	}
	return func() {
		for _, p := range ps {
			persona.Unregister(p.Kind())
		}
	}
}

// stubPromptSnapshot returns a persona prompt snapshot with stub content for
// every required key so the engine's prompt-load step is bypassed in tests.
func stubPromptSnapshot() map[string]string {
	snap := make(map[string]string, len(prompts.Keys()))
	for _, k := range prompts.Keys() {
		snap[k] = "stub prompt for " + k
	}
	return snap
}

// baseWorkflow builds a WorkflowState past the Director phase.
func baseWorkflow(required ...state.PersonaKind) *state.WorkflowState {
	ws := state.NewWorkflowState("tenant-1", "scope-1", "test request")
	ws.ProviderName = "mock"
	ws.ModelName = "mock-model"
	ws.RequiredPersonas = required
	ws.Summaries = map[state.PersonaKind]string{
		state.PersonaDirector: "director done",
	}
	ws.PersonaPromptSnapshot = stubPromptSnapshot()
	return ws
}

// ─── Test: QA cannot inject Tasks ────────────────────────────────────────────

func TestRoleEnforcement_QACannotInjectTasks(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaProjectMgr, state.PersonaQA)
	ws.Summaries[state.PersonaProjectMgr] = "pm done"
	ws.Summaries[state.PersonaArchitect] = "arch done"
	ws.Summaries[state.PersonaPod] = "impl done"
	ws.Tasks = []state.Task{
		{ID: "arch-t1", Title: "original task", AssignedTo: state.PersonaPod, Status: state.TaskStatusCompleted},
	}

	cleanup := registerPersonas(t,
		&fixedPersona{kind: state.PersonaProjectMgr, out: &state.PersonaOutput{
			Persona: state.PersonaProjectMgr, Summary: "pm",
		}},
		&fixedPersona{kind: state.PersonaQA, out: &state.PersonaOutput{
			Persona: state.PersonaQA,
			Summary: "qa done",
			// QA should never return Tasks — engine must reject this.
			Tasks: []state.Task{
				{ID: "qa-injected", Title: "injected by QA", AssignedTo: state.PersonaPod, Status: state.TaskStatusPending},
			},
			BlockingIssues: nil,
		}},
	)
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID) // may error at Finalizer — that's fine

	final, _ := ms.GetWorkflow(ctx, ws.ID)

	for _, task := range final.Tasks {
		if task.ID == "qa-injected" {
			t.Errorf("QA-injected task %q found in WorkflowState.Tasks — role enforcement failed", task.ID)
		}
	}
	var foundOriginal bool
	for _, task := range final.Tasks {
		if task.ID == "arch-t1" {
			foundOriginal = true
		}
	}
	if !foundOriginal {
		t.Errorf("original Architect task was lost; tasks: %+v", final.Tasks)
	}
	var warnFound bool
	for _, s := range final.AllSuggestions {
		if strings.Contains(s, "role-enforcement") && strings.Contains(s, string(state.PersonaQA)) {
			warnFound = true
			break
		}
	}
	if !warnFound {
		t.Errorf("expected role-enforcement warning suggestion; got: %v", final.AllSuggestions)
	}
}

// ─── Test: QA cannot inject Artifacts ────────────────────────────────────────

func TestRoleEnforcement_QACannotInjectArtifacts(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaQA)
	ws.Summaries[state.PersonaProjectMgr] = "pm done"
	ws.Summaries[state.PersonaArchitect] = "arch done"
	ws.Summaries[state.PersonaPod] = "impl done"

	cleanup := registerPersonas(t,
		&fixedPersona{kind: state.PersonaQA, out: &state.PersonaOutput{
			Persona: state.PersonaQA,
			Summary: "qa done",
			Artifacts: []state.Artifact{
				{Name: "injected.md", Kind: state.ArtifactKindMarkdown, Content: "forbidden"},
			},
			BlockingIssues: nil,
		}},
	)
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID)

	final, _ := ms.GetWorkflow(ctx, ws.ID)
	for _, a := range final.Artifacts {
		if a.Name == "injected.md" {
			t.Errorf("QA-produced artifact %q found in Artifacts — role enforcement failed", a.Name)
		}
	}
	var warnFound bool
	for _, s := range final.AllSuggestions {
		if strings.Contains(s, "role-enforcement") && strings.Contains(s, string(state.PersonaQA)) {
			warnFound = true
			break
		}
	}
	if !warnFound {
		t.Errorf("expected role-enforcement warning for QA artifact; got: %v", final.AllSuggestions)
	}
}

// ─── Test: Pod skips tasks assigned to QA ────────────────────────────

func TestRunPodPhase_SkipsQAAssignedTasks(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaPod)
	ws.Summaries[state.PersonaProjectMgr] = "pm done"
	ws.Summaries[state.PersonaArchitect] = "arch done"
	ws.Tasks = []state.Task{
		{ID: "impl-t1", Title: "valid pod task", AssignedTo: state.PersonaPod, Status: state.TaskStatusPending},
		{ID: "qa-t1", Title: "task wrongly assigned to qa", AssignedTo: state.PersonaQA, Status: state.TaskStatusPending},
	}

	cleanup := registerPersonas(t,
		&fixedPersona{kind: state.PersonaPod, out: &state.PersonaOutput{
			Persona:   state.PersonaPod,
			Summary:   "impl done",
			Artifacts: []state.Artifact{{Name: "out.md", Kind: state.ArtifactKindMarkdown, Content: "hello"}},
		}},
	)
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID)

	// task.started must NOT fire for the QA-assigned task.
	for _, ev := range ms.events {
		if ev.Type != events.EventTaskStarted {
			continue
		}
		var payload map[string]string
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload["task_id"] == "qa-t1" {
			t.Errorf("task.started fired for QA-assigned task %q — ownership enforcement failed", "qa-t1")
		}
	}

	final, _ := ms.GetWorkflow(ctx, ws.ID)
	for _, task := range final.Tasks {
		if task.ID == "qa-t1" {
			if task.Status == state.TaskStatusCompleted {
				t.Errorf("QA-assigned task was completed by Pod — ownership enforcement failed")
			}
			if task.Status == state.TaskStatusRunning {
				t.Errorf("QA-assigned task was set to Running by Pod — ownership enforcement failed")
			}
		}
	}
}

// ─── Test: blocking issues surface in BuildHandoffContext ─────────────────────

func TestBuildHandoffContext_BlockingIssuesVisible(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-test",
		Mode:       "content",
		Request:    "write a blog post",
		BlockingIssues: []string{
			"Missing introduction section",
			"Conclusion does not reference the main argument",
		},
	}

	handoffCtx := base.BuildHandoffContext(packet)

	if !strings.Contains(handoffCtx, "Missing introduction section") {
		t.Errorf("blocking issue 1 not in handoff context:\n%s", handoffCtx)
	}
	if !strings.Contains(handoffCtx, "Conclusion does not reference") {
		t.Errorf("blocking issue 2 not in handoff context:\n%s", handoffCtx)
	}
	if !strings.Contains(handoffCtx, "QA Blocking Issues") {
		t.Errorf("'QA Blocking Issues' section header not in handoff context:\n%s", handoffCtx)
	}
}

func TestBuildHandoffContext_NoBlockingIssues_NoBanner(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-test",
		Mode:       "content",
		Request:    "write a blog post",
	}

	handoffCtx := base.BuildHandoffContext(packet)

	if strings.Contains(handoffCtx, "QA Blocking Issues") {
		t.Errorf("unexpected 'QA Blocking Issues' section when no issues present:\n%s", handoffCtx)
	}
}

func TestBuildHandoffContext_RemediationContext(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID:     "wf-test",
		Mode:           "software",
		Request:        "build an API",
		BlockingIssues: []string{"missing error handling"},
		IsRemediation:  true,
		QACycle:        2,
	}

	handoffCtx := base.BuildHandoffContext(packet)

	if !strings.Contains(handoffCtx, "Remediation Context") {
		t.Errorf("'Remediation Context' section not found:\n%s", handoffCtx)
	}
	if !strings.Contains(handoffCtx, "QA cycle 2") {
		t.Errorf("QA cycle number not found in remediation context:\n%s", handoffCtx)
	}
}

func TestBuildHandoffContext_ReviewThreadVisible(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-test",
		Mode:       "software",
		Request:    "build an API",
		ReviewThread: []state.ReviewThreadEntry{
			{Persona: state.PersonaDirector, Kind: "summary", Message: "director selected a repo-backed software workflow"},
			{Persona: state.PersonaMatriarch, Kind: "question", Message: "confirm whether retries should fail fast on validation exhaustion", RemediationAttempt: 1},
			{Persona: state.PersonaQA, Kind: "blocking_issue", Message: "validation still fails in go test ./...", QACycle: 2},
		},
	}

	handoffCtx := base.BuildHandoffContext(packet)

	if !strings.Contains(handoffCtx, "Review Thread") {
		t.Errorf("expected review thread section in handoff context:\n%s", handoffCtx)
	}
	if !strings.Contains(handoffCtx, "director selected a repo-backed software workflow") {
		t.Errorf("director review thread entry missing:\n%s", handoffCtx)
	}
	if !strings.Contains(handoffCtx, "validation still fails in go test ./...") {
		t.Errorf("qa review thread entry missing:\n%s", handoffCtx)
	}
}

// ─── Test: execution progress updated after run ───────────────────────────────

func TestExecution_CurrentPersonaSetAfterRun(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := state.NewWorkflowState("t1", "s1", "test exec progress")
	ws.ProviderName = "mock"
	ws.ModelName = "mock"
	ws.PersonaPromptSnapshot = stubPromptSnapshot()

	cleanup := registerPersonas(t,
		&fixedPersona{kind: state.PersonaDirector, out: &state.PersonaOutput{
			Persona: state.PersonaDirector,
			Summary: "director done",
		}},
	)
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID) // may fail after director — that's fine

	final, _ := ms.GetWorkflow(ctx, ws.ID)
	if final.Execution.CurrentPersona == "" {
		t.Error("Execution.CurrentPersona is empty after Director ran — execution progress not being set")
	}
}
