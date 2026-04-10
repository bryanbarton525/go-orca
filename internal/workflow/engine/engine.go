// Package engine implements the gorca workflow state machine.
//
// The Engine drives a single workflow run through the canonical persona sequence:
//
//	Director → ProjectManager → Architect → Implementer(s) → QA → Finalizer
//
// When QA reports blocking issues the Architect is re-invoked to produce a
// targeted remediation task set, then the Implementer executes those tasks,
// and QA runs again.  This loop repeats up to MaxQARetries times.  Each
// persona is enforcement-gated: only Implementer may produce Artifacts, only
// Architect may produce Design/Tasks, and Implementer only executes tasks
// whose AssignedTo field is "implementer".  The engine writes all state
// transitions and persona events to the journal.
package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/finalizer/actions"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/director"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
)

// ScopeResolver resolves a scope ID to its ancestor slug chain.
// The engine uses this to pass scope slugs (not UUIDs) to the
// customization registry, which filters sources by slug.
type ScopeResolver interface {
	// ScopeSlugsForID returns the ordered slug chain for a scope (scope itself
	// first, then ancestors up to global).  Returns nil on error — callers
	// treat a nil chain as "all scopes match".
	ScopeSlugsForID(ctx context.Context, scopeID string) []string
}

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

// ImprovementDispatcher applies a single RefinerImprovement and returns the
// outcome.  It is called by the engine after the Finalizer phase completes.
//
// The interface is defined in the engine package so that Options can accept it
// without the engine importing the concrete improvements package (which imports
// neither engine nor scheduler — no circular dependency).
//
// improvements.ConcreteDispatcher satisfies this interface.
type ImprovementDispatcher interface {
	Dispatch(ctx context.Context, parentWS *state.WorkflowState, imp state.RefinerImprovement) (state.ImprovementApplyResult, error)
}

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

	// PersonaMaxRetries is the number of additional attempts made when a
	// persona's LLM call fails with a transient error (e.g. context deadline
	// exceeded, connection refused).  Only retried when the parent context is
	// still alive — a cancelled parent (server shutdown / workflow cancel)
	// aborts immediately.  Defaults to 3.
	PersonaMaxRetries int

	// PersonaRetryBackoff is the base wait duration before the first retry.
	// Each subsequent retry doubles the wait (exponential backoff).
	// Defaults to 10 seconds.
	PersonaRetryBackoff time.Duration

	// CustomizationRegistry, when set, is snapshotted at workflow start to
	// populate skills/agent/prompts context in every HandoffPacket.
	CustomizationRegistry *customization.Registry

	// ScopeResolver, when set alongside CustomizationRegistry, resolves
	// workflow scope IDs to slug chains for correct customization filtering.
	// If nil, scope filtering is skipped (all sources match every workflow).
	ScopeResolver ScopeResolver

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

	// ImprovementDispatcher, when set, is called after the Finalizer phase
	// completes to route each RefinerImprovement to direct-apply or a child
	// improvement workflow.  When nil the engine falls back to speculative
	// applied_path construction (backward-compatible with existing tests).
	ImprovementDispatcher ImprovementDispatcher

	// PersonaPromptRoot is the directory containing the base persona prompt
	// markdown files (e.g. director.md, project_manager.md …).
	// Defaults to prompts.DefaultRoot ("prompts/personas") when empty.
	PersonaPromptRoot string

	// ToolRegistry, when set, provides the set of registered tools whose
	// specs are injected into every persona's system prompt as a
	// ## Available tools section.
	// When nil, no tool context is injected.
	ToolRegistry *tools.Registry
}

func (o *Options) applyDefaults() {
	if o.MaxQARetries <= 0 {
		o.MaxQARetries = 2
	}
	if o.HandoffTimeout <= 0 {
		o.HandoffTimeout = 5 * time.Minute
	}
	if o.PersonaMaxRetries <= 0 {
		o.PersonaMaxRetries = 3
	}
	if o.PersonaRetryBackoff <= 0 {
		o.PersonaRetryBackoff = 10 * time.Second
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

// SetImprovementDispatcher replaces the dispatcher after construction.
// It must be called before any workflows are enqueued; it is not safe for
// concurrent use with Run.
func (e *Engine) SetImprovementDispatcher(d ImprovementDispatcher) {
	e.opts.ImprovementDispatcher = d
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
	// ── Prompt snapshot ───────────────────────────────────────────────────────
	// Load persona prompts once at workflow start and persist the snapshot so
	// that resume, retry, and replay use identical prompt text regardless of
	// subsequent edits to the files on disk.
	if len(ws.PersonaPromptSnapshot) == 0 {
		promptRoot := e.opts.PersonaPromptRoot
		if promptRoot == "" {
			promptRoot = prompts.DefaultRoot
		}
		snapshot, err := prompts.Load(promptRoot)
		if err != nil {
			return fmt.Errorf("engine: load persona prompts: %w", err)
		}
		ws.PersonaPromptSnapshot = snapshot
		if err := e.store.SaveWorkflow(ctx, ws); err != nil {
			return fmt.Errorf("engine: save prompt snapshot: %w", err)
		}
	}

	// Snapshot customizations once at workflow start so live changes don't
	// affect a running workflow.
	//
	// The customization registry filters sources by scope slug, not UUID.
	// Resolve the scope chain to get the slug of the workflow's scope before
	// calling Snapshot.  When the scope ID cannot be resolved (e.g. no
	// ScopeResolver is configured), we pass "" so all sources are included.
	var snap *customization.Snapshot
	if e.opts.CustomizationRegistry != nil {
		scopeSlug := ""
		if e.opts.ScopeResolver != nil && ws.ScopeID != "" {
			slugs := e.opts.ScopeResolver.ScopeSlugsForID(ctx, ws.ScopeID)
			if len(slugs) > 0 {
				scopeSlug = slugs[0] // highest-precedence slug (the scope itself)
			}
		}
		var err error
		snap, err = e.opts.CustomizationRegistry.Snapshot(scopeSlug)
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

	// personaRequired returns true when the given kind should run.  Director
	// is always required.  All others are gated by ws.RequiredPersonas when
	// that list is non-empty (it is populated by the Director after Phase 1).
	personaRequired := func(kind state.PersonaKind) bool {
		if kind == state.PersonaDirector {
			return true
		}
		if len(ws.RequiredPersonas) == 0 {
			return true // Director hasn't run yet or defaulted — include all
		}
		for _, k := range ws.RequiredPersonas {
			if k == kind {
				return true
			}
		}
		return false
	}

	// Phase 1: Director (always mandatory)
	if !phaseComplete(state.PersonaDirector) {
		if err := e.runPersona(ctx, ws, state.PersonaDirector, snap); err != nil {
			return fmt.Errorf("director phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 2: Project Manager
	if personaRequired(state.PersonaProjectMgr) && !phaseComplete(state.PersonaProjectMgr) {
		if err := e.runPersona(ctx, ws, state.PersonaProjectMgr, snap); err != nil {
			return fmt.Errorf("pm phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 3: Architect
	if personaRequired(state.PersonaArchitect) && !phaseComplete(state.PersonaArchitect) {
		if err := e.runPersona(ctx, ws, state.PersonaArchitect, snap); err != nil {
			return fmt.Errorf("architect phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 4: Implementer — runs once per ready/pending task.
	if personaRequired(state.PersonaImplementer) {
		if err := e.runImplementerPhase(ctx, ws, snap); err != nil {
			return fmt.Errorf("implementer phase: %w", err)
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 5: QA — architect-led remediation loop.
	//
	// On each QA pass:
	//   1. QA validates; if no blocking issues, advance to Finalizer.
	//   2. On blockers, Architect is called with the issues to produce
	//      a targeted remediation task set assigned to Implementer only.
	//   3. Implementer runs those new tasks.
	//   4. QA runs again.  Repeats up to MaxQARetries times.
	if personaRequired(state.PersonaQA) {
		if !phaseComplete(state.PersonaQA) || len(ws.BlockingIssues) > 0 {
			for qaCycle := 1; qaCycle <= e.opts.MaxQARetries+1; qaCycle++ {
				// Update visible progress before QA runs.
				ws.Execution.CurrentPersona = state.PersonaQA
				ws.Execution.QACycle = qaCycle
				ws.Execution.ActiveTaskID = ""
				ws.Execution.ActiveTaskTitle = ""
				_ = e.store.SaveWorkflow(ctx, ws)

				if err := e.runPersona(ctx, ws, state.PersonaQA, snap); err != nil {
					return fmt.Errorf("qa phase (cycle %d): %w", qaCycle, err)
				}

				if len(ws.BlockingIssues) == 0 {
					break // QA passed
				}

				if qaCycle > e.opts.MaxQARetries {
					// QA retries exhausted — emit a warning event and continue
					// to the Finalizer rather than failing the workflow.
					exhaustedEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
						events.EventQAExhausted, state.PersonaQA,
						events.QAExhaustedPayload{
							RetriesAllowed: e.opts.MaxQARetries,
							BlockingIssues: ws.BlockingIssues,
						})
					_ = e.store.AppendEvents(ctx, exhaustedEvt)
					// Surface the unresolved issues to the Refiner's retrospective.
					note := fmt.Sprintf("[qa.exhausted] %d blocking issue(s) unresolved after %d remediation cycle(s): %v",
						len(ws.BlockingIssues), e.opts.MaxQARetries, ws.BlockingIssues)
					ws.AllSuggestions = append(ws.AllSuggestions, note)
					break
				}

				// Architect-led remediation: re-plan with the current blocking issues.
				if personaRequired(state.PersonaArchitect) && personaRequired(state.PersonaImplementer) {
					ws.Execution.CurrentPersona = state.PersonaArchitect
					ws.Execution.RemediationAttempt = qaCycle
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.runRemediationPlanning(ctx, ws, snap, qaCycle); err != nil {
						return fmt.Errorf("remediation planning (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkPause(ctx, ws); err != nil {
						return err
					}

					ws.Execution.CurrentPersona = state.PersonaImplementer
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.runImplementerPhase(ctx, ws, snap); err != nil {
						return fmt.Errorf("implementer remediation (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkPause(ctx, ws); err != nil {
						return err
					}
				}

				// Clear blocking issues accumulated in this pass so QA evaluates
				// the remediation fresh.  Retain AllSuggestions for history.
				ws.BlockingIssues = nil
			}
		}
		if err := e.checkPause(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 6: Finalizer (includes inline Refiner retrospective)
	if personaRequired(state.PersonaFinalizer) && !phaseComplete(state.PersonaFinalizer) {
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

	// Pre-announce: update current_persona in persisted state before the LLM
	// call starts so that GET /workflows/:id reflects the in-flight persona
	// immediately, not only after it completes.
	ws.Execution.CurrentPersona = kind
	_ = e.store.SaveWorkflow(ctx, ws)

	// Retry loop: attempt the LLM call up to (1 + PersonaMaxRetries) times.
	// A fresh context.WithTimeout is created for each attempt so that a prior
	// deadline expiry never poisons a subsequent try.  The loop aborts without
	// retry when the parent context is already cancelled (server shutdown,
	// workflow cancel).
	maxAttempts := 1 + e.opts.PersonaMaxRetries
	var out *state.PersonaOutput
	var err error
	var elapsed time.Duration

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Parent context cancelled — no point retrying.
			if ctx.Err() != nil {
				break
			}

			// Exponential backoff: backoff * 2^(attempt-1)
			waitDur := e.opts.PersonaRetryBackoff * time.Duration(uint(1)<<uint(attempt-1))

			retryEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventPersonaRetrying, kind,
				events.PersonaRetryingPayload{
					Persona:      kind,
					Attempt:      attempt,
					MaxAttempts:  maxAttempts,
					Error:        err.Error(),
					RetryAfterMs: waitDur.Milliseconds(),
				})
			_ = e.store.AppendEvents(ctx, retryEvt)

			select {
			case <-ctx.Done():
				// Parent cancelled during wait.
			case <-time.After(waitDur):
			}

			if ctx.Err() != nil {
				break
			}
		}

		personaCtx, cancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
		start := time.Now()
		out, err = p.Execute(personaCtx, packet)
		elapsed = time.Since(start)
		cancel() // release immediately; do not defer inside a loop

		if err == nil {
			break
		}
	}

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

	// ── Finalizer post-processing ─────────────────────────────────────────────
	if kind == state.PersonaFinalizer {
		// 1. Route each improvement through the dispatcher (when configured and
		//    not already inside an improvement workflow — recursion guard).
		if ws.Finalization != nil && len(ws.Finalization.RefinerImprovements) > 0 {
			recursionGuard := ws.Execution.ImprovementDepth >= 1

			if e.opts.ImprovementDispatcher != nil && !recursionGuard {
				// Dispatcher path: dispatch each improvement, collect results,
				// then emit refiner.suggestion events with actual outcomes.
				results := make([]state.ImprovementApplyResult, 0, len(ws.Finalization.RefinerImprovements))
				for _, imp := range ws.Finalization.RefinerImprovements {
					result, _ := e.opts.ImprovementDispatcher.Dispatch(ctx, ws, imp)
					results = append(results, result)

					suggEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
						events.EventRefinerSuggestion, state.PersonaRefiner,
						events.RefinerSuggestionPayload{
							Component:       imp.ComponentType,
							Name:            imp.ComponentName,
							Suggestion:      fmt.Sprintf("[%s] %s → %s", imp.Priority, imp.Problem, imp.ProposedFix),
							AppliedPath:     result.AppliedPath,
							Status:          result.Status,
							ChildWorkflowID: result.ChildWorkflowID,
						})
					_ = e.store.AppendEvents(ctx, suggEvt)
				}
				ws.Finalization.ImprovementResults = results
			} else {
				// Fallback / recursion-guarded path: speculative applied_path
				// construction for backward compatibility and when depth >= 1.
				for _, imp := range ws.Finalization.RefinerImprovements {
					appliedPath := ""
					if e.opts.ImprovementsRoot != "" && imp.Content != "" {
						var relPath string
						switch imp.ComponentType {
						case "skill":
							relPath = filepath.Join("skills", imp.ComponentName, "SKILL.md")
						case "prompt":
							relPath = filepath.Join("prompts", imp.ComponentName+".prompt.md")
						case "agent":
							relPath = filepath.Join("agents", imp.ComponentName+".agent.md")
						default:
							relPath = filepath.Join("personas", imp.ComponentName+".md")
						}
						appliedPath = filepath.Join(e.opts.ImprovementsRoot, relPath)
					}
					suggEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
						events.EventRefinerSuggestion, state.PersonaRefiner,
						events.RefinerSuggestionPayload{
							Component:   imp.ComponentType,
							Name:        imp.ComponentName,
							Suggestion:  fmt.Sprintf("[%s] %s → %s", imp.Priority, imp.Problem, imp.ProposedFix),
							AppliedPath: appliedPath,
						})
					_ = e.store.AppendEvents(ctx, suggEvt)
				}
			}
		}

		// 2. Resolve the delivery action: caller-provided wins over LLM choice.
		actionKey := ""
		if ws.DeliveryAction != "" {
			actionKey = ws.DeliveryAction
		} else if ws.Finalization != nil && ws.Finalization.Action != "" {
			actionKey = ws.Finalization.Action
		}

		if actionKey != "" {
			actionIn := actions.Input{
				Workflow:  ws,
				Artifacts: ws.Artifacts,
				Config:    ws.DeliveryConfig,
			}
			actionOut, actionErr := actions.Global.Execute(ctx, actions.ActionKind(actionKey), actionIn)
			if actionErr != nil {
				return fmt.Errorf("delivery action %q failed: %w", actionKey, actionErr)
			}
			if !actionOut.Success {
				return fmt.Errorf("delivery action %q reported failure: %s", actionKey, actionOut.Error)
			}
			// Merge action links/metadata back into the finalization result.
			if ws.Finalization != nil {
				ws.Finalization.Links = append(ws.Finalization.Links, actionOut.Links...)
				if ws.Finalization.Metadata == nil {
					ws.Finalization.Metadata = make(map[string]string)
				}
				for k, v := range actionOut.Metadata {
					ws.Finalization.Metadata[k] = v
				}
			}
		}
	}

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save after persona %s: %w", kind, err)
	}

	return nil
}

// runImplementerPhase runs the Implementer against every runnable task that is
// assigned to the Implementer persona.  Tasks assigned to any other persona are
// silently skipped so role boundaries are maintained at the engine layer
// regardless of what the LLM's Architect output said.
func (e *Engine) runImplementerPhase(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot) error {
	p, ok := persona.Get(state.PersonaImplementer)
	if !ok {
		return fmt.Errorf("implementer persona not registered")
	}

	for i := range ws.Tasks {
		t := &ws.Tasks[i]

		// Only process Implementer-owned tasks that still need work.
		if t.AssignedTo != state.PersonaImplementer {
			continue
		}
		if t.Status != state.TaskStatusReady &&
			t.Status != state.TaskStatusPending &&
			t.Status != state.TaskStatusFailed {
			continue
		}

		// Update visible progress before calling LLM.
		ws.Execution.CurrentPersona = state.PersonaImplementer
		ws.Execution.ActiveTaskID = t.ID
		ws.Execution.ActiveTaskTitle = t.Title

		// Build a packet scoped to this single task.
		packet := e.buildPacket(ws, state.PersonaImplementer, snap)
		packet.Tasks = []state.Task{*t}

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
			ws.Execution.ActiveTaskID = ""
			ws.Execution.ActiveTaskTitle = ""
			failEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventTaskFailed, state.PersonaImplementer,
				map[string]string{"task_id": t.ID, "error": err.Error()})
			_ = e.store.AppendEvents(ctx, failEvt)
			_ = e.store.SaveWorkflow(ctx, ws)
			return fmt.Errorf("implementer task %s: %w", t.ID[:8], err)
		}

		// Mark task complete and attach artifacts.
		now := time.Now().UTC()
		t.Status = state.TaskStatusCompleted
		t.CompletedAt = &now
		for _, art := range out.Artifacts {
			ws.Artifacts = append(ws.Artifacts, art)
			artEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventArtifactProduced, state.PersonaImplementer,
				map[string]string{"task_id": t.ID, "artifact_name": art.Name, "kind": string(art.Kind)})
			_ = e.store.AppendEvents(ctx, artEvt)
		}

		if ws.Summaries == nil {
			ws.Summaries = make(map[state.PersonaKind]string)
		}
		// Append per-task summary rather than overwrite.
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		ws.Summaries[state.PersonaImplementer] += fmt.Sprintf("[%s] %s\n", shortID, out.Summary)

		doneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskCompleted, state.PersonaImplementer,
			map[string]string{"task_id": t.ID, "summary": out.Summary})
		_ = e.store.AppendEvents(ctx, doneEvt)
	}

	ws.Execution.ActiveTaskID = ""
	ws.Execution.ActiveTaskTitle = ""
	return e.store.SaveWorkflow(ctx, ws)
}

// runRemediationPlanning invokes the Architect with the current blocking issues
// so it can produce a targeted set of implementer-only remediation tasks.
// The new tasks are appended to the task graph (existing completed tasks are
// preserved for audit) with Attempt and RemediationSource set.
func (e *Engine) runRemediationPlanning(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot, qaCycle int) error {
	p, ok := persona.Get(state.PersonaArchitect)
	if !ok {
		return fmt.Errorf("architect persona not registered")
	}

	packet := e.buildPacket(ws, state.PersonaArchitect, snap)
	packet.IsRemediation = true

	archCtx, archCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(archCtx, packet)
	archCancel()
	if err != nil {
		return fmt.Errorf("architect remediation: %w", err)
	}

	// Validate: Architect must only produce Implementer-assigned tasks during
	// remediation.  Any task assigned to another persona is dropped with a warning.
	validTasks := make([]state.Task, 0, len(out.Tasks))
	for _, t := range out.Tasks {
		if t.AssignedTo != state.PersonaImplementer && t.AssignedTo != "" {
			// Emit a suggestion so the issue is visible without aborting the cycle.
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[warning][remediation] Architect emitted task %q assigned to %q; only 'implementer' is valid during remediation — task dropped", t.Title, t.AssignedTo))
			continue
		}
		if t.AssignedTo == "" {
			t.AssignedTo = state.PersonaImplementer
		}
		t.Attempt = qaCycle
		t.RemediationSource = "qa_remediation"
		validTasks = append(validTasks, t)
	}

	if len(validTasks) == 0 {
		return fmt.Errorf("architect produced no valid implementer tasks for remediation cycle %d (blocking issues: %v)", qaCycle, ws.BlockingIssues)
	}

	// Append new tasks; existing tasks (including completed ones) are retained.
	for _, t := range validTasks {
		ws.Tasks = append(ws.Tasks, t)
		createdEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskCreated, state.PersonaArchitect,
			map[string]string{"task_id": t.ID, "title": t.Title, "attempt": fmt.Sprint(qaCycle)})
		_ = e.store.AppendEvents(ctx, createdEvt)
	}

	// Update architect summary (append so prior planning is retained).
	if ws.Summaries == nil {
		ws.Summaries = make(map[state.PersonaKind]string)
	}
	ws.Summaries[state.PersonaArchitect] += fmt.Sprintf("[remediation cycle %d] %s\n", qaCycle, out.Summary)

	return e.store.SaveWorkflow(ctx, ws)
}

// ─── State helpers ────────────────────────────────────────────────────────────

// applyOutput merges a PersonaOutput into the WorkflowState.
// Role contracts are enforced here:
//   - Only Implementer may append Artifacts (other personas use task-level path).
//   - Only Architect may set Design or replace Tasks.
//   - Only QA may set BlockingIssues (Finalizer/Refiner may suggest).
//   - Violations are recorded as AllSuggestions warnings, not hard failures,
//     so a misbehaving LLM does not crash the workflow.
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
	// Only Architect may write Design or the task graph; record a warning for
	// other personas and drop the output to prevent role drift.
	if out.Design != nil {
		if out.Persona == state.PersonaArchitect {
			ws.Design = out.Design
		} else {
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[warning][role-enforcement] persona %q attempted to write Design — ignored", out.Persona))
		}
	}
	if len(out.Tasks) > 0 {
		if out.Persona == state.PersonaArchitect {
			ws.Tasks = out.Tasks
		} else {
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[warning][role-enforcement] persona %q attempted to write Tasks — ignored", out.Persona))
		}
	}
	// Only Implementer may append Artifacts via applyOutput; other personas
	// that legitimately produce artifacts (e.g. Refiner) use direct ws.Artifacts
	// writes in their own runner paths.  QA must not create artifacts.
	if len(out.Artifacts) > 0 {
		if out.Persona == state.PersonaImplementer {
			ws.Artifacts = append(ws.Artifacts, out.Artifacts...)
		} else {
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[warning][role-enforcement] persona %q attempted to write %d Artifact(s) — ignored", out.Persona, len(out.Artifacts)))
		}
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

	// Update live execution progress after every persona phase.
	ws.Execution.CurrentPersona = out.Persona

	// Director sets provider/model.
	if out.Persona == state.PersonaDirector {
		explicitMode := ws.Mode != ""
		explicitProvider := ws.ProviderName != ""
		explicitModel := ws.ModelName != ""

		// Parse the raw JSON output from the Director to extract its decisions.
		dirOut := director.OutputFromRaw(out.RawContent, state.HandoffPacket{
			ProviderName: ws.ProviderName,
			ModelName:    ws.ModelName,
			Mode:         ws.Mode,
			Request:      ws.Request,
		})
		if !explicitProvider && dirOut.Provider != "" {
			// Normalize to lowercase and validate the provider is registered.
			// If the LLM chose an unregistered provider, fall back to the
			// engine default and reset the model too so they stay consistent.
			normalized := strings.ToLower(dirOut.Provider)
			if _, ok := common.Get(normalized); ok {
				ws.ProviderName = normalized
			} else {
				ws.ProviderName = e.opts.DefaultProvider
				if !explicitModel {
					ws.ModelName = e.opts.DefaultModel
				}
			}
		}
		if !explicitModel && dirOut.Model != "" {
			ws.ModelName = dirOut.Model
		}
		if !explicitMode && dirOut.Mode != "" {
			ws.Mode = dirOut.Mode
		}
		if dirOut.Title != "" {
			ws.Title = dirOut.Title
		}
		// Store the Director's pipeline plan so subsequent phases can be
		// filtered (required_personas) and the Finalizer can be forced to the
		// correct delivery action (finalizer_action).
		if len(dirOut.RequiredPersonas) > 0 {
			ws.RequiredPersonas = dirOut.RequiredPersonas
		}
		if dirOut.FinalizerAction != "" {
			ws.FinalizerAction = dirOut.FinalizerAction
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
		WorkflowID:            ws.ID,
		TenantID:              ws.TenantID,
		ScopeID:               ws.ScopeID,
		Mode:                  ws.Mode,
		Request:               ws.Request,
		Constitution:          ws.Constitution,
		Requirements:          ws.Requirements,
		Design:                ws.Design,
		Tasks:                 ws.Tasks,
		Artifacts:             ws.Artifacts,
		Summaries:             ws.Summaries,
		CurrentPersona:        kind,
		ProviderName:          provider,
		ModelName:             model,
		BlockingIssues:        ws.BlockingIssues,
		AllSuggestions:        ws.AllSuggestions,
		ImprovementsPath:      e.opts.ImprovementsRoot,
		PersonaPromptSnapshot: ws.PersonaPromptSnapshot,
		FinalizerAction:       ws.FinalizerAction,
		DeliveryAction:        ws.DeliveryAction,
		DeliveryConfig:        ws.DeliveryConfig,
		QACycle:               ws.Execution.QACycle,
		RemediationAttempt:    ws.Execution.RemediationAttempt,
	}

	// Populate customization context from the workflow-start snapshot.
	if snap != nil {
		packet.SkillsContext = snap.SkillsContext()
		packet.CustomAgentMD = snap.AgentsContext()
		packet.PromptsContext = snap.PromptsContext()
	}

	// Populate tool context from the registry.
	if e.opts.ToolRegistry != nil {
		packet.ToolsContext = formatToolSpecs(e.opts.ToolRegistry.Specs())
		packet.ToolRegistry = e.opts.ToolRegistry
	}

	return packet
}

// formatToolSpecs renders a list of tool specs as a markdown section for
// injection into persona system prompts.
func formatToolSpecs(specs []tools.ToolSpec) string {
	if len(specs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, s := range specs {
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n**Parameters:**\n```json\n%s\n```\n\n", s.Name, s.Description, s.Parameters))
	}
	return strings.TrimRight(sb.String(), "\n")
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
