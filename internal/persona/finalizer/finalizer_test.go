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
