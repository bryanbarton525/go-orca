package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestFilterDuplicateRemediationTasks(t *testing.T) {
	ws := &state.WorkflowState{
		Tasks: []state.Task{
			{Title: "Install Dependencies", RemediationSource: "implementation_validation", Attempt: 1},
		},
	}
	incoming := []state.Task{
		{Title: "Install Dependencies", AssignedTo: state.PersonaPod},
		{Title: "Fix package.json strict JSON syntax", AssignedTo: state.PersonaPod},
	}
	out := filterDuplicateRemediationTasks(ws, incoming, "implementation_validation", 2)
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1 (duplicate install dropped): %+v", len(out), out)
	}
	if out[0].Title != "Fix package.json strict JSON syntax" {
		t.Fatalf("got title %q", out[0].Title)
	}
}

func TestPackageJSONRemediationTask_injectedWhenInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	if err := os.WriteFile(path, []byte("// bad\n{\"name\":\"x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := &state.WorkflowState{
		ID: "wf-1",
		Execution: state.Execution{
			Workspace: &state.WorkspaceInfo{Path: dir},
		},
		BlockingIssues: []string{"validation install_dependencies failed: not valid JSON in package.json"},
	}
	task := packageJSONRemediationTask(ws, "implementation_validation", 2)
	if task == nil {
		t.Fatal("expected injected task")
	}
	if task.AssignedTo != state.PersonaPod {
		t.Fatalf("assigned_to = %q", task.AssignedTo)
	}
}
