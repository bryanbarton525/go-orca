package engine

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestRemediationTaskSubset(t *testing.T) {
	t.Parallel()
	if remediationTaskSubset(nil) {
		t.Fatal("nil tasks")
	}
	if remediationTaskSubset([]state.Task{{Title: "x"}}) {
		t.Fatal("non-remediation task")
	}
	tasks := []state.Task{
		{Title: "fix deps", RemediationSource: "implementation_validation"},
	}
	if !remediationTaskSubset(tasks) {
		t.Fatal("expected remediation subset")
	}
}
