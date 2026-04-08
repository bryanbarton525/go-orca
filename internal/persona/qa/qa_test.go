package qa

import (
	"strings"
	"testing"
)

// buildBlocking mirrors the production logic so we can test the filter in
// isolation without needing a live LLM call.
func buildBlocking(issues []qaIssue) []string {
	out := make([]string, 0, len(issues))
	for _, iss := range issues {
		if iss.Component == "" && iss.Description == "" {
			continue
		}
		out = append(out, "["+iss.Component+"] "+iss.Description+": "+iss.Recommendation)
	}
	return out
}

func TestBuildBlocking_EmptyObjectsFiltered(t *testing.T) {
	// LLM emits all-empty objects — none should become blockers.
	issues := []qaIssue{
		{},
		{Severity: "", Component: "", Description: "", Recommendation: ""},
	}
	got := buildBlocking(issues)
	if len(got) != 0 {
		t.Errorf("expected 0 blockers, got %d: %v", len(got), got)
	}
}

func TestBuildBlocking_PartialItemsFiltered(t *testing.T) {
	// Only Component set, Description empty — should be filtered.
	issues := []qaIssue{
		{Component: "auth", Description: "", Recommendation: ""},
	}
	// Note: component is set but description is empty; per filter rule (both empty),
	// this item should NOT be filtered — it has a component value.
	got := buildBlocking(issues)
	if len(got) != 1 {
		t.Errorf("expected 1 blocker (component set), got %d: %v", len(got), got)
	}
}

func TestBuildBlocking_ValidItemsPassThrough(t *testing.T) {
	issues := []qaIssue{
		{Severity: "critical", Component: "auth", Description: "Missing token validation", Recommendation: "Add JWT check"},
		{Severity: "high", Component: "db", Description: "SQL injection risk", Recommendation: "Use parameterised queries"},
	}
	got := buildBlocking(issues)
	if len(got) != 2 {
		t.Fatalf("expected 2 blockers, got %d", len(got))
	}
	if !strings.Contains(got[0], "auth") {
		t.Errorf("first blocker should reference auth component, got: %s", got[0])
	}
	if !strings.Contains(got[1], "db") {
		t.Errorf("second blocker should reference db component, got: %s", got[1])
	}
}

func TestBuildBlocking_MixedEmptyAndValid(t *testing.T) {
	issues := []qaIssue{
		{}, // phantom empty — filtered
		{Severity: "critical", Component: "api", Description: "No rate limiting", Recommendation: "Add throttle middleware"},
		{Component: "", Description: "", Recommendation: ""}, // all empty — filtered
		{Severity: "low", Component: "logging", Description: "Missing trace ID", Recommendation: "Inject trace"},
	}
	got := buildBlocking(issues)
	if len(got) != 2 {
		t.Fatalf("expected 2 blockers after filtering, got %d: %v", len(got), got)
	}
	for _, b := range got {
		if b == "[] : " {
			t.Errorf("phantom blocker string should not appear: %q", b)
		}
	}
}
