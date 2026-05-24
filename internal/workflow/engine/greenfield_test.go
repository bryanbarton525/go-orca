package engine

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestToolchainMatchesRequest_GoVsNextJS(t *testing.T) {
	ws := state.NewWorkflowState("t", "s", "Using nextjs create a web app")
	ws.Execution.Toolchain = &state.ToolchainSelection{ID: "go"}
	if toolchainMatchesRequest(ws) {
		t.Fatal("go toolchain should not match nextjs request")
	}
}
