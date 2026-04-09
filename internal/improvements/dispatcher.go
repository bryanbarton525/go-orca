package improvements

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/state"
)

// WorkflowCreator is the minimal persistence interface needed by the dispatcher
// to create and persist child improvement workflows.
// storage.WorkflowStore satisfies this interface.
type WorkflowCreator interface {
	CreateWorkflow(ctx context.Context, ws *state.WorkflowState) error
	SaveWorkflow(ctx context.Context, ws *state.WorkflowState) error
}

// WorkflowEnqueuer allows the dispatcher to schedule a child workflow for
// immediate execution.  scheduler.Scheduler satisfies this interface.
type WorkflowEnqueuer interface {
	Enqueue(workflowID string) error
}

// ConcreteDispatcher routes each RefinerImprovement to direct-apply or a child
// improvement workflow according to the locked routing policy.
//
// Its Dispatch method satisfies the engine.ImprovementDispatcher interface
// without importing the engine package (Go structural typing).
type ConcreteDispatcher struct {
	fs       *FileStore
	store    WorkflowCreator
	enqueuer WorkflowEnqueuer
}

// NewConcreteDispatcher creates a ConcreteDispatcher.
// root is the improvements root directory (e.g. artifacts/improvements).
func NewConcreteDispatcher(root string, store WorkflowCreator, enqueuer WorkflowEnqueuer) *ConcreteDispatcher {
	return &ConcreteDispatcher{
		fs:       NewFileStore(root),
		store:    store,
		enqueuer: enqueuer,
	}
}

// Dispatch applies a single RefinerImprovement per the routing policy.
//
// This method signature is compatible with engine.ImprovementDispatcher so
// that main.go can pass a *ConcreteDispatcher to engine.Options without the
// improvements package importing the engine package.
func (d *ConcreteDispatcher) Dispatch(ctx context.Context, parentWS *state.WorkflowState, imp state.RefinerImprovement) (state.ImprovementApplyResult, error) {
	result := state.ImprovementApplyResult{
		ComponentType: imp.ComponentType,
		ComponentName: imp.ComponentName,
	}

	mode := Route(imp)
	switch mode {
	case ApplyModeAdvisory:
		result.Status = "skipped"
		result.Message = "advisory-only improvement; no files written"
		return result, nil

	case ApplyModeDirect:
		return d.applyDirect(ctx, parentWS.ID, imp, result)

	case ApplyModeWorkflow:
		return d.applyViaWorkflow(ctx, parentWS, imp, result)

	default:
		result.Status = "error"
		result.Message = fmt.Sprintf("unknown apply mode %q", mode)
		return result, fmt.Errorf("improvements/dispatcher: unknown apply mode %q", mode)
	}
}

// applyDirect validates, stages, and promotes improvement files to active/.
func (d *ConcreteDispatcher) applyDirect(ctx context.Context, workflowID string, imp state.RefinerImprovement, result state.ImprovementApplyResult) (state.ImprovementApplyResult, error) {
	files := improvementFiles(imp)
	if len(files) == 0 {
		result.Status = "skipped"
		result.Message = "no files to write"
		return result, nil
	}

	// Security check first: traversal and absolute paths are hard errors.
	for _, f := range files {
		if err := ValidatePath(f.Path); err != nil {
			result.Status = "error"
			result.Message = err.Error()
			return result, err
		}
	}

	// Scope check: only persona/prompt/skill may be applied directly.
	// Out-of-scope improvements are silently skipped (not errors).
	if err := ValidateSurface(imp); err != nil {
		result.Status = "skipped"
		result.Message = err.Error()
		return result, nil
	}

	// Validate file schemas.
	if err := ValidateImprovement(imp); err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result, err
	}

	// Stage files.
	if err := d.fs.Stage(workflowID, files); err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("staging failed: %v", err)
		return result, err
	}

	// Promote to active/.
	appliedPaths, err := d.fs.Promote(workflowID, files)
	if err != nil {
		d.fs.Rollback(workflowID, files)
		result.Status = "error"
		result.Message = fmt.Sprintf("promote failed: %v", err)
		return result, err
	}

	// Write manifest (best-effort; non-fatal on failure).
	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.Path
	}
	_ = d.fs.WriteManifest(ctx, Manifest{
		WorkflowID:    workflowID,
		ComponentType: imp.ComponentType,
		ComponentName: imp.ComponentName,
		ChangeType:    imp.ChangeType,
		ApplyMode:     ApplyModeDirect,
		Files:         filePaths,
		AppliedAt:     time.Now().UTC(),
		Status:        "applied",
	})

	result.Status = "applied"
	if len(appliedPaths) > 0 {
		result.AppliedPath = appliedPaths[0] // primary path for SSE event emission
	}
	return result, nil
}

// applyViaWorkflow launches a child improvement workflow that will open a
// GitHub PR for the given improvement.
func (d *ConcreteDispatcher) applyViaWorkflow(ctx context.Context, parentWS *state.WorkflowState, imp state.RefinerImprovement, result state.ImprovementApplyResult) (state.ImprovementApplyResult, error) {
	// Enforce improvement surface before spawning a child workflow or PR.
	if err := ValidateSurface(imp); err != nil {
		result.Status = "skipped"
		result.Message = err.Error()
		return result, nil
	}

	childID, err := d.launchChildWorkflow(ctx, parentWS, imp)
	if err != nil {
		result.Status = "error"
		result.Message = fmt.Sprintf("child workflow launch failed: %v", err)
		return result, err
	}
	result.Status = "dispatched"
	result.ChildWorkflowID = childID
	result.Message = fmt.Sprintf("queued improvement workflow %s", childID)
	return result, nil
}
