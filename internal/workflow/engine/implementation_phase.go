package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

// implementationLoopOpts configures runImplementationValidationLoop.
type implementationLoopOpts struct {
	runInitialPod     bool
	phasePrefix       string
	remediationSource string
	maxRetries        int
}

// runImplementationPhase runs Pod work and iterates on validation, documentation,
// and git checkpoint requirements before QA verification.
func (e *Engine) runImplementationPhase(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot) error {
	if err := e.ensureToolchainBootstrap(ctx, ws, "implementation-bootstrap"); err != nil {
		return fmt.Errorf("implementation bootstrap: %w", err)
	}
	if err := e.runToolchainCheckpoint(ctx, ws, "implementation-bootstrap"); err != nil {
		return fmt.Errorf("implementation bootstrap checkpoint: %w", err)
	}

	needsToolchain := workflowNeedsToolchain(ws.Mode) && ws.Execution.Toolchain != nil
	if !needsToolchain {
		if err := e.runPodPhase(ctx, ws, snap); err != nil {
			return err
		}
		return e.markImplementationReady(ctx, ws, "", nil)
	}

	return e.runImplementationValidationLoop(ctx, ws, snap, implementationLoopOpts{
		runInitialPod:     true,
		phasePrefix:       "implementation",
		remediationSource: "implementation_validation",
		maxRetries:        e.maxImplementationRetries(),
	})
}

// runImplementationValidationLoop executes Pod tasks and repeats validate →
// checkpoint → documentation → git evidence checks until all pass or retries
// are exhausted. Used for the initial implementation phase and for QA-cycle
// Pod remediation (same build/test/fix behavior).
func (e *Engine) runImplementationValidationLoop(
	ctx context.Context,
	ws *state.WorkflowState,
	snap *customization.Snapshot,
	opts implementationLoopOpts,
) error {
	if opts.runInitialPod {
		if err := e.runPodPhase(ctx, ws, snap); err != nil {
			return err
		}
	}

	maxRetries := opts.maxRetries
	if maxRetries == 0 {
		maxRetries = e.maxImplementationRetries()
	}

	for cycle := 1; ; cycle++ {
		ws.Execution.ImplementationCycle = cycle
		phase := opts.phasePrefix
		if cycle > 1 {
			phase = fmt.Sprintf("%s-remediation-%d", opts.phasePrefix, cycle-1)
		}
		_ = e.store.SaveWorkflow(ctx, ws)

		validationIssues, err := e.runToolchainValidation(ctx, ws, phase)
		if err != nil {
			return fmt.Errorf("toolchain validation (%s): %w", phase, err)
		}
		if err := e.runToolchainCheckpoint(ctx, ws, phase); err != nil {
			return fmt.Errorf("toolchain checkpoint (%s): %w", phase, err)
		}

		allIssues := append([]string(nil), validationIssues...)
		allIssues = append(allIssues, e.verifyImplementationDocumentation(ws)...)
		allIssues = append(allIssues, e.verifyImplementationCheckpoint(ws, phase)...)

		if len(allIssues) == 0 {
			if err := e.markImplementationReady(ctx, ws, phase, validationIssues); err != nil {
				return err
			}
			ws.BlockingIssues = nil
			return e.store.SaveWorkflow(ctx, ws)
		}

		ws.BlockingIssues = allIssues
		if err := e.store.SaveWorkflow(ctx, ws); err != nil {
			return err
		}
		if maxRetries >= 0 && cycle > maxRetries {
			exhaustedEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventImplementationExhausted, state.PersonaPod,
				events.ImplementationExhaustedPayload{
					RetriesAllowed: maxRetries,
					BlockingIssues: allIssues,
					Cycle:          cycle,
				})
			_ = e.store.AppendEvents(ctx, exhaustedEvt)
			note := fmt.Sprintf("[%s] %d issue(s) unresolved after %d cycle(s)",
				opts.remediationSource, len(allIssues), cycle)
			ws.AllSuggestions = append(ws.AllSuggestions, note)
			return fmt.Errorf("%s exhausted after %d cycle(s): %v", opts.remediationSource, cycle, allIssues)
		}

		if !personaRequiredForWorkflow(ws, state.PersonaArchitect) {
			return fmt.Errorf("%s failed (cycle %d): %v", opts.remediationSource, cycle, allIssues)
		}

		ws.Execution.CurrentPersona = state.PersonaArchitect
		ws.Execution.RemediationAttempt = cycle
		_ = e.store.SaveWorkflow(ctx, ws)

		if err := e.runRemediationPlanning(ctx, ws, snap, cycle, opts.remediationSource); err != nil {
			return fmt.Errorf("%s planning (cycle %d): %w", opts.remediationSource, cycle, err)
		}
		planPhase := fmt.Sprintf("%s-plan-%d", opts.phasePrefix, cycle)
		if err := e.runToolchainCheckpointUnlessMinimal(ctx, ws, planPhase); err != nil {
			return fmt.Errorf("%s plan checkpoint (cycle %d): %w", opts.remediationSource, cycle, err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}

		ws.Execution.CurrentPersona = state.PersonaPod
		_ = e.store.SaveWorkflow(ctx, ws)

		bootstrapPhase := fmt.Sprintf("%s-remediation-%d-bootstrap", opts.phasePrefix, cycle)
		if err := e.ensureToolchainBootstrap(ctx, ws, bootstrapPhase); err != nil {
			return fmt.Errorf("%s bootstrap (cycle %d): %w", opts.remediationSource, cycle, err)
		}
		if err := e.runToolchainCheckpointUnlessMinimal(ctx, ws, bootstrapPhase); err != nil {
			return fmt.Errorf("%s bootstrap checkpoint (cycle %d): %w", opts.remediationSource, cycle, err)
		}
		if err := e.runPodPhase(ctx, ws, snap); err != nil {
			return fmt.Errorf("%s pod (cycle %d): %w", opts.remediationSource, cycle, err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}
}

func (e *Engine) maxImplementationRetries() int {
	if e.opts.MaxImplementationRetries != 0 {
		return e.opts.MaxImplementationRetries
	}
	return e.opts.MaxQARetries
}

// assertImplementationReadyForQA ensures QA only runs after implementation
// validation, documentation, and checkpoint requirements succeeded.
func (e *Engine) assertImplementationReadyForQA(ws *state.WorkflowState) error {
	if !workflowNeedsToolchain(ws.Mode) {
		return nil
	}
	if ws.Execution.Toolchain == nil {
		return nil
	}
	gate := ws.Execution.ImplementationGate
	if gate != nil && gate.Ready {
		return nil
	}
	if blocked, run := lastValidationFailed(ws); blocked {
		phase := ""
		if run != nil {
			phase = run.Phase
		}
		return fmt.Errorf("implementation validation has not passed (last phase %q)", phase)
	}
	if gate == nil || !gate.DocumentationOK {
		return fmt.Errorf("implementation documentation gate has not passed")
	}
	if gate == nil || strings.TrimSpace(gate.CheckpointSHA) == "" {
		if len(ws.Execution.Checkpoints) == 0 {
			return fmt.Errorf("implementation git checkpoint missing — latest iteration must be committed before QA")
		}
	}
	return fmt.Errorf("implementation gate not ready for QA")
}

func (e *Engine) markImplementationReady(
	ctx context.Context,
	ws *state.WorkflowState,
	validationPhase string,
	validationIssues []string,
) error {
	gate := &state.ImplementationGate{
		Ready:                 true,
		ValidationPassed:      len(validationIssues) == 0,
		LatestValidationPhase: validationPhase,
		DocumentationOK:       len(e.verifyImplementationDocumentation(ws)) == 0,
		VerifiedAt:            time.Now().UTC(),
	}
	if cp := latestCheckpointForPhase(ws, validationPhase); cp != nil {
		gate.CheckpointSHA = cp.CommitSHA
		gate.CheckpointPhase = cp.Phase
	} else if cp := latestCheckpoint(ws); cp != nil {
		gate.CheckpointSHA = cp.CommitSHA
		gate.CheckpointPhase = cp.Phase
	}
	ws.Execution.ImplementationGate = gate

	readyEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventImplementationReady, state.PersonaPod,
		events.ImplementationReadyPayload{
			ValidationPhase: validationPhase,
			CheckpointSHA:   gate.CheckpointSHA,
			DocumentationOK: gate.DocumentationOK,
		})
	_ = e.store.AppendEvents(ctx, readyEvt)
	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) verifyImplementationDocumentation(ws *state.WorkflowState) []string {
	if ws == nil || ws.Execution.Workspace == nil || strings.TrimSpace(ws.Execution.Workspace.Path) == "" {
		return nil
	}
	if !workflowNeedsToolchain(ws.Mode) {
		return nil
	}
	root := filepath.Clean(ws.Execution.Workspace.Path)
	candidates := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "readme.md"),
		filepath.Join(root, "docs", "README.md"),
		filepath.Join(root, "plan.md"),
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > 0 {
			return nil
		}
	}
	return []string{
		"implementation documentation missing or empty: add or update README.md, docs/README.md, or plan.md in the workspace before QA",
	}
}

func (e *Engine) verifyImplementationCheckpoint(ws *state.WorkflowState, phase string) []string {
	if ws == nil || ws.Execution.Toolchain == nil || e.opts.ToolRegistry == nil {
		return nil
	}
	tc, ok := e.toolchainByID(ws.Execution.Toolchain.ID)
	if !ok || strings.TrimSpace(tc.CheckpointCapability) == "" {
		return nil
	}
	cp := latestCheckpointForPhase(ws, phase)
	if cp == nil {
		cp = latestCheckpoint(ws)
	}
	if cp == nil || strings.TrimSpace(cp.CommitSHA) == "" {
		return []string{
			fmt.Sprintf("implementation git checkpoint missing for phase %q — run git_checkpoint and commit the latest iteration before QA", phase),
		}
	}
	return nil
}

func latestCheckpoint(ws *state.WorkflowState) *state.Checkpoint {
	if ws == nil || len(ws.Execution.Checkpoints) == 0 {
		return nil
	}
	cp := ws.Execution.Checkpoints[len(ws.Execution.Checkpoints)-1]
	return &cp
}

func latestCheckpointForPhase(ws *state.WorkflowState, phase string) *state.Checkpoint {
	if ws == nil || len(ws.Execution.Checkpoints) == 0 {
		return nil
	}
	phase = strings.TrimSpace(phase)
	for i := len(ws.Execution.Checkpoints) - 1; i >= 0; i-- {
		cp := ws.Execution.Checkpoints[i]
		if phase == "" || cp.Phase == phase {
			return &cp
		}
	}
	return nil
}

// EnforceFinalizerValidationGate exposes the finalizer validation gate for tests.
func (e *Engine) EnforceFinalizerValidationGate(ctx context.Context, ws *state.WorkflowState) error {
	return e.enforceFinalizerValidationGate(ctx, ws)
}

// enforceFinalizerValidationGate blocks finalization when the most recent
// toolchain validation failed (e.g. QA passed visually but tests did not).
func (e *Engine) enforceFinalizerValidationGate(ctx context.Context, ws *state.WorkflowState) error {
	if !e.opts.EnforceValidationGate || !workflowNeedsToolchain(ws.Mode) {
		return nil
	}
	blocked, run := lastValidationFailed(ws)
	if !blocked {
		return nil
	}
	issues := []string{}
	if run != nil {
		issues = append(issues, run.Summary)
		for _, step := range run.Steps {
			if !step.Passed {
				issues = append(issues, fmt.Sprintf("%s: %s", step.Capability, firstNonEmpty(step.Error, step.Output)))
			}
		}
	}
	toolchainID := ""
	profile := ""
	if run != nil {
		toolchainID = run.ToolchainID
		profile = run.Profile
	}
	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventValidationGateBlocked, "",
		events.ValidationGateBlockedPayload{
			ToolchainID: toolchainID,
			Profile:     profile,
			Phase:       "finalizer-gate",
			Issues:      issues,
		})
	_ = e.store.AppendEvents(ctx, evt)
	ws.Status = state.WorkflowStatusFailed
	ws.AllSuggestions = append(ws.AllSuggestions,
		"[validation-gate] finalizer blocked: most recent toolchain validation failed")
	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return err
	}
	return fmt.Errorf("validation gate: most recent toolchain validation failed (%s)", toolchainID)
}

func formatImplementationGateForQA(ws *state.WorkflowState) string {
	if ws == nil || ws.Execution.ImplementationGate == nil {
		return ""
	}
	g := ws.Execution.ImplementationGate
	if !g.Ready {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Implementation verification (engine-confirmed)\n\n")
	b.WriteString("The implementation Pod phase completed the following before this QA pass:\n\n")
	if g.ValidationPassed {
		b.WriteString("- Toolchain validation (build/test/tidy/format): **passed**")
		if g.LatestValidationPhase != "" {
			b.WriteString(fmt.Sprintf(" (phase `%s`)", g.LatestValidationPhase))
		}
		b.WriteString("\n")
	}
	if g.DocumentationOK {
		b.WriteString("- Workspace documentation: **present** (README/plan)\n")
	}
	if sha := strings.TrimSpace(g.CheckpointSHA); sha != "" {
		b.WriteString(fmt.Sprintf("- Latest git checkpoint: `%s`", sha))
		if g.CheckpointPhase != "" {
			b.WriteString(fmt.Sprintf(" (phase `%s`)", g.CheckpointPhase))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nYour QA pass must confirm **design/charter conformance** and delivery readiness. ")
	b.WriteString("Do not re-open raw compile/test failures unless new evidence contradicts the implementation gate.\n")
	return b.String()
}
