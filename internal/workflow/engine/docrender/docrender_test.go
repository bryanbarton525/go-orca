package docrender

import (
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestRenderConstitution_NilInputs(t *testing.T) {
	got := RenderConstitution(nil, nil)
	if !strings.HasPrefix(got, "# Constitution") {
		t.Fatalf("expected header, got %q", got)
	}
}

func TestRenderConstitution_Populated(t *testing.T) {
	c := &state.Constitution{
		Vision:             "Ship reliable code.",
		Goals:              []string{"Working tests", "Clean diff"},
		Constraints:        []string{"No new deps", "Idiomatic Go"},
		Audience:           "Engineering team",
		OutputMedium:       "git repository",
		AcceptanceCriteria: []string{"go test passes", "go build passes"},
		OutOfScope:         []string{"UI redesign"},
	}
	r := &state.Requirements{
		Functional: []state.Requirement{
			{ID: "F1", Title: "Add /healthz", Description: "Return 200 OK.", Priority: "must", Source: "user"},
		},
		NonFunctional: []state.Requirement{
			{ID: "NF1", Title: "p99 < 50ms", Description: "Latency cap.", Priority: "should", Source: "ops"},
		},
		Dependencies: []string{"net/http"},
	}
	got := RenderConstitution(c, r)

	checks := []string{
		"## Vision",
		"Ship reliable code.",
		"## Goals",
		"- Working tests",
		"## Acceptance Criteria",
		"- go test passes",
		"## Out of Scope",
		"## Functional Requirements",
		"| F1 | must | Add /healthz |",
		"## Non-Functional Requirements",
		"| NF1 | should | p99 < 50ms |",
		"## Dependencies",
		"- net/http",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestRenderConstitution_EmptyListsSkipped(t *testing.T) {
	c := &state.Constitution{Vision: "x"}
	got := RenderConstitution(c, nil)
	if strings.Contains(got, "## Goals") {
		t.Errorf("empty Goals section should be skipped:\n%s", got)
	}
	if strings.Contains(got, "## Out of Scope") {
		t.Errorf("empty OutOfScope section should be skipped:\n%s", got)
	}
}

func TestRenderPlan_NilInputs(t *testing.T) {
	got := RenderPlan(nil, nil)
	if !strings.HasPrefix(got, "# Plan") {
		t.Fatalf("expected header, got %q", got)
	}
	if strings.Contains(got, "## Task Graph") {
		t.Errorf("Task Graph should not appear when there are no tasks")
	}
}

func TestRenderPlan_Populated(t *testing.T) {
	d := &state.Design{
		Overview:       "Two-service split.",
		DeliveryTarget: "k8s",
		TechStack:      []string{"Go 1.22", "Postgres"},
		Components: []state.DesignComponent{
			{Name: "api", Description: "HTTP handler", Inputs: []string{"request"}, Outputs: []string{"response"}},
		},
		Decisions: []state.DesignDecision{
			{Decision: "Use chi router", Rationale: "stdlib-compatible", Tradeoffs: "extra dep"},
		},
	}
	tasks := []state.Task{
		{ID: "task-001234567", Title: "Write handler", Description: "Implement /healthz", Specialty: "backend", Attempt: 0},
		{ID: "task-002345678", Title: "Add helm value", Description: "wire endpoint", Specialty: "ops", Attempt: 0, DependsOn: []string{"task-001234567"}},
		{ID: "task-003", Title: "Remediation only", Description: "x", Attempt: 1}, // should be excluded
	}
	got := RenderPlan(d, tasks)

	checks := []string{
		"## Overview",
		"Two-service split.",
		"## Delivery Target",
		"k8s",
		"## Tech Stack",
		"- Go 1.22",
		"## Components",
		"| api | HTTP handler | request | response |",
		"## Architectural Decisions",
		"**Use chi router**",
		"Rationale: stdlib-compatible",
		"Tradeoffs: extra dep",
		"## Task Graph",
		"| task-001 | backend | Write handler |",
		"| task-002 | ops | Add helm value | task-001 |",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Remediation only") {
		t.Errorf("Attempt=1 task leaked into initial plan output:\n%s", got)
	}
}

func TestRenderPlan_TaskGroupingPutsBlankSpecialtyLast(t *testing.T) {
	tasks := []state.Task{
		{ID: "t1", Title: "no specialty", Specialty: "", Attempt: 0},
		{ID: "t2", Title: "ops thing", Specialty: "ops", Attempt: 0},
		{ID: "t3", Title: "backend thing", Specialty: "backend", Attempt: 0},
	}
	got := RenderPlan(nil, tasks)
	idxBackend := strings.Index(got, "backend thing")
	idxOps := strings.Index(got, "ops thing")
	idxBlank := strings.Index(got, "no specialty")
	if !(idxBackend < idxOps && idxOps < idxBlank) {
		t.Errorf("expected order backend < ops < blank-specialty; got backend=%d ops=%d blank=%d\n%s",
			idxBackend, idxOps, idxBlank, got)
	}
}

func TestRenderTriageSection(t *testing.T) {
	got := RenderTriageSection(2, "Classified as design gap.", []string{"build failed", "tests failed"})
	wants := []string{
		"## Remediation Cycle 2 — PM Triage",
		"Classified as design gap.",
		"- build failed",
		"- tests failed",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRenderRemediationSection(t *testing.T) {
	d := &state.Design{Overview: "Same as before."}
	tasks := []state.Task{
		{ID: "rem-001", Title: "Fix tests", Description: "Make them pass", Attempt: 1, Specialty: "backend"},
		{ID: "old-001", Title: "Original", Attempt: 0},
	}
	got := RenderRemediationSection(1, d, tasks)
	if !strings.Contains(got, "## Remediation Cycle 1 — Architect") {
		t.Errorf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "Fix tests") {
		t.Errorf("missing remediation task:\n%s", got)
	}
	if strings.Contains(got, "Original") {
		t.Errorf("non-cycle task leaked into section:\n%s", got)
	}
	if !strings.Contains(got, "Same as before.") {
		t.Errorf("missing current overview reference:\n%s", got)
	}
}

func TestRenderRemediationSection_NoTasks(t *testing.T) {
	got := RenderRemediationSection(3, nil, nil)
	if !strings.Contains(got, "_No remediation tasks were emitted for this cycle._") {
		t.Errorf("expected empty-cycle marker:\n%s", got)
	}
}

func TestRenderConstitutionAmendment(t *testing.T) {
	got := RenderConstitutionAmendment(2, "Added explicit p95 latency target after triage.")
	if !strings.Contains(got, "## Constitution Amendment — Cycle 2") {
		t.Errorf("missing amendment header:\n%s", got)
	}
	if !strings.Contains(got, "Added explicit p95 latency target after triage.") {
		t.Errorf("missing amendment body:\n%s", got)
	}
}

func TestEscapeCellHandlesPipesAndNewlines(t *testing.T) {
	c := &state.Constitution{}
	r := &state.Requirements{
		Functional: []state.Requirement{
			{ID: "F1", Title: "a|b", Description: "line1\nline2", Priority: "must"},
		},
	}
	got := RenderConstitution(c, r)
	if !strings.Contains(got, `a\|b`) {
		t.Errorf("pipe not escaped:\n%s", got)
	}
	if strings.Contains(got, "line1\nline2") {
		t.Errorf("newline not stripped:\n%s", got)
	}
	if !strings.Contains(got, "line1 line2") {
		t.Errorf("newline not collapsed to space:\n%s", got)
	}
}
