package refiner

import (
	"testing"
)

// ─── normalizeImprovements ────────────────────────────────────────────────────

func TestRefinerNormalize_DropsBlankComponentName(t *testing.T) {
	imps := []Improvement{
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

func TestRefinerNormalize_DropsInvalidPriority(t *testing.T) {
	imps := []Improvement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "critical"},
		{ComponentType: "persona", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "director" {
		t.Errorf("wrong improvement retained: %+v", result[0])
	}
}

func TestRefinerNormalize_DropsBlankPriority(t *testing.T) {
	imps := []Improvement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "p", ProposedFix: "f", Priority: ""},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements with blank priority, got %d", len(result))
	}
}

func TestRefinerNormalize_KeepsValidImprovement(t *testing.T) {
	imps := []Improvement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "prob", ProposedFix: "fix", Priority: "medium"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
}

func TestRefinerNormalize_EmptyInput(t *testing.T) {
	result := normalizeImprovements(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestRefinerNormalize_NormalizesPriorityCase(t *testing.T) {
	imps := []Improvement{
		{ComponentType: "persona", ComponentName: "a", Problem: "p", ProposedFix: "f", Priority: "HIGH"},
		{ComponentType: "persona", ComponentName: "b", Problem: "p", ProposedFix: "f", Priority: "Medium"},
		{ComponentType: "persona", ComponentName: "c", Problem: "p", ProposedFix: "f", Priority: "LOW"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 3 {
		t.Fatalf("expected 3 improvements, got %d", len(result))
	}
	expected := []string{"high", "medium", "low"}
	for i, imp := range result {
		if imp.Priority != expected[i] {
			t.Errorf("[%d] priority: got %q, want %q", i, imp.Priority, expected[i])
		}
	}
}

func TestRefinerNormalize_TrimsWhitespace(t *testing.T) {
	imps := []Improvement{
		{ComponentType: "  skill  ", ComponentName: "  my-skill  ", Problem: "  prob  ", ProposedFix: "  fix  ", Priority: "  high  "},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "my-skill" {
		t.Errorf("ComponentName not trimmed: %q", result[0].ComponentName)
	}
	if result[0].Priority != "high" {
		t.Errorf("Priority not trimmed: %q", result[0].Priority)
	}
}
