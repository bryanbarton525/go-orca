package improvements_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/improvements"
	"github.com/go-orca/go-orca/internal/state"
)

// ─── Router tests ─────────────────────────────────────────────────────────────

func TestRoute_Advisory_WhenChangeTypeAdvisory(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "foo",
		ChangeType:    "advisory",
		Priority:      "low",
		Content:       "some content",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeAdvisory {
		t.Errorf("expected advisory, got %q", got)
	}
}

func TestRoute_Advisory_WhenNoFilesOrContent(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "foo",
		ChangeType:    "update",
		Priority:      "low",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeAdvisory {
		t.Errorf("expected advisory for empty content, got %q", got)
	}
}

func TestRoute_Workflow_ForPersona(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "persona",
		ComponentName: "pod",
		ChangeType:    "update",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("persona should always route to workflow, got %q", got)
	}
}

func TestRoute_Workflow_ForCreate(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "new-skill",
		ChangeType:    "create",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("create should always route to workflow, got %q", got)
	}
}

func TestRoute_Workflow_ForMediumPriority(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "foo",
		ChangeType:    "update",
		Priority:      "medium",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("medium priority should route to workflow, got %q", got)
	}
}

func TestRoute_Workflow_ForHighPriority(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "prompt",
		ComponentName: "system",
		ChangeType:    "update",
		Priority:      "high",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("high priority should route to workflow, got %q", got)
	}
}

func TestRoute_Direct_ForLowPrioritySkillUpdate(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "foo",
		ChangeType:    "update",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeDirect {
		t.Errorf("low priority skill update should route to direct, got %q", got)
	}
}

func TestRoute_Workflow_ForLowPriorityAgentUpdate(t *testing.T) {
	// agent is not a permitted direct-apply surface; low-priority agent updates
	// must go through a workflow PR so the surface policy can gate them.
	imp := state.RefinerImprovement{
		ComponentType: "agent",
		ComponentName: "my-agent",
		ChangeType:    "update",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("low priority agent update should route to workflow, got %q", got)
	}
}

func TestRoute_Direct_ForLowPriorityPromptUpdate(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "prompt",
		ComponentName: "delivery",
		ChangeType:    "update",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeDirect {
		t.Errorf("low priority prompt update should route to direct, got %q", got)
	}
}

func TestRoute_Workflow_Default(t *testing.T) {
	// unknown component type with low priority update → workflow (safe fallback)
	imp := state.RefinerImprovement{
		ComponentType: "unknown",
		ComponentName: "foo",
		ChangeType:    "update",
		Priority:      "low",
		Content:       "x",
	}
	if got := improvements.Route(imp); got != improvements.ApplyModeWorkflow {
		t.Errorf("unknown component type should fall back to workflow, got %q", got)
	}
}

// ─── Validator tests ──────────────────────────────────────────────────────────

func TestValidateImprovement_ValidSkillFrontmatter(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		Content: `---
name: my-skill
description: Does something useful
---
# Body
`,
	}
	if err := improvements.ValidateImprovement(imp); err != nil {
		t.Errorf("expected valid skill, got error: %v", err)
	}
}

func TestValidateImprovement_MissingSkillName(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		Content: `---
description: Does something useful
---
# Body
`,
	}
	if err := improvements.ValidateImprovement(imp); err == nil {
		t.Error("expected error for missing 'name' field, got nil")
	}
}

func TestValidateImprovement_MissingSkillDescription(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		Content: `---
name: my-skill
---
# Body
`,
	}
	if err := improvements.ValidateImprovement(imp); err == nil {
		t.Error("expected error for missing 'description' field, got nil")
	}
}

func TestValidateImprovement_ValidAgentFrontmatter(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "agent",
		ComponentName: "my-agent",
		Files: []state.ImprovementFile{{
			Path: "agents/my-agent.agent.md",
			Content: `---
name: my-agent
description: An agent
model: gpt-4o
color: blue
---
# Body
`,
		}},
	}
	if err := improvements.ValidateImprovement(imp); err != nil {
		t.Errorf("expected valid agent, got error: %v", err)
	}
}

func TestValidateImprovement_AgentMissingColor(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "agent",
		ComponentName: "my-agent",
		Files: []state.ImprovementFile{{
			Path: "agents/my-agent.agent.md",
			Content: `---
name: my-agent
description: An agent
model: gpt-4o
---
`,
		}},
	}
	if err := improvements.ValidateImprovement(imp); err == nil {
		t.Error("expected error for missing 'color' in agent frontmatter")
	}
}

func TestValidateImprovement_PromptNoSchema(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "prompt",
		ComponentName: "system",
		Content:       "# System\nDo stuff.\n",
	}
	if err := improvements.ValidateImprovement(imp); err != nil {
		t.Errorf("prompt should have no schema requirements, got: %v", err)
	}
}

// ─── Path policy tests ────────────────────────────────────────────────────────

func TestValidatePath_AllowsNormalPaths(t *testing.T) {
	cases := []string{
		"skills/my-skill/SKILL.md",
		"agents/my-agent.agent.md",
		"prompts/delivery.prompt.md",
	}
	for _, c := range cases {
		if err := improvements.ValidatePath(c); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", c, err)
		}
	}
}

func TestValidatePath_BlocksTraversal(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"skills/../../secrets",
		"agents/../../../etc/passwd",
	}
	for _, c := range cases {
		if err := improvements.ValidatePath(c); err == nil {
			t.Errorf("expected %q to be rejected as traversal, got nil", c)
		}
	}
}

func TestValidatePath_BlocksAbsolutePaths(t *testing.T) {
	if err := improvements.ValidatePath("/etc/passwd"); err == nil {
		t.Error("expected absolute path to be rejected")
	}
}

func TestValidatePath_BlocksPersonasPrefix(t *testing.T) {
	if err := improvements.ValidatePath("personas/pod.md"); err == nil {
		t.Error("expected personas/ path to be blocked")
	}
}

// ─── FileStore tests ──────────────────────────────────────────────────────────

func TestFileStore_StageAndPromote(t *testing.T) {
	root := t.TempDir()
	fs := improvements.NewFileStore(root)
	wfID := "test-workflow-1"
	files := []state.ImprovementFile{
		{Path: "skills/my-skill/SKILL.md", Content: "# SKILL\n"},
	}

	if err := fs.Stage(wfID, files); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}

	appliedPaths, err := fs.Promote(wfID, files)
	if err != nil {
		t.Fatalf("Promote failed: %v", err)
	}
	if len(appliedPaths) != 1 {
		t.Fatalf("expected 1 applied path, got %d", len(appliedPaths))
	}

	expected := filepath.Join(root, "active", "skills", "my-skill", "SKILL.md")
	if appliedPaths[0] != expected {
		t.Errorf("expected path %q, got %q", expected, appliedPaths[0])
	}

	data, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read active file: %v", err)
	}
	if string(data) != "# SKILL\n" {
		t.Errorf("unexpected content: %q", string(data))
	}

	// Staging directory should be cleaned up.
	stagingDir := filepath.Join(root, "staging", wfID)
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Error("staging directory should have been removed after promote")
	}
}

func TestFileStore_Rollback(t *testing.T) {
	root := t.TempDir()
	fs := improvements.NewFileStore(root)
	wfID := "test-workflow-rollback"
	files := []state.ImprovementFile{
		{Path: "skills/foo/SKILL.md", Content: "content"},
	}

	if err := fs.Stage(wfID, files); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}
	fs.Rollback(wfID, files)

	// Staging should be gone.
	if _, err := os.Stat(filepath.Join(root, "staging", wfID)); !os.IsNotExist(err) {
		t.Error("staging dir should be removed after rollback")
	}
	// Disabled dir should have the file.
	disabledPath := filepath.Join(root, "disabled", wfID, "skills", "foo", "SKILL.md")
	if _, err := os.Stat(disabledPath); err != nil {
		t.Errorf("disabled file not found: %v", err)
	}
}

func TestFileStore_WriteManifest(t *testing.T) {
	root := t.TempDir()
	fs := improvements.NewFileStore(root)
	m := improvements.Manifest{
		WorkflowID:    "wf-abc",
		ComponentType: "skill",
		ComponentName: "test-skill",
		ApplyMode:     improvements.ApplyModeDirect,
		Files:         []string{"skills/test-skill/SKILL.md"},
		Status:        "applied",
	}
	if err := fs.WriteManifest(context.Background(), m); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}
	mPath := filepath.Join(root, "manifests", "wf-abc.json")
	if _, err := os.Stat(mPath); err != nil {
		t.Errorf("manifest file not found: %v", err)
	}
}

// ─── ConcreteDispatcher tests (advisory / direct apply) ───────────────────────

type mockWorkflowCreator struct {
	created []*state.WorkflowState
	saved   []*state.WorkflowState
}

func (m *mockWorkflowCreator) CreateWorkflow(_ context.Context, ws *state.WorkflowState) error {
	m.created = append(m.created, ws)
	return nil
}
func (m *mockWorkflowCreator) SaveWorkflow(_ context.Context, ws *state.WorkflowState) error {
	m.saved = append(m.saved, ws)
	return nil
}

type mockEnqueuer struct {
	enqueued []string
}

func (m *mockEnqueuer) Enqueue(id string) error {
	m.enqueued = append(m.enqueued, id)
	return nil
}

func parentWS() *state.WorkflowState {
	return &state.WorkflowState{
		ID:       "parent-wf-id",
		TenantID: "tenant-1",
		ScopeID:  "scope-1",
		Execution: state.Execution{
			ImprovementDepth: 0,
		},
	}
}

func TestDispatcher_Advisory_Skipped(t *testing.T) {
	root := t.TempDir()
	store := &mockWorkflowCreator{}
	enq := &mockEnqueuer{}
	d := improvements.NewConcreteDispatcher(root, store, enq)

	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "foo",
		ChangeType:    "advisory",
		Priority:      "low",
	}
	result, err := d.Dispatch(context.Background(), parentWS(), imp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "skipped" {
		t.Errorf("expected status 'skipped', got %q", result.Status)
	}
	if len(store.created) != 0 {
		t.Error("advisory dispatch should not create workflows")
	}
}

func TestDispatcher_Direct_AppliesFile(t *testing.T) {
	root := t.TempDir()
	store := &mockWorkflowCreator{}
	enq := &mockEnqueuer{}
	d := improvements.NewConcreteDispatcher(root, store, enq)

	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		ChangeType:    "update",
		Priority:      "low",
		Content: `---
name: my-skill
description: A useful skill
---
# Body
`,
	}
	result, err := d.Dispatch(context.Background(), parentWS(), imp)
	if err != nil {
		t.Fatalf("direct apply failed: %v", err)
	}
	if result.Status != "applied" {
		t.Errorf("expected status 'applied', got %q", result.Status)
	}
	if result.AppliedPath == "" {
		t.Error("AppliedPath should be set for direct apply")
	}
	if !strings.Contains(result.AppliedPath, "active") {
		t.Errorf("AppliedPath %q should contain 'active'", result.AppliedPath)
	}
	if !strings.HasSuffix(result.AppliedPath, "SKILL.md") {
		t.Errorf("AppliedPath %q should end in SKILL.md", result.AppliedPath)
	}

	// Verify file exists on disk.
	if _, err := os.Stat(result.AppliedPath); err != nil {
		t.Errorf("applied file not found on disk: %v", err)
	}
}

func TestDispatcher_Direct_RejectsTraversalPath(t *testing.T) {
	root := t.TempDir()
	d := improvements.NewConcreteDispatcher(root, &mockWorkflowCreator{}, &mockEnqueuer{})

	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "../../evil",
		ChangeType:    "update",
		Priority:      "low",
		Files: []state.ImprovementFile{{
			Path:    "../../evil/SKILL.md",
			Content: "bad",
		}},
	}
	result, err := d.Dispatch(context.Background(), parentWS(), imp)
	if err == nil {
		t.Error("expected error for traversal path, got nil")
	}
	if result.Status != "error" {
		t.Errorf("expected status 'error', got %q", result.Status)
	}
}

func TestDispatcher_Workflow_LaunchesChildWorkflow(t *testing.T) {
	root := t.TempDir()
	store := &mockWorkflowCreator{}
	enq := &mockEnqueuer{}
	d := improvements.NewConcreteDispatcher(root, store, enq)

	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "new-skill",
		ChangeType:    "create",
		Priority:      "low",
		Content:       "content",
	}
	result, err := d.Dispatch(context.Background(), parentWS(), imp)
	if err != nil {
		t.Fatalf("workflow dispatch failed: %v", err)
	}
	if result.Status != "dispatched" {
		t.Errorf("expected status 'dispatched', got %q", result.Status)
	}
	if result.ChildWorkflowID == "" {
		t.Error("ChildWorkflowID should be set for workflow dispatch")
	}
	if len(store.created) != 1 {
		t.Errorf("expected 1 workflow created, got %d", len(store.created))
	}
	if len(enq.enqueued) != 1 {
		t.Errorf("expected 1 workflow enqueued, got %d", len(enq.enqueued))
	}

	// Verify child workflow metadata.
	child := store.created[0]
	if child.Execution.WorkflowKind != "improvement" {
		t.Errorf("child WorkflowKind should be 'improvement', got %q", child.Execution.WorkflowKind)
	}
	if child.Execution.ParentWorkflowID != "parent-wf-id" {
		t.Errorf("child ParentWorkflowID should be 'parent-wf-id', got %q", child.Execution.ParentWorkflowID)
	}
	if child.Execution.ImprovementDepth != 1 {
		t.Errorf("child ImprovementDepth should be 1, got %d", child.Execution.ImprovementDepth)
	}
	if child.DeliveryAction != "github-pr" {
		t.Errorf("child DeliveryAction should be 'github-pr', got %q", child.DeliveryAction)
	}
	if len(child.Artifacts) == 0 {
		t.Error("child workflow should have pre-populated artifacts")
	}
}
