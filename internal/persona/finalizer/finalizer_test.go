package finalizer

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

// ─── normalizeImprovements ────────────────────────────────────────────────────

func TestNormalizeImprovements_DropsBlankComponentName(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "implementer" {
		t.Errorf("wrong improvement retained: %+v", result[0])
	}
}

func TestNormalizeImprovements_DropsBlankComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "medium"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d: %+v", len(result), result)
	}
}

func TestNormalizeImprovements_DropsBlankProblem(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "", ProposedFix: "f", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsBlankProposedFix(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "p", ProposedFix: "", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsInvalidPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "urgent"},
		{ComponentType: "persona", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "medium"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "director" {
		t.Errorf("wrong improvement retained: %+v", result[0])
	}
}

func TestNormalizeImprovements_DropsBlankPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "p", ProposedFix: "f", Priority: ""},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements with blank priority, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsValidImprovement(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "prob", ProposedFix: "fix", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "my-skill" {
		t.Errorf("unexpected component name: %q", result[0].ComponentName)
	}
}

func TestNormalizeImprovements_EmptyInput(t *testing.T) {
	result := normalizeImprovements(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_TrimsWhitespace(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "  persona  ", ComponentName: "  implementer  ", Problem: "  p  ", ProposedFix: "  f  ", Priority: "  high  "},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after trimming, got %d", len(result))
	}
	if result[0].ComponentType != "persona" {
		t.Errorf("ComponentType not trimmed: %q", result[0].ComponentType)
	}
	if result[0].ComponentName != "implementer" {
		t.Errorf("ComponentName not trimmed: %q", result[0].ComponentName)
	}
	if result[0].Priority != "high" {
		t.Errorf("Priority not trimmed/lowercased: %q", result[0].Priority)
	}
}

func TestNormalizeImprovements_NormalizesPriorityCase(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "HIGH"},
		{ComponentType: "persona", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "Medium"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "LOW"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 3 {
		t.Fatalf("expected 3 improvements after case normalization, got %d", len(result))
	}
	if result[0].Priority != "high" {
		t.Errorf("expected 'high', got %q", result[0].Priority)
	}
	if result[1].Priority != "medium" {
		t.Errorf("expected 'medium', got %q", result[1].Priority)
	}
	if result[2].Priority != "low" {
		t.Errorf("expected 'low', got %q", result[2].Priority)
	}
}

func TestNormalizeImprovements_DropsPlaceholderComponentNames(t *testing.T) {
	placeholders := []string{"N/A", "n/a", "NA", "unknown", "placeholder", "TBD", "tbd"}
	for _, name := range placeholders {
		imps := []state.RefinerImprovement{
			{ComponentType: "persona", ComponentName: name, Problem: "p", ProposedFix: "f", Priority: "low"},
		}
		result := normalizeImprovements(imps)
		if len(result) != 0 {
			t.Errorf("component_name %q should be rejected as placeholder, got %d results", name, len(result))
		}
	}
}

// ─── deduplication ────────────────────────────────────────────────────────────

func TestNormalizeImprovements_DeduplicatesByComponentTypeAndName(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "p1", ProposedFix: "f1", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p2", ProposedFix: "f2", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p3", ProposedFix: "f3", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after dedup, got %d", len(result))
	}
	if result[0].Problem != "p1" {
		t.Errorf("expected first-seen entry retained, got problem=%q", result[0].Problem)
	}
}

func TestNormalizeImprovements_DeduplicateKeepsHigherPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "low-p", ProposedFix: "f1", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "high-p", ProposedFix: "f2", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after dedup, got %d", len(result))
	}
	if result[0].Priority != "high" {
		t.Errorf("expected higher-priority entry, got priority=%q problem=%q", result[0].Priority, result[0].Problem)
	}
}

func TestNormalizeImprovements_DifferentComponentsNotDeduped(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "skill", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 3 {
		t.Fatalf("expected 3 improvements (different keys), got %d", len(result))
	}
}

func TestNormalizeImprovements_PreservesContent(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "p", ProposedFix: "f", Priority: "high", Content: "# My Skill\n"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].Content != "# My Skill" {
		// Content is trimmed.
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

// ─── surface policy filtering inside normalizeImprovements ───────────────────

func TestNormalizeImprovements_DropsAgentComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "agent", ComponentName: "my-agent", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "agents/my-agent.agent.md", Content: "# agent"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for agent type, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsWorkflowComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "workflow", ComponentName: "engine", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "advisory"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for workflow type, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsGoSourceFilePath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "engine", Problem: "p", ProposedFix: "f", Priority: "high",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "internal/workflow/engine/engine.go", Content: "package engine"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for Go source path, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsPersonaWithAllowedPath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "prompts/personas/implementer.md", Content: "# impl"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement for valid persona path, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsSkillWithAllowedPath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "skills/my-skill/SKILL.md", Content: "---\nname: my-skill\ndescription: d\n---\n"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement for valid skill path, got %d", len(result))
	}
}
