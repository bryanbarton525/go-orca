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

func TestResolveAndSanitizeTaskDependencies_BreaksSimpleCycle(t *testing.T) {
	t.Parallel()

	tasks := []Task{
		{ID: "a", Title: "Create store.go", DependsOn: []string{"b"}},
		{ID: "b", Title: "Update main.go", DependsOn: []string{"a"}},
	}

	fixed, warnings := ResolveAndSanitizeTaskDependencies(tasks)
	if len(warnings) == 0 {
		t.Fatalf("expected cycle warning, got none")
	}
	if len(fixed[0].DependsOn) > 0 && len(fixed[1].DependsOn) > 0 {
		t.Fatalf("expected at least one dependency edge removed to break cycle, got a=%v b=%v",
			fixed[0].DependsOn, fixed[1].DependsOn)
	}
}

func TestResolveAndSanitizeTaskDependencies_BreaksLongCycle(t *testing.T) {
	t.Parallel()

	tasks := []Task{
		{ID: "a", Title: "Create store.go", DependsOn: []string{"b"}},
		{ID: "b", Title: "Create handlers.go", DependsOn: []string{"c"}},
		{ID: "c", Title: "Update main.go", DependsOn: []string{"a"}},
	}

	fixed, _ := ResolveAndSanitizeTaskDependencies(tasks)
	seenWithDeps := 0
	for _, t := range fixed {
		if len(t.DependsOn) > 0 {
			seenWithDeps++
		}
	}
	if seenWithDeps >= len(fixed) {
		t.Fatalf("expected at least one edge removed from long cycle, got %+v", fixed)
	}
}
