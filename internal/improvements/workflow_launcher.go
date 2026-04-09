package improvements

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/google/uuid"
)

// launchChildWorkflow constructs and enqueues a child improvement workflow that
// will open a GitHub PR for the given improvement.
//
// The child workflow is pre-configured to skip all pipeline phases except the
// Finalizer, which executes the github-pr delivery action with the improvement
// files pre-loaded as artifacts.
func (d *ConcreteDispatcher) launchChildWorkflow(ctx context.Context, parentWS *state.WorkflowState, imp state.RefinerImprovement) (string, error) {
	request := fmt.Sprintf(
		"Self-improvement: apply %s to %s %q\n\nProblem: %s\n\nProposed fix: %s",
		changeTypeLabel(imp.ChangeType), imp.ComponentType, imp.ComponentName,
		imp.Problem, imp.ProposedFix,
	)

	now := time.Now().UTC()
	title := fmt.Sprintf("improvement(%s): update %s", imp.ComponentType, imp.ComponentName)

	childWS := &state.WorkflowState{
		ID:        uuid.New().String(),
		TenantID:  parentWS.TenantID,
		ScopeID:   parentWS.ScopeID,
		Status:    state.WorkflowStatusPending,
		Title:     title,
		Request:   request,
		Summaries: make(map[state.PersonaKind]string),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Mark as improvement workflow for the recursion guard.
	childWS.Execution.WorkflowKind = "improvement"
	childWS.Execution.ParentWorkflowID = parentWS.ID
	childWS.Execution.ImprovementDepth = parentWS.Execution.ImprovementDepth + 1

	// Pre-populate Director summary to skip the Director phase.
	childWS.Summaries[state.PersonaDirector] = "(pre-populated: improvement workflow)"

	// Only run the Finalizer.
	childWS.RequiredPersonas = []state.PersonaKind{state.PersonaFinalizer}

	// Build a short ID for the branch name.
	shortID := childWS.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	headBranch := fmt.Sprintf("improvements/%s-%s", imp.ComponentName, shortID)

	deliveryCfg := map[string]string{
		"repo":        "bryanbarton525/go-orca",
		"base_branch": "main",
		"head_branch": headBranch,
	}
	cfgJSON, err := json.Marshal(deliveryCfg)
	if err != nil {
		return "", fmt.Errorf("improvements/workflow_launcher: marshal delivery config: %w", err)
	}
	childWS.DeliveryAction = "github-pr"
	childWS.FinalizerAction = "github-pr"
	childWS.DeliveryConfig = cfgJSON

	// Pre-populate artifacts with improvement files.
	for _, f := range improvementFiles(imp) {
		childWS.Artifacts = append(childWS.Artifacts, state.Artifact{
			ID:          uuid.New().String(),
			WorkflowID:  childWS.ID,
			Kind:        artifactKindForPath(f.Path),
			Name:        filepath.Base(f.Path),
			Description: fmt.Sprintf("%s improvement: %s", imp.ComponentType, imp.ComponentName),
			Path:        f.Path,
			Content:     f.Content,
			CreatedBy:   state.PersonaRefiner,
			CreatedAt:   now,
		})
	}

	if err := d.store.CreateWorkflow(ctx, childWS); err != nil {
		return "", fmt.Errorf("improvements/workflow_launcher: create workflow: %w", err)
	}
	if err := d.enqueuer.Enqueue(childWS.ID); err != nil {
		return "", fmt.Errorf("improvements/workflow_launcher: enqueue workflow %s: %w", childWS.ID, err)
	}
	return childWS.ID, nil
}

// artifactKindForPath derives an ArtifactKind from a relative file path.
func artifactKindForPath(path string) state.ArtifactKind {
	switch filepath.Ext(path) {
	case ".md":
		return state.ArtifactKindMarkdown
	default:
		return state.ArtifactKindDocument
	}
}

// changeTypeLabel returns a human-readable label for a change type.
func changeTypeLabel(ct string) string {
	switch ct {
	case "create":
		return "creation of"
	case "update":
		return "update to"
	case "advisory":
		return "advisory note on"
	default:
		return "change to"
	}
}
