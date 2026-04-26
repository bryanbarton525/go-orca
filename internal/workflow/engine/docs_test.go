package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine/docrender"
)

func newWSWithWorkspace(t *testing.T) (*state.WorkflowState, string) {
	t.Helper()
	dir := t.TempDir()
	ws := state.NewWorkflowState("tenant", "scope", "build a thing")
	ws.Execution.Workspace = &state.WorkspaceInfo{
		Path:      dir,
		Branch:    "workflow/test",
		CreatedBy: "test",
	}
	ws.Constitution = &state.Constitution{
		Vision:             "Ship a /healthz endpoint.",
		Goals:              []string{"Returns 200"},
		AcceptanceCriteria: []string{"go test passes", "go build passes"},
	}
	ws.Requirements = &state.Requirements{
		Functional: []state.Requirement{
			{ID: "F1", Title: "Healthz route", Description: "GET /healthz returns 200.", Priority: "must"},
		},
	}
	ws.Design = &state.Design{
		Overview:  "Single handler.",
		TechStack: []string{"Go"},
		Components: []state.DesignComponent{
			{Name: "handler", Description: "HTTP handler"},
		},
	}
	ws.Tasks = []state.Task{
		{ID: "task-001abcdef", Title: "Write handler", Description: "Implement /healthz", Specialty: "backend", Attempt: 0},
	}
	return ws, dir
}

func TestMaterializeConstitution_WritesToWorkspace(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, dir := newWSWithWorkspace(t)

	if err := eng.materializeConstitution(context.Background(), ws); err != nil {
		t.Fatalf("materializeConstitution: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, docrender.ConstitutionFile))
	if err != nil {
		t.Fatalf("read constitution.md: %v", err)
	}
	got := string(body)
	for _, want := range []string{"# Constitution", "Ship a /healthz endpoint.", "go test passes", "Healthz route"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in constitution.md:\n%s", want, got)
		}
	}
}

func TestMaterializePlan_WritesToWorkspace(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, dir := newWSWithWorkspace(t)

	if err := eng.materializePlan(context.Background(), ws); err != nil {
		t.Fatalf("materializePlan: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, docrender.PlanFile))
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	got := string(body)
	for _, want := range []string{"# Plan", "Single handler.", "## Components", "| handler |", "## Task Graph", "Write handler"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in plan.md:\n%s", want, got)
		}
	}
}

func TestAppendPlanRemediation_AppendsSection(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, dir := newWSWithWorkspace(t)

	if err := eng.materializePlan(context.Background(), ws); err != nil {
		t.Fatalf("materializePlan: %v", err)
	}
	// Add a remediation task with Attempt=1.
	ws.Tasks = append(ws.Tasks, state.Task{
		ID: "rem-001", Title: "Fix flaky test", Description: "Stabilise it",
		Specialty: "backend", Attempt: 1, RemediationSource: "qa_remediation",
	})
	if err := eng.appendPlanRemediation(context.Background(), ws, 1); err != nil {
		t.Fatalf("appendPlanRemediation: %v", err)
	}

	body, _ := os.ReadFile(filepath.Join(dir, docrender.PlanFile))
	got := string(body)
	if !strings.Contains(got, "# Plan") {
		t.Errorf("initial header lost after append:\n%s", got)
	}
	if !strings.Contains(got, "## Remediation Cycle 1 — Architect") {
		t.Errorf("missing remediation header:\n%s", got)
	}
	if !strings.Contains(got, "Fix flaky test") {
		t.Errorf("missing remediation task body:\n%s", got)
	}
}

func TestAppendPlanTriage_TriggersConstitutionAmendmentOnRequirementGap(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, dir := newWSWithWorkspace(t)

	if err := eng.materializeConstitution(context.Background(), ws); err != nil {
		t.Fatalf("materializeConstitution: %v", err)
	}
	if err := eng.materializePlan(context.Background(), ws); err != nil {
		t.Fatalf("materializePlan: %v", err)
	}

	// Triage classifies a requirement gap → constitution.md gets an amendment.
	if err := eng.appendPlanTriage(context.Background(), ws, 1,
		"Classified blocker as a requirement gap: missing latency target.",
		[]string{"p99 latency unbounded"}); err != nil {
		t.Fatalf("appendPlanTriage: %v", err)
	}

	plan, _ := os.ReadFile(filepath.Join(dir, docrender.PlanFile))
	if !strings.Contains(string(plan), "## Remediation Cycle 1 — PM Triage") {
		t.Errorf("plan triage section missing:\n%s", string(plan))
	}

	cons, _ := os.ReadFile(filepath.Join(dir, docrender.ConstitutionFile))
	if !strings.Contains(string(cons), "## Constitution Amendment — Cycle 1") {
		t.Errorf("constitution amendment missing on requirement-gap classification:\n%s", string(cons))
	}
	// Original constitution body must still be present (immutability).
	if !strings.Contains(string(cons), "Ship a /healthz endpoint.") {
		t.Errorf("original constitution body lost:\n%s", string(cons))
	}
}

func TestAppendPlanTriage_NoAmendmentWithoutRequirementGapPhrase(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, dir := newWSWithWorkspace(t)

	_ = eng.materializeConstitution(context.Background(), ws)
	_ = eng.materializePlan(context.Background(), ws)
	if err := eng.appendPlanTriage(context.Background(), ws, 1,
		"Classified as design gap: handler routing wrong.",
		[]string{"404 instead of 200"}); err != nil {
		t.Fatalf("appendPlanTriage: %v", err)
	}

	cons, _ := os.ReadFile(filepath.Join(dir, docrender.ConstitutionFile))
	if strings.Contains(string(cons), "## Constitution Amendment — Cycle") {
		t.Errorf("design-gap classification should NOT trigger constitution amendment:\n%s", string(cons))
	}
}

func TestPersistDoc_FallsBackToArtifactWhenNoWorkspace(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws := state.NewWorkflowState("tenant", "scope", "content workflow")
	// Intentionally no workspace.
	ws.Constitution = &state.Constitution{Vision: "Write a blog post."}

	if err := eng.materializeConstitution(context.Background(), ws); err != nil {
		t.Fatalf("materializeConstitution: %v", err)
	}
	if len(ws.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(ws.Artifacts))
	}
	a := ws.Artifacts[0]
	if a.Name != docrender.ConstitutionFile || a.Kind != state.ArtifactKindMarkdown {
		t.Errorf("wrong artifact name/kind: %+v", a)
	}
	if !strings.Contains(a.Content, "Write a blog post.") {
		t.Errorf("artifact content missing constitution body:\n%s", a.Content)
	}
}

func TestBuildHandoffContext_UsesWorkspaceMarkdown(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock"})
	ws, _ := newWSWithWorkspace(t)
	if err := eng.materializeConstitution(context.Background(), ws); err != nil {
		t.Fatalf("materializeConstitution: %v", err)
	}
	if err := eng.materializePlan(context.Background(), ws); err != nil {
		t.Fatalf("materializePlan: %v", err)
	}

	packet := state.HandoffPacket{
		WorkflowID:   ws.ID,
		Mode:         ws.Mode,
		Request:      ws.Request,
		Constitution: ws.Constitution,
		Requirements: ws.Requirements,
		Design:       ws.Design,
		Tasks:        ws.Tasks,
		Workspace:    ws.Execution.Workspace,
	}
	prompt := base.BuildHandoffContext(packet)

	// The rendered markdown headers should appear, the JSON code fences should not.
	for _, want := range []string{"## Constitution", "Ship a /healthz endpoint.", "## Plan", "Write handler"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected %q in handoff context:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "```json\n{\n  \"vision\"") {
		t.Errorf("constitution still rendered as JSON; expected markdown:\n%s", prompt)
	}
}

func TestBuildHandoffContext_FallsBackToJSONWhenNoDocs(t *testing.T) {
	// No workspace, no markdown artifact → fall back to JSON for back-compat.
	packet := state.HandoffPacket{
		WorkflowID:   "wf",
		Constitution: &state.Constitution{Vision: "legacy"},
		Design:       &state.Design{Overview: "legacy design"},
		Tasks:        []state.Task{{ID: "t-old001", Status: "pending", Title: "x"}},
	}
	prompt := base.BuildHandoffContext(packet)

	if !strings.Contains(prompt, "```json") {
		t.Errorf("expected JSON fallback when no docs are materialized:\n%s", prompt)
	}
	if !strings.Contains(prompt, "legacy design") {
		t.Errorf("expected design body in fallback:\n%s", prompt)
	}
}

func TestLoadDocForState_PrefersDiskOverArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "constitution.md"), []byte("disk content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := state.NewWorkflowState("t", "s", "r")
	ws.Execution.Workspace = &state.WorkspaceInfo{Path: dir}
	ws.Artifacts = []state.Artifact{
		{Name: "constitution.md", Kind: state.ArtifactKindMarkdown, Content: "artifact content"},
	}
	got := loadDocForState(ws, "constitution.md")
	if got != "disk content" {
		t.Errorf("expected disk content to win, got %q", got)
	}
}
