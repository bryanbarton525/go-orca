package state

import "testing"

func TestResolveAndSanitizeTaskDependencies(t *testing.T) {
	t.Parallel()

	tasks := []Task{
		{ID: "a", Title: "Bootstrap module"},
		{
			ID:        "b",
			Title:     "Create HTTP handlers for newspaper view",
			DependsOn: []string{"Create HTTP handlers for newspaper view"},
		},
		{
			ID:        "c",
			Title:     "Wire router in main.go",
			DependsOn: []string{"Create HTTP handlers", "Implement OIDC middleware for request authentication"},
		},
	}

	fixed, warnings := ResolveAndSanitizeTaskDependencies(tasks)
	if len(warnings) < 2 {
		t.Fatalf("expected warnings for self and dropped deps, got %v", warnings)
	}
	if len(fixed[1].DependsOn) != 0 {
		t.Fatalf("self-dependency should be removed, got %v", fixed[1].DependsOn)
	}
	if len(fixed[2].DependsOn) != 1 || fixed[2].DependsOn[0] != "b" {
		t.Fatalf("expected dependency on b only, got %v", fixed[2].DependsOn)
	}
}
