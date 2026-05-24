package engine

import (
	"strings"

	"github.com/go-orca/go-orca/internal/state"
)

// toolchainMatchesRequest returns false when the selected toolchain clearly
// disagrees with an explicit stack mention in the request (e.g. Go vs Next.js).
func toolchainMatchesRequest(ws *state.WorkflowState) bool {
	if ws == nil || ws.Execution.Toolchain == nil {
		return true
	}
	hay := strings.ToLower(ws.Request + " " + ws.Title)
	tcID := strings.ToLower(ws.Execution.Toolchain.ID)
	switch tcID {
	case "go", "golang":
		if strings.Contains(hay, "nextjs") || strings.Contains(hay, "next.js") ||
			strings.Contains(hay, "react") {
			return false
		}
	case "nextjs", "node":
		if strings.Contains(hay, "golang") || strings.Contains(hay, " go ") {
			return false
		}
	}
	return true
}
