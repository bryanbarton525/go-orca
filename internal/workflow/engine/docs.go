package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine/docrender"
)

// materializeConstitution writes constitution.md from the PM-authored
// Constitution + Requirements. When the workflow has a workspace it lands on
// disk and (when a checkpoint capability is configured) gets committed to the
// workflow branch. Otherwise it is persisted as an Artifact so downstream
// personas still receive it via BuildHandoffContext.
//
// Safe to call multiple times: the first PM run materializes the canonical
// document; subsequent calls (which today don't happen because PM only runs
// once outside triage) would overwrite it.
func (e *Engine) materializeConstitution(ctx context.Context, ws *state.WorkflowState) error {
	if ws == nil || ws.Constitution == nil {
		return nil
	}
	body := docrender.RenderConstitution(ws.Constitution, ws.Requirements)
	return e.persistDoc(ctx, ws, docrender.ConstitutionFile, body, state.PersonaProjectMgr, "constitution-initial", false)
}

// materializePlan writes the initial plan.md from the Architect-authored
// Design + Tasks. Initial pass only: remediation cycles append via
// appendPlanRemediation.
func (e *Engine) materializePlan(ctx context.Context, ws *state.WorkflowState) error {
	if ws == nil || (ws.Design == nil && len(ws.Tasks) == 0) {
		return nil
	}
	body := docrender.RenderPlan(ws.Design, ws.Tasks)
	return e.persistDoc(ctx, ws, docrender.PlanFile, body, state.PersonaArchitect, "plan-initial", false)
}

// appendPlanTriage appends a PM remediation-triage section to plan.md.
// brief is the PM's free-text summary for this cycle (typically the latest
// suffix added to ws.Summaries[PM]); blockingIssues are the QA findings the
// PM was triaging.
//
// When brief contains the case-insensitive phrase "requirement gap" (the
// PM prompt instructs the model to use this phrase for its formal
// classification), a Constitution Amendment block is also appended to
// constitution.md so the original charter remains intact while the new
// requirement is documented.
func (e *Engine) appendPlanTriage(ctx context.Context, ws *state.WorkflowState, cycle int, brief string, blockingIssues []string) error {
	if ws == nil {
		return nil
	}
	section := docrender.RenderTriageSection(cycle, brief, blockingIssues)
	if err := e.persistDoc(ctx, ws, docrender.PlanFile, section, state.PersonaProjectMgr,
		fmt.Sprintf("plan-triage-%d", cycle), true); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(brief), "requirement gap") {
		amendment := docrender.RenderConstitutionAmendment(cycle, brief)
		if err := e.persistDoc(ctx, ws, docrender.ConstitutionFile, amendment, state.PersonaProjectMgr,
			fmt.Sprintf("constitution-amendment-%d", cycle), true); err != nil {
			return err
		}
	}
	return nil
}

// appendPlanRemediation appends an Architect remediation-planning section to
// plan.md, listing only tasks tagged with the given cycle.
func (e *Engine) appendPlanRemediation(ctx context.Context, ws *state.WorkflowState, cycle int) error {
	if ws == nil {
		return nil
	}
	section := docrender.RenderRemediationSection(cycle, ws.Design, ws.Tasks)
	return e.persistDoc(ctx, ws, docrender.PlanFile, section, state.PersonaArchitect,
		fmt.Sprintf("plan-remediation-%d", cycle), true)
}

// persistDoc dispatches between disk-write+checkpoint (workspace present) and
// artifact-store (no workspace). When append is true, existing on-disk content
// is preserved and the new body is appended; for the artifact path "append"
// means the new content is concatenated onto the latest stored artifact's
// Content rather than replacing it.
//
// checkpointPhase is passed through to runToolchainCheckpoint for the git
// commit message label so the design-doc commit is distinguishable from code
// checkpoints in `git log`.
func (e *Engine) persistDoc(
	ctx context.Context,
	ws *state.WorkflowState,
	name, body string,
	createdBy state.PersonaKind,
	checkpointPhase string,
	appendMode bool,
) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}

	if ws.Execution.Workspace != nil && strings.TrimSpace(ws.Execution.Workspace.Path) != "" {
		path := filepath.Join(ws.Execution.Workspace.Path, name)
		if appendMode {
			if existing, err := os.ReadFile(path); err == nil {
				body = strings.TrimRight(string(existing), "\n") + "\n" + body
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		// Best-effort git checkpoint of the doc. runToolchainCheckpoint already
		// handles "no toolchain configured" by returning nil; failures surface
		// as suggestions rather than aborting the workflow.
		if cerr := e.runToolchainCheckpoint(ctx, ws, checkpointPhase); cerr != nil {
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[doc-checkpoint] %s checkpoint failed: %v", name, cerr))
		}
		return e.store.SaveWorkflow(ctx, ws)
	}

	// No workspace — store as an Artifact so BuildHandoffContext can still
	// surface it. latestArtifactsByLogicalFile (engine.go) already collapses
	// duplicates by name+kind, so appending a new entry is correct: the latest
	// one wins for downstream personas.
	finalContent := body
	if appendMode {
		// Find the most recent artifact with this name and concat onto its
		// content so triage/remediation sections accumulate.
		for i := len(ws.Artifacts) - 1; i >= 0; i-- {
			a := ws.Artifacts[i]
			if a.Name == name && a.Kind == state.ArtifactKindMarkdown {
				finalContent = strings.TrimRight(a.Content, "\n") + "\n" + body
				break
			}
		}
	}
	ws.Artifacts = append(ws.Artifacts, state.Artifact{
		ID:         uuid.New().String(),
		WorkflowID: ws.ID,
		Kind:       state.ArtifactKindMarkdown,
		Name:       name,
		Content:    finalContent,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC(),
	})
	return e.store.SaveWorkflow(ctx, ws)
}

// loadDocForState is the engine-internal counterpart to the persona-side
// loader (persona/base.LoadWorkflowDoc). Used by tests in this package; the
// persona layer has its own packet-based loader so it does not import engine.
func loadDocForState(ws *state.WorkflowState, name string) string {
	if ws == nil {
		return ""
	}
	if ws.Execution.Workspace != nil && strings.TrimSpace(ws.Execution.Workspace.Path) != "" {
		path := filepath.Join(ws.Execution.Workspace.Path, name)
		if b, err := os.ReadFile(path); err == nil {
			return string(b)
		}
	}
	for i := len(ws.Artifacts) - 1; i >= 0; i-- {
		a := ws.Artifacts[i]
		if a.Name == name && a.Kind == state.ArtifactKindMarkdown {
			return a.Content
		}
	}
	return ""
}
