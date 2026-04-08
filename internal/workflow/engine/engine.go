// Package engine implements the gorca workflow state machine.
//
// The Engine drives a single workflow run through the canonical persona sequence:
//
//	Director → ProjectManager → Architect → Implementer(s) → QA → Finalizer
//
// QA failures with blocking issues will re-invoke the Implementer (up to a
// configured retry limit) before proceeding to Finalizer. The engine writes
// all state transitions and persona events to the journal.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/director"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

// Store is the persistence interface required by the Engine.
// A concrete implementation (Postgres or SQLite) satisfies this.
type Store interface {
	// GetWorkflow retrieves the workflow state by ID.
	GetWorkflow(ctx context.Context, id string) (*state.WorkflowState, error)
	// SaveWorkflow persists the full workflow state (upsert).
	SaveWorkflow(ctx context.Context, ws *state.WorkflowState) error
	// AppendEvents atomically appends events to the journal.
	AppendEvents(ctx context.Context, evts ...*events.Event) error
}

// ErrPaused is returned by Run when a PauseFunc triggers a workflow pause.
// The workflow status is set to WorkflowStatusPaused before returning this
// error; callers (e.g. the scheduler) should NOT treat this as a failure.
var ErrPaused = fmt.Errorf("engine: workflow paused")

// Options configures the Engine.
type Options struct {
	// MaxQARetries is the maximum number of times the Implementer will be
	// re-run after QA returns blocking issues.  Defaults to 2.
	MaxQARetries int

	// DefaultProvider is used when the Director does not select one.
	DefaultProvider string
	// DefaultModel is used when the Director does not select one.
	DefaultModel string

	// HandoffTimeout is the per-persona execution timeout.
	// Defaults to 5 minutes.
	HandoffTimeout time.Duration

	// CustomizationRegistry, when set, is snapshotted at workflow start to
	// populate skills/agent/prompts context in every HandoffPacket.
	CustomizationRegistry *customization.Registry

	// PauseFunc, when non-nil, is called between persona phases.  If it
	// returns true the engine transitions the workflow to WorkflowStatusPaused
	// and returns ErrPaused.  The workflow can be re-enqueued via the
	// POST /workflows/:id/resume endpoint.
	PauseFunc func() bool

	// ImprovementsRoot is the directory where the Refiner writes improvement
	// files (SKILL.md, .prompt.md, .agent.md) after each completed workflow.
	// When empty, improvements are stored as suggestion strings only.
	// Defaults to empty (disabled).
	ImprovementsRoot string
}

func (o *Options) applyDefaults() {
	if o.MaxQARetries <= 0 {
		o.MaxQARetries = 2
	}
	if o.HandoffTimeout <= 0 {
		o.HandoffTimeout = 5 * time.Minute
	}
}

// Engine drives a single workflow run.
type Engine struct {
	store Store
	opts  Options
}

// New creates a new Engine.
func New(store Store, opts Options) *Engine {
	opts.applyDefaults()
	return &Engine{store: store, opts: opts}
}

// Run executes the workflow identified by workflowID to completion (or failure).
// It is safe to call Run in a goroutine; the caller is responsible for
// lifecycle management (see scheduler package).
func (e *Engine) Run(ctx context.Context, workflowID string) error {
	ws, err := e.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("engine: load workflow %s: %w", workflowID, err)
	}

	if ws.Status == state.WorkflowStatusCompleted ||
		ws.Status == state.WorkflowStatusCancelled {
		return fmt.Errorf("engine: workflow %s is already terminal (%s)", workflowID, ws.Status)
	}

	if err := e.transition(ctx, ws, state.WorkflowStatusRunning); err != nil {
		return err
	}

	runErr := e.runPhases(ctx, ws)
	if runErr != nil {
		if runErr == ErrPaused {
			// Paused is not a failure; transition was already applied inside runPhases.
			return ErrPaused
		}
		ws.ErrorMessage = runErr.Error()
		_ = e.transition(ctx, ws, state.WorkflowStatusFailed)
		return runErr
	}

	return e.transition(ctx, ws, state.WorkflowStatusCompleted)
}

// ─── Internal phase execution ────────────────────────────────────────────────

// checkPause transitions the workflow to paused if the PauseFunc fires.
// Returns ErrPaused if the caller should stop execution.
func (e *Engine) checkPause(ctx context.Context, ws *state.WorkflowState) error {
	if e.opts.PauseFunc == nil || !e.opts.PauseFunc() {
		return nil
	}
	if err := e.transition(ctx, ws, state.WorkflowStatusPaused); err != nil {
		return fmt.Errorf("pause transition: %w", err)
	}
	return ErrPaused
}

func (e *Engine) runPhases(ctx context.Context, ws *state.WorkflowState) error {
	// Snapshot customizations once at workflow start so live changes don't
	// affect a running workflow.
	var snap *customization.Snapshot
	if e.opts.CustomizationRegistry != nil {
		var err error
		snap, err = e.opts.CustomizationRegistry.Snapshot(ws.ScopeID)
		if err != nil {
			// Non-fatal: log and continue without customizations.
			snap = nil
		}
	}

	// phaseComplete returns true when a persona phase already ran in a prior
	// attempt (indicated by a non-empty summary entry).  This lets a resumed or
	// retried workflow skip phases that succeeded, rather than replaying the
	// entire pipeline from scratch.
	phaseComplete := func(kind state.PersonaKind) bool {
		return ws.Summaries != nil && ws.Summaries[kind] != ""
	}

	// Phase 1: Director
	if !phaseComplete(state.PersonaDirector) {
		if err := e.runPersona(ctx, ws, state.PersonaDirector, snap); err != nil {
			return fmt.Errorf("director phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 2: Project Manager
	if !phaseComplete(state.PersonaProjectMgr) {
		if err := e.runPersona(ctx, ws, state.PersonaProjectMgr, snap); err != nil {
			return fmt.Errorf("pm phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 3: Architect
	if !phaseComplete(state.PersonaArchitect) {
		if err := e.runPersona(ctx, ws, state.PersonaArchitect, snap); err != nil {
			return fmt.Errorf("architect phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 4: Implementer — runs once per ready/pending task.
	// Any tasks already marked completed survive the skip — only unfinished
	// tasks are sent to the LLM.
	if err := e.runImplementerPhase(ctx, ws, snap); err != nil {
		return fmt.Errorf("implementer phase: %w", err)
	}
	if err := e.checkPause(ctx, ws); err != nil {
		return err
	}

	// Phase 5: QA — retry loop (skip entirely if QA already passed in a prior run)
	if !phaseComplete(state.PersonaQA) || len(ws.BlockingIssues) > 0 {
		for attempt := 0; attempt <= e.opts.MaxQARetries; attempt++ {
			if err := e.runPersona(ctx, ws, state.PersonaQA, snap); err != nil {
				return fmt.Errorf("qa phase (attempt %d): %w", attempt+1, err)
			}

			if len(ws.BlockingIssues) == 0 {
				break // QA passed
			}

			if attempt == e.opts.MaxQARetries {
				return fmt.Errorf("qa: %d blocking issues remain after %d retries: %v",
					len(ws.BlockingIssues), e.opts.MaxQARetries, ws.BlockingIssues)
			}

			// Re-run Implementer to address blocking issues.
			if err := e.runImplementerPhase(ctx, ws, snap); err != nil {
				return fmt.Errorf("implementer re-run (attempt %d): %w", attempt+1, err)
			}
			if err := e.checkPause(ctx, ws); err != nil {
				return err
			}
			ws.BlockingIssues = nil // reset before next QA pass
		}
	}
	if err := e.checkPause(ctx, ws); err != nil {
		return err
	}

	// Phase 6: Finalizer (includes inline Refiner retrospective)
	if !phaseComplete(state.PersonaFinalizer) {
		if err := e.runPersona(ctx, ws, state.PersonaFinalizer, snap); err != nil {
			return fmt.Errorf("finalizer phase: %w", err)
		}
	}

	return nil
}

// runPersona dispatches a single persona phase against the current workflow state.
func (e *Engine) runPersona(ctx context.Context, ws *state.WorkflowState, kind state.PersonaKind, snap *customization.Snapshot) error {
	p, ok := persona.Get(kind)
	if !ok {
		return fmt.Errorf("persona %q not registered", kind)
	}

	packet := e.buildPacket(ws, kind, snap)

	startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventPersonaStarted, kind,
		events.PersonaStartedPayload{
			Persona:      kind,
			ProviderName: packet.ProviderName,
			ModelName:    packet.ModelName,
		})
	_ = e.store.AppendEvents(ctx, startEvt)

	// Apply per-persona handoff timeout.
	personaCtx, cancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	defer cancel()

	start := time.Now()
	out, err := p.Execute(personaCtx, packet)
	elapsed := time.Since(start)

	if err != nil {
		failEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaFailed, kind,
			events.PersonaFailedPayload{Persona: kind, Error: err.Error()})
		_ = e.store.AppendEvents(ctx, failEvt)
		return err
	}

	// Merge output back into workflow state.
	e.applyOutput(ws, out)

	doneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventPersonaCompleted, kind,
		events.PersonaCompletedPayload{
			Persona:        kind,
			DurationMs:     elapsed.Milliseconds(),
			Summary:        out.Summary,
			BlockingIssues: out.BlockingIssues,
		})
	_ = e.store.AppendEvents(ctx, doneEvt)

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save after persona %s: %w", kind, err)
	}

	return nil
}

// runImplementerPhase runs the Implementer against every ready task.
func (e *Engine) runImplementerPhase(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot) error {
	for i := range ws.Tasks {
		t := &ws.Tasks[i]
		if t.Status != state.TaskStatusReady &&
			t.Status != state.TaskStatusPending &&
			t.Status != state.TaskStatusFailed {
			continue
		}

		// Build a packet scoped to this single task.
		packet := e.buildPacket(ws, state.PersonaImplementer, snap)
		packet.Tasks = []state.Task{*t}

		p, ok := persona.Get(state.PersonaImplementer)
		if !ok {
			return fmt.Errorf("implementer persona not registered")
		}

		// Mark task running before LLM call so callers see the transition.
		t.Status = state.TaskStatusRunning
		if err := e.store.SaveWorkflow(ctx, ws); err != nil {
			return fmt.Errorf("save before implementer task %s: %w", t.ID[:8], err)
		}

		startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskStarted, state.PersonaImplementer,
			map[string]string{"task_id": t.ID, "title": t.Title})
		_ = e.store.AppendEvents(ctx, startEvt)

		taskCtx, taskCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
		out, err := p.Execute(taskCtx, packet)
		taskCancel()
		if err != nil {
			t.Status = state.TaskStatusFailed
			_ = e.store.SaveWorkflow(ctx, ws)
			return fmt.Errorf("implementer task %s: %w", t.ID[:8], err)
		}

		// Mark task complete and attach artifacts.
		now := time.Now().UTC()
		t.Status = state.TaskStatusCompleted
		t.CompletedAt = &now
		ws.Artifacts = append(ws.Artifacts, out.Artifacts...)

		if ws.Summaries == nil {
			ws.Summaries = make(map[state.PersonaKind]string)
		}
		// Append per-task summary rather than overwrite.
		ws.Summaries[state.PersonaImplementer] += fmt.Sprintf("[%s] %s\n", t.ID[:8], out.Summary)

		doneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskCompleted, state.PersonaImplementer,
			map[string]string{"task_id": t.ID, "summary": out.Summary})
		_ = e.store.AppendEvents(ctx, doneEvt)
	}

	return e.store.SaveWorkflow(ctx, ws)
}

// ─── State helpers ────────────────────────────────────────────────────────────

// applyOutput merges a PersonaOutput into the WorkflowState.
func (e *Engine) applyOutput(ws *state.WorkflowState, out *state.PersonaOutput) {
	if ws.Summaries == nil {
		ws.Summaries = make(map[state.PersonaKind]string)
	}
	// Use a sentinel when the LLM returns an empty summary so phaseComplete()
	// correctly identifies this persona as having already run.
	summary := out.Summary
	if summary == "" {
		summary = "(completed)"
	}
	ws.Summaries[out.Persona] = summary

	if out.Constitution != nil {
		ws.Constitution = out.Constitution
	}
	if out.Requirements != nil {
		ws.Requirements = out.Requirements
	}
	if out.Design != nil {
		ws.Design = out.Design
	}
	if len(out.Tasks) > 0 {
		ws.Tasks = out.Tasks
	}
	if len(out.Artifacts) > 0 {
		ws.Artifacts = append(ws.Artifacts, out.Artifacts...)
	}
	if out.Finalization != nil {
		ws.Finalization = out.Finalization
	}
	if len(out.BlockingIssues) > 0 {
		ws.BlockingIssues = append(ws.BlockingIssues, out.BlockingIssues...)
	}
	if len(out.Suggestions) > 0 {
		ws.AllSuggestions = append(ws.AllSuggestions, out.Suggestions...)
	}

	// Director sets provider/model.
	if out.Persona == state.PersonaDirector {
		// Parse the raw JSON output from the Director to extract its decisions.
		dirOut := director.OutputFromRaw(out.RawContent, state.HandoffPacket{
			ProviderName: ws.ProviderName,
			ModelName:    ws.ModelName,
		})
		if dirOut.Provider != "" {
			// Normalize to lowercase and validate the provider is registered.
			// If the LLM chose an unregistered provider, fall back to the
			// engine default and reset the model too so they stay consistent.
			normalized := strings.ToLower(dirOut.Provider)
			if _, ok := common.Get(normalized); ok {
				ws.ProviderName = normalized
				if dirOut.Model != "" {
					ws.ModelName = dirOut.Model
				}
			} else {
				ws.ProviderName = e.opts.DefaultProvider
				ws.ModelName = e.opts.DefaultModel
			}
		}
		if dirOut.Mode != "" {
			ws.Mode = dirOut.Mode
		}
		if dirOut.Title != "" {
			ws.Title = dirOut.Title
		}
	}

	ws.UpdatedAt = time.Now().UTC()
}

// buildPacket constructs the HandoffPacket for the given persona from the
// current WorkflowState snapshot.
func (e *Engine) buildPacket(ws *state.WorkflowState, kind state.PersonaKind, snap *customization.Snapshot) state.HandoffPacket {
	provider := ws.ProviderName
	if provider == "" {
		provider = e.opts.DefaultProvider
	}
	model := ws.ModelName
	if model == "" {
		model = e.opts.DefaultModel
	}

	packet := state.HandoffPacket{
		WorkflowID:       ws.ID,
		TenantID:         ws.TenantID,
		ScopeID:          ws.ScopeID,
		Mode:             ws.Mode,
		Request:          ws.Request,
		Constitution:     ws.Constitution,
		Requirements:     ws.Requirements,
		Design:           ws.Design,
		Tasks:            ws.Tasks,
		Artifacts:        ws.Artifacts,
		Summaries:        ws.Summaries,
		CurrentPersona:   kind,
		ProviderName:     provider,
		ModelName:        model,
		BlockingIssues:   ws.BlockingIssues,
		AllSuggestions:   ws.AllSuggestions,
		ImprovementsPath: e.opts.ImprovementsRoot,
	}

	// Populate customization context from the workflow-start snapshot.
	if snap != nil {
		packet.SkillsContext = snap.SkillsContext()
		packet.CustomAgentMD = snap.AgentsContext()
		packet.PromptsContext = snap.PromptsContext()
	}

	return packet
}

// transition updates workflow status, appends a transition event, and saves.
func (e *Engine) transition(ctx context.Context, ws *state.WorkflowState, to state.WorkflowStatus) error {
	from := ws.Status
	ws.Status = to
	ws.UpdatedAt = time.Now().UTC()

	switch to {
	case state.WorkflowStatusRunning:
		now := time.Now().UTC()
		ws.StartedAt = &now
	case state.WorkflowStatusCompleted, state.WorkflowStatusFailed, state.WorkflowStatusCancelled:
		now := time.Now().UTC()
		ws.CompletedAt = &now
	}

	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventStateTransition, "",
		events.StateTransitionPayload{From: from, To: to})
	if err := e.store.AppendEvents(ctx, evt); err != nil {
		return fmt.Errorf("transition event: %w", err)
	}

	return e.store.SaveWorkflow(ctx, ws)
}
