package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
)

type countingTool struct {
	name  string
	calls int
}

func (t *countingTool) Name() string                { return t.name }
func (t *countingTool) Description() string         { return "counts tool calls" }
func (t *countingTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *countingTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return json.RawMessage(`{"passed":true}`), nil
}

func TestRunToolchainValidation_FailsFastWhenGoWorkspaceHasNoSourceFiles(t *testing.T) {
	reg := tools.NewRegistry()
	runTests := &countingTool{name: "go_test"}
	reg.Register(runTests)

	eng := New(noopStore{}, Options{
		DefaultProvider: "mock",
		DefaultModel:    "mock",
		ToolRegistry:    reg,
		Toolchains: []ToolchainConfig{{
			ID:                 "go",
			Languages:          []string{"go", "golang"},
			Capabilities:       []string{"run_tests"},
			CapabilityTools:    map[string]string{"run_tests": "go_test"},
			ValidationProfiles: map[string][]string{"default": {"run_tests"}},
		}},
	})

	ws, _ := newWSWithWorkspace(t)
	ws.Execution.Toolchain = &state.ToolchainSelection{ID: "go", Language: "go", Profile: "default"}

	issues, err := eng.runToolchainValidation(context.Background(), ws, "implementation")
	if err != nil {
		t.Fatalf("runToolchainValidation() error = %v", err)
	}
	if runTests.calls != 0 {
		t.Fatalf("go_test tool call count = %d, want 0", runTests.calls)
	}
	if len(issues) != 1 {
		t.Fatalf("issues len = %d, want 1 (%v)", len(issues), issues)
	}
	if got := issues[0]; got == "" || got[:14] != "[Source Files]" {
		t.Fatalf("issue = %q, want source-files preflight issue", got)
	}
	if len(ws.Execution.ValidationRuns) != 1 {
		t.Fatalf("validation run count = %d, want 1", len(ws.Execution.ValidationRuns))
	}
	run := ws.Execution.ValidationRuns[0]
	if run.Passed {
		t.Fatal("expected validation run to fail")
	}
	if len(run.Steps) != 1 || run.Steps[0].Capability != "workspace_preflight" {
		t.Fatalf("validation steps = %+v, want single workspace_preflight step", run.Steps)
	}
}