package engine

import (
	"context"
	"strings"

	"github.com/go-orca/go-orca/internal/state"
)

// runToolchainCheckpointUnlessMinimal skips git checkpoints on high-frequency
// remediation micro-phases when MinimalCheckpoints is enabled.
func (e *Engine) runToolchainCheckpointUnlessMinimal(ctx context.Context, ws *state.WorkflowState, phase string) error {
	if e.opts.MinimalCheckpoints && isRemediationMicroCheckpoint(phase) {
		return nil
	}
	return e.runToolchainCheckpoint(ctx, ws, phase)
}

func isRemediationMicroCheckpoint(phase string) bool {
	p := strings.ToLower(phase)
	if strings.Contains(p, "-plan-") || strings.Contains(p, "-bootstrap") {
		return true
	}
	if strings.HasSuffix(p, "-bootstrap") {
		return true
	}
	return false
}
