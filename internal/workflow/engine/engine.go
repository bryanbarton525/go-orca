// Package engine implements the gorca workflow state machine.
//
// The Engine drives a single workflow run through the canonical persona sequence:
//
//	Director → ProjectManager → Architect → Pod(s) → QA → Finalizer
//
// When QA reports blocking issues the Architect is re-invoked to produce a
// targeted remediation task set, then the Pod executes those tasks,
// and QA runs again.  This loop repeats up to MaxQARetries times.  Each
// persona is enforcement-gated: only Pod may produce Artifacts, only
// Architect may produce Design/Tasks, and Pod only executes tasks
// whose AssignedTo field is "pod".  The engine writes all state
// transitions and persona events to the journal.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/finalizer/actions"
	"github.com/go-orca/go-orca/internal/logger"
	"github.com/go-orca/go-orca/internal/mcp/capabilities"
	mcpregistry "github.com/go-orca/go-orca/internal/mcp/registry"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/director"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
	"go.uber.org/zap"
)

// ScopeResolver resolves a scope ID to its ancestor slug chain.
type ScopeResolver interface {
	ScopeSlugsForID(ctx context.Context, scopeID string) []string
}

// AttachmentStore is the persistence interface for attachment ingestion.
type AttachmentStore interface {
	ListAttachmentsByWorkflow(ctx context.Context, workflowID string) ([]*state.Attachment, error)
	ListAttachmentsBySession(ctx context.Context, sessionID string) ([]*state.Attachment, error)
	GetAttachment(ctx context.Context, id string) (*state.Attachment, error)
	UpdateAttachmentStatus(ctx context.Context, id string, status state.AttachmentStatus, summary string, chunkCount int, errMsg string) error
	CreateAttachmentChunks(ctx context.Context, chunks []state.AttachmentChunk) error
	ListAttachmentChunks(ctx context.Context, attachmentID string) ([]state.AttachmentChunk, error)
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

// ErrCancelled is returned by Run when the persisted workflow status is flipped
// to cancelled while the engine is mid-flight. Callers should treat it as a
// terminal stop, not a failure that should be retried.
var ErrCancelled = fmt.Errorf("engine: workflow cancelled")

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
	// MaxQARetries is the maximum number of times the Pod will be
	// re-run after QA returns blocking issues.  Defaults to 2.
	MaxQARetries int

	// DefaultProvider is used when the Director does not select one.
	DefaultProvider string
	// DefaultModel is used when the Director does not select one.
	DefaultModel string
	// ProviderDefaults stores the configured default model per provider so the
	// engine can resolve safe fallbacks after live model discovery.
	ProviderDefaults map[string]string
	// ExcludedModels stores the deny-list per provider. Keys are provider names;
	// nested keys are canonicalized model IDs.
	ExcludedModels map[string]map[string]struct{}
	// ModelDiscoveryTimeout bounds a single provider Models() call when the
	// engine snapshots the available model catalog at workflow start.
	ModelDiscoveryTimeout time.Duration

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
	// POST /api/v1/workflows/{id}/resume endpoint.
	PauseFunc func() bool

	// ImprovementsRoot is the directory where the Refiner writes improvement
	// files (SKILL.md, .prompt.md, .agent.md) after each completed workflow.
	// When empty, improvements are stored as suggestion strings only.
	// Defaults to empty (disabled).
	ImprovementsRoot string
	// WorkspaceRoot is the base directory for engine-owned workflow workspaces.
	// Toolchain MCP servers should mount the same path when using shared-volume
	// deployment. Defaults to empty (workspace setup disabled).
	WorkspaceRoot string

	// Toolchains describes governed MCP-backed language/build toolchains. When
	// configured, software workflows are validated through these capabilities
	// after each implementation phase before QA/finalization.
	Toolchains []ToolchainConfig

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

	// MCPRegistry, when set, becomes the sole resolution path for toolchain
	// capability invocations.  When nil, the engine falls back to direct
	// dispatch via ToolRegistry by tool name (legacy path).
	MCPRegistry *mcpregistry.Registry

	// EnforceValidationGate, when true, prevents software/mixed/ops
	// workflows from running the Finalizer phase when the most recent
	// toolchain validation run failed.  An EventValidationGateBlocked is
	// emitted and the workflow is marked failed instead.  Default false to
	// preserve existing behaviour; operators opt in via
	// workflow.enforce_validation_gate in go-orca.yaml.
	EnforceValidationGate bool

	// AttachmentStore provides access to upload sessions, attachments, and
	// chunks for the pre-Director ingestion stage.  When nil, ingestion is
	// skipped even if the workflow has an upload_session_id.
	AttachmentStore AttachmentStore

	// IngestionProvider is the provider name used for attachment summarisation.
	// Falls back to DefaultProvider when empty.
	IngestionProvider string
	// IngestionModel is the model used for attachment summarisation.
	// Falls back to DefaultModel when empty.
	IngestionModel string
	// IngestionMaxWorkers limits parallel attachment processing goroutines.
	// Defaults to 4.
	IngestionMaxWorkers int
	// IngestionTimeout is the per-attachment summarisation timeout.
	// Defaults to 5 minutes.
	IngestionTimeout time.Duration
	// IngestionChunkSize is the max character count per attachment chunk.
	// Defaults to 4000.
	IngestionChunkSize int
}

// ToolchainConfig maps a language stack to MCP tools that implement build/test
// capabilities. Tools must already be present in Options.ToolRegistry.
type ToolchainConfig struct {
	ID                   string
	Languages            []string
	MCPServer            string
	Capabilities         []string
	CapabilityTools      map[string]string
	ValidationProfiles   map[string][]string
	CheckpointCapability string
	PushCheckpoints      bool
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
	if o.ModelDiscoveryTimeout <= 0 {
		o.ModelDiscoveryTimeout = defaultModelDiscoveryTimeout
	}
	if o.IngestionMaxWorkers <= 0 {
		o.IngestionMaxWorkers = 4
	}
	if o.IngestionTimeout <= 0 {
		o.IngestionTimeout = 5 * time.Minute
	}
	if o.IngestionChunkSize <= 0 {
		o.IngestionChunkSize = 4000
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
		if errors.Is(runErr, ErrPaused) {
			// Pause and cancellation are terminal control signals, not failures.
			return ErrPaused
		}
		if errors.Is(runErr, ErrCancelled) {
			// Pause and cancellation are terminal control signals, not failures.
			return ErrCancelled
		}
		ws.ErrorMessage = runErr.Error()
		_ = e.transition(ctx, ws, state.WorkflowStatusFailed)
		return runErr
	}

	if err := e.checkControlState(ctx, ws); err != nil {
		if errors.Is(err, ErrPaused) {
			return ErrPaused
		}
		if errors.Is(err, ErrCancelled) {
			return ErrCancelled
		}
		return err
	}

	return e.transition(ctx, ws, state.WorkflowStatusCompleted)
}

// ─── Internal phase execution ────────────────────────────────────────────────

// checkControlState stops execution when an external control signal changes the
// persisted workflow state. Cancellation is detected from the store so the API
// can stop an in-flight run even though the scheduler's parent context remains
// alive.
func (e *Engine) checkControlState(ctx context.Context, ws *state.WorkflowState) error {
	latest, err := e.store.GetWorkflow(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("load workflow control state: %w", err)
	}

	if latest.Status == state.WorkflowStatusCancelled {
		if activeTaskID := ws.Execution.ActiveTaskID; activeTaskID != "" {
			for i := range ws.Tasks {
				if ws.Tasks[i].ID == activeTaskID && ws.Tasks[i].Status == state.TaskStatusRunning {
					ws.Tasks[i].Status = state.TaskStatusPending
					break
				}
			}
		}

		ws.Execution.CurrentPersona = ""
		ws.Execution.ActiveTaskID = ""
		ws.Execution.ActiveTaskTitle = ""
		ws.ErrorMessage = latest.ErrorMessage

		if ws.Status != state.WorkflowStatusCancelled {
			if err := e.transition(ctx, ws, state.WorkflowStatusCancelled); err != nil {
				return fmt.Errorf("cancel transition: %w", err)
			}
		}

		return ErrCancelled
	}

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
	if err := e.ensureProviderCatalogs(ctx, ws); err != nil {
		return fmt.Errorf("engine: snapshot provider catalogs: %w", err)
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

	// Phase 0: Pre-Director attachment ingestion (engine-owned, not a persona).
	if ws.UploadSessionID != "" && e.opts.AttachmentStore != nil {
		if err := e.ensureAttachmentProcessing(ctx, ws); err != nil {
			return fmt.Errorf("attachment ingestion: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 1: Director (always mandatory)
	if !phaseComplete(state.PersonaDirector) {
		if err := e.runPersona(ctx, ws, state.PersonaDirector, snap); err != nil {
			return fmt.Errorf("director phase: %w", err)
		}
		if err := e.ensureWorkspaceAndToolchain(ctx, ws); err != nil {
			return fmt.Errorf("workspace setup: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}
	if err := e.ensureWorkspaceAndToolchain(ctx, ws); err != nil {
		return fmt.Errorf("workspace setup: %w", err)
	}

	// Phase 2: Project Manager
	if personaRequired(state.PersonaProjectMgr) && !phaseComplete(state.PersonaProjectMgr) {
		if err := e.runPersona(ctx, ws, state.PersonaProjectMgr, snap); err != nil {
			return fmt.Errorf("pm phase: %w", err)
		}
		if err := e.materializeConstitution(ctx, ws); err != nil {
			return fmt.Errorf("materialize constitution: %w", err)
		}
		if err := e.runToolchainCheckpoint(ctx, ws, "constitution"); err != nil {
			return fmt.Errorf("constitution checkpoint: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 2.5: Matriarch — captures pragmatic engineering defaults for
	// architectural decisions without requiring a human interruption.
	if personaRequired(state.PersonaMatriarch) && !phaseComplete(state.PersonaMatriarch) {
		if err := e.runPersona(ctx, ws, state.PersonaMatriarch, snap); err != nil {
			return fmt.Errorf("matriarch phase: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 3: Architect
	if personaRequired(state.PersonaArchitect) && !phaseComplete(state.PersonaArchitect) {
		if err := e.runPersona(ctx, ws, state.PersonaArchitect, snap); err != nil {
			return fmt.Errorf("architect phase: %w", err)
		}
		if err := e.materializePlan(ctx, ws); err != nil {
			return fmt.Errorf("materialize plan: %w", err)
		}
		if err := e.runToolchainCheckpoint(ctx, ws, "plan"); err != nil {
			return fmt.Errorf("plan checkpoint: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 4: Pod — runs once per ready/pending task.
	if personaRequired(state.PersonaPod) {
		if err := e.ensureToolchainBootstrap(ctx, ws, "implementation-bootstrap"); err != nil {
			return fmt.Errorf("implementation bootstrap: %w", err)
		}
		if err := e.runToolchainCheckpoint(ctx, ws, "implementation-bootstrap"); err != nil {
			return fmt.Errorf("implementation bootstrap checkpoint: %w", err)
		}
		if err := e.runPodPhase(ctx, ws, snap); err != nil {
			return fmt.Errorf("pod phase: %w", err)
		}
		validationIssues, err := e.runToolchainValidation(ctx, ws, "implementation")
		if err != nil {
			return fmt.Errorf("implementation validation: %w", err)
		}
		ws.BlockingIssues = append(ws.BlockingIssues, validationIssues...)
		if err := e.runToolchainCheckpoint(ctx, ws, "implementation"); err != nil {
			return fmt.Errorf("implementation checkpoint: %w", err)
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 5: QA — architect-led remediation loop.
	//
	// On each QA pass:
	//   1. QA validates; if no blocking issues, advance to Finalizer.
	//   2. On blockers, Architect is called with the issues to produce
	//      a targeted remediation task set assigned to Pod only.
	//   3. Pod runs those new tasks.
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
					// QA or validation remediation retries exhausted — emit the
					// exhaustion event and fail before Finalizer. This keeps
					// validation failures inside the remediation loop instead of
					// surfacing as a late finalizer gate failure.
					exhaustedEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
						events.EventQAExhausted, state.PersonaQA,
						events.QAExhaustedPayload{
							RetriesAllowed: e.opts.MaxQARetries,
							BlockingIssues: ws.BlockingIssues,
						})
					_ = e.store.AppendEvents(ctx, exhaustedEvt)
					note := fmt.Sprintf("[qa.exhausted] %d blocking issue(s) unresolved after %d remediation cycle(s): %v",
						len(ws.BlockingIssues), e.opts.MaxQARetries, ws.BlockingIssues)
					ws.AllSuggestions = append(ws.AllSuggestions, note)
					return fmt.Errorf("qa remediation exhausted after %d cycle(s): %v", e.opts.MaxQARetries, ws.BlockingIssues)
				}

				// PM-led triage before Architect remediation. The PM classifies whether
				// blockers are requirement, design, implementation, or environment issues;
				// the Architect then uses that brief to produce targeted pod tasks.
				if personaRequired(state.PersonaProjectMgr) {
					ws.Execution.CurrentPersona = state.PersonaProjectMgr
					ws.Execution.RemediationAttempt = qaCycle
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.runRemediationTriage(ctx, ws, snap, qaCycle); err != nil {
						return fmt.Errorf("remediation triage (cycle %d): %w", qaCycle, err)
					}
					if err := e.runToolchainCheckpoint(ctx, ws, fmt.Sprintf("remediation-triage-%d", qaCycle)); err != nil {
						return fmt.Errorf("remediation triage checkpoint (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkControlState(ctx, ws); err != nil {
						return err
					}
				}

				if personaRequired(state.PersonaMatriarch) {
					ws.Execution.CurrentPersona = state.PersonaMatriarch
					ws.Execution.RemediationAttempt = qaCycle
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.runRemediationMatriarch(ctx, ws, snap, qaCycle); err != nil {
						return fmt.Errorf("remediation matriarch (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkControlState(ctx, ws); err != nil {
						return err
					}
				}

				// Architect-led remediation: re-plan with the PM brief and current blocking issues.
				if personaRequired(state.PersonaArchitect) && personaRequired(state.PersonaPod) {
					ws.Execution.CurrentPersona = state.PersonaArchitect
					ws.Execution.RemediationAttempt = qaCycle
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.runRemediationPlanning(ctx, ws, snap, qaCycle); err != nil {
						return fmt.Errorf("remediation planning (cycle %d): %w", qaCycle, err)
					}
					if err := e.runToolchainCheckpoint(ctx, ws, fmt.Sprintf("remediation-plan-%d", qaCycle)); err != nil {
						return fmt.Errorf("remediation plan checkpoint (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkControlState(ctx, ws); err != nil {
						return err
					}

					ws.Execution.CurrentPersona = state.PersonaPod
					_ = e.store.SaveWorkflow(ctx, ws)

					if err := e.ensureToolchainBootstrap(ctx, ws, fmt.Sprintf("remediation-%d-bootstrap", qaCycle)); err != nil {
						return fmt.Errorf("remediation bootstrap (cycle %d): %w", qaCycle, err)
					}
					if err := e.runToolchainCheckpoint(ctx, ws, fmt.Sprintf("remediation-%d-bootstrap", qaCycle)); err != nil {
						return fmt.Errorf("remediation bootstrap checkpoint (cycle %d): %w", qaCycle, err)
					}
					if err := e.runPodPhase(ctx, ws, snap); err != nil {
						return fmt.Errorf("pod remediation (cycle %d): %w", qaCycle, err)
					}
					validationIssues, err := e.runToolchainValidation(ctx, ws, fmt.Sprintf("remediation-%d", qaCycle))
					if err != nil {
						return fmt.Errorf("remediation validation (cycle %d): %w", qaCycle, err)
					}
					if err := e.runToolchainCheckpoint(ctx, ws, fmt.Sprintf("remediation-%d", qaCycle)); err != nil {
						return fmt.Errorf("remediation checkpoint (cycle %d): %w", qaCycle, err)
					}
					if err := e.checkControlState(ctx, ws); err != nil {
						return err
					}
					// Clear previous QA findings after remediation, but preserve fresh
					// engine validation failures so QA reviews the current repo state.
					ws.BlockingIssues = validationIssues
				}
				if !personaRequired(state.PersonaArchitect) || !personaRequired(state.PersonaPod) {
					// Clear blocking issues accumulated in this pass so QA evaluates
					// any non-implementation remediation fresh. Retain suggestions.
					ws.BlockingIssues = nil
				}
			}
		}
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}
	}

	// Phase 6: Finalizer (includes inline Refiner retrospective)
	//
	// Hard validation gate: if EnforceValidationGate is set and the most
	// recent validation run for a software-class workflow failed, refuse to
	// finalize.  This stops workflows where QA gave a visual-only pass but
	// the toolchain reported real failures.
	if e.opts.EnforceValidationGate && workflowNeedsToolchain(ws.Mode) {
		if blocked, run := lastValidationFailed(ws); blocked {
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
			if run != nil {
				toolchainID = run.ToolchainID
			}
			profile := ""
			if run != nil {
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
	}

	if personaRequired(state.PersonaFinalizer) && !phaseComplete(state.PersonaFinalizer) {
		if err := e.runPersona(ctx, ws, state.PersonaFinalizer, snap); err != nil {
			return fmt.Errorf("finalizer phase: %w", err)
		}
	}

	return nil
}

// runPersona dispatches a single persona phase against the current workflow state.
func (e *Engine) runPersona(ctx context.Context, ws *state.WorkflowState, kind state.PersonaKind, snap *customization.Snapshot) error {
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(kind)
	if !ok {
		return fmt.Errorf("persona %q not registered", kind)
	}

	packet := e.buildPacket(ws, kind, snap)
	if packet.ProviderName == "" {
		return fmt.Errorf("engine: no provider resolved for persona %q", kind)
	}
	if packet.ModelName == "" {
		return fmt.Errorf("engine: no allowed model resolved for provider %q", packet.ProviderName)
	}

	startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventPersonaStarted, kind,
		events.PersonaStartedPayload{
			Persona:      kind,
			ProviderName: packet.ProviderName,
			ModelName:    packet.ModelName,
		})
	_ = e.store.AppendEvents(ctx, startEvt)

	// Pre-announce: update current_persona in persisted state before the LLM
	// call starts so that GET /api/v1/workflows/{id} reflects the in-flight persona
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
		if err := e.checkControlState(ctx, ws); err != nil {
			return err
		}

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
					Reason:       err.Error(),
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

			if err := e.checkControlState(ctx, ws); err != nil {
				return err
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

	if err := e.checkControlState(ctx, ws); err != nil {
		return err
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

	// ── Finalizer post-processing ─────────────────────────────────────────────
	if kind == state.PersonaFinalizer {
		if err := e.postProcessFinalizer(ctx, ws); err != nil {
			ws.Finalization = nil
			delete(ws.Summaries, state.PersonaFinalizer)

			failEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventPersonaFailed, kind,
				events.PersonaFailedPayload{Persona: kind, Error: err.Error()})
			_ = e.store.AppendEvents(ctx, failEvt)
			return err
		}
	}

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

func (e *Engine) postProcessFinalizer(ctx context.Context, ws *state.WorkflowState) error {
	// Route each improvement through the dispatcher (when configured and not
	// already inside an improvement workflow — recursion guard).
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

	// Resolve the delivery action: caller-provided wins over LLM choice.
	actionKey := ""
	if ws.DeliveryAction != "" {
		actionKey = ws.DeliveryAction
	} else if ws.Finalization != nil && ws.Finalization.Action != "" {
		actionKey = ws.Finalization.Action
	}

	if actionKey == "" {
		return nil
	}

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
			ws.Finalization.Metadata = make(map[string]any)
		}
		for k, v := range actionOut.Metadata {
			ws.Finalization.Metadata[k] = v
		}
	}

	return nil
}

// runPodPhase runs the Pod against every runnable task that is
// assigned to the Pod persona.  Tasks assigned to any other persona are
// silently skipped so role boundaries are maintained at the engine layer
// regardless of what the LLM's Architect output said.
func (e *Engine) runPodPhase(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot) error {
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(state.PersonaPod)
	if !ok {
		return fmt.Errorf("pod persona not registered")
	}

	// Resolve the provider and model once for the whole phase so persona events
	// carry accurate routing information even though tasks run serially.
	implPacket := e.buildPacket(ws, state.PersonaPod, snap)
	implPhaseStart := time.Now()

	implStartEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventPersonaStarted, state.PersonaPod,
		events.PersonaStartedPayload{
			Persona:      state.PersonaPod,
			ProviderName: implPacket.ProviderName,
			ModelName:    implPacket.ModelName,
		})
	_ = e.store.AppendEvents(ctx, implStartEvt)
	e.refreshPodTaskReadiness(ws)

	var implErr error
	var tasksAttempted int
	var podTasksSeen int
	var blockedByDependencies int
	for {
		e.refreshPodTaskReadiness(ws)
		ranTaskThisPass := false
		podTasksSeen = 0
		blockedByDependencies = 0
		for i := range ws.Tasks {
			if err := e.checkControlState(ctx, ws); err != nil {
				return err
			}

			t := &ws.Tasks[i]

			// Only process Pod-owned tasks that still need work.
			// Normalise to lowercase before comparing so that model responses with
			// "Pod" (capital I) or other case variants are not silently skipped.
			if state.PersonaKind(strings.ToLower(strings.TrimSpace(string(t.AssignedTo)))) != state.PersonaPod {
				continue
			}
			podTasksSeen++
			if !e.taskDependenciesSatisfied(ws, t) {
				if t.Status == state.TaskStatusReady {
					t.Status = state.TaskStatusPending
				}
				blockedByDependencies++
				continue
			}
			if t.Status != state.TaskStatusReady &&
				t.Status != state.TaskStatusPending &&
				t.Status != state.TaskStatusFailed {
				continue
			}

			tasksAttempted++
			ranTaskThisPass = true

			// Update visible progress before calling LLM.
			ws.Execution.CurrentPersona = state.PersonaPod
			ws.Execution.ActiveTaskID = t.ID
			ws.Execution.ActiveTaskTitle = t.Title

			// Build a packet scoped to this single task.
			packet := e.buildPacket(ws, state.PersonaPod, snap)
			packet.Tasks = []state.Task{*t}

			// Mark task running before LLM call so callers see the transition.
			t.Status = state.TaskStatusRunning
			if err := e.store.SaveWorkflow(ctx, ws); err != nil {
				return fmt.Errorf("save before pod task %s: %w", t.ID[:8], err)
			}

			startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventTaskStarted, state.PersonaPod,
				events.TaskStartedPayload{TaskID: t.ID, Title: t.Title})
			_ = e.store.AppendEvents(ctx, startEvt)

			taskStart := time.Now()
			taskCtx, taskCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
			out, err := p.Execute(taskCtx, packet)
			taskElapsed := time.Since(taskStart)
			taskCancel()
			if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
				return controlErr
			}
			if err != nil {
				t.Status = state.TaskStatusFailed
				ws.Execution.ActiveTaskID = ""
				ws.Execution.ActiveTaskTitle = ""
				failEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
					events.EventTaskFailed, state.PersonaPod,
					events.TaskFailedPayload{TaskID: t.ID, Title: t.Title, Error: err.Error()})
				_ = e.store.AppendEvents(ctx, failEvt)
				_ = e.store.SaveWorkflow(ctx, ws)
				implErr = fmt.Errorf("pod task %s: %w", t.ID[:8], err)
				break
			}

			// Mark task complete and attach artifacts.
			now := time.Now().UTC()
			t.Status = state.TaskStatusCompleted
			t.CompletedAt = &now
			e.refreshPodTaskReadiness(ws)
			for _, art := range out.Artifacts {
				ws.Artifacts = mergeOrAppendArtifact(ws.Mode, ws.Artifacts, art)
				artEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
					events.EventArtifactProduced, state.PersonaPod,
					events.ArtifactProducedPayload{
						TaskID:        t.ID,
						ArtifactName:  art.Name,
						Kind:          string(art.Kind),
						ContentLength: len(art.Content),
					})
				_ = e.store.AppendEvents(ctx, artEvt)

				// Persist the artifact to the on-disk workspace so that the
				// go-toolchain MCP (and git checkpoint) can read the produced files.
				// This is a safety net for models that produce the code in their
				// Phase B JSON response but do not call write_file during Phase A.
				if ws.Execution.Workspace != nil {
					if writeErr := e.writeArtifactToWorkspace(ws, art); writeErr != nil {
						logger.Warn("engine: failed to flush artifact to workspace",
							zap.String("workflow_id", ws.ID),
							zap.String("task_id", t.ID),
							zap.String("artifact_name", art.Name),
							zap.String("artifact_kind", string(art.Kind)),
							zap.Error(writeErr),
						)
					}
				}
			}

			if ws.Summaries == nil {
				ws.Summaries = make(map[state.PersonaKind]string)
			}
			// Append per-task summary rather than overwrite.
			shortID := t.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			ws.Summaries[state.PersonaPod] += fmt.Sprintf("[%s] %s\n", shortID, out.Summary)

			doneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
				events.EventTaskCompleted, state.PersonaPod,
				events.TaskCompletedPayload{
					TaskID:     t.ID,
					Title:      t.Title,
					Summary:    out.Summary,
					DurationMs: taskElapsed.Milliseconds(),
				})
			_ = e.store.AppendEvents(ctx, doneEvt)
		}
		if implErr != nil || !ranTaskThisPass {
			break
		}
	}

	// Emit persona.completed (or persona.failed) for the pod phase as a whole.
	implElapsed := time.Since(implPhaseStart)
	ws.Execution.ActiveTaskID = ""
	ws.Execution.ActiveTaskTitle = ""
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}
	if implErr != nil {
		implFailEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaFailed, state.PersonaPod,
			events.PersonaFailedPayload{Persona: state.PersonaPod, Error: implErr.Error()})
		_ = e.store.AppendEvents(ctx, implFailEvt)
		_ = e.store.SaveWorkflow(ctx, ws)
		return implErr
	}

	// If 0 tasks were attempted the architect produced nothing for the
	// pod.  Fail loudly here rather than letting QA run against an
	// empty artifact list, which would trigger the confusing "No artifact
	// provided" → remediation → "no valid pod tasks" death spiral.
	if tasksAttempted == 0 {
		noTaskErr := fmt.Errorf("architect produced no runnable pod tasks — "+
			"the workflow request may be empty, the architect may have misunderstood it, "+
			"or task dependencies remain unsatisfied (pod tasks=%d blocked_by_dependencies=%d)",
			podTasksSeen, blockedByDependencies)
		implFailEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaFailed, state.PersonaPod,
			events.PersonaFailedPayload{Persona: state.PersonaPod, Error: noTaskErr.Error()})
		_ = e.store.AppendEvents(ctx, implFailEvt)
		_ = e.store.SaveWorkflow(ctx, ws)
		return noTaskErr
	}

	implDoneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventPersonaCompleted, state.PersonaPod,
		events.PersonaCompletedPayload{
			Persona:    state.PersonaPod,
			DurationMs: implElapsed.Milliseconds(),
			Summary:    ws.Summaries[state.PersonaPod],
		})
	_ = e.store.AppendEvents(ctx, implDoneEvt)

	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) refreshPodTaskReadiness(ws *state.WorkflowState) {
	if ws == nil {
		return
	}
	for i := range ws.Tasks {
		t := &ws.Tasks[i]
		if state.PersonaKind(strings.ToLower(strings.TrimSpace(string(t.AssignedTo)))) != state.PersonaPod {
			continue
		}
		switch t.Status {
		case state.TaskStatusCompleted, state.TaskStatusRunning:
			continue
		}
		if e.taskDependenciesSatisfied(ws, t) {
			if t.Status == state.TaskStatusPending {
				t.Status = state.TaskStatusReady
			}
			continue
		}
		if t.Status == state.TaskStatusReady {
			t.Status = state.TaskStatusPending
		}
	}
}

func (e *Engine) taskDependenciesSatisfied(ws *state.WorkflowState, task *state.Task) bool {
	if ws == nil || task == nil || len(task.DependsOn) == 0 {
		return true
	}
	statuses := make(map[string]state.TaskStatus, len(ws.Tasks))
	for _, candidate := range ws.Tasks {
		statuses[candidate.ID] = candidate.Status
	}
	for _, depID := range task.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		if statuses[depID] != state.TaskStatusCompleted {
			return false
		}
	}
	return true
}

// runRemediationPlanning invokes the Architect with the current blocking issues
// so it can produce a targeted set of pod-only remediation tasks.
// The new tasks are appended to the task graph (existing completed tasks are
// preserved for audit) with Attempt and RemediationSource set.
func (e *Engine) runRemediationPlanning(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot, qaCycle int) error {
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(state.PersonaArchitect)
	if !ok {
		return fmt.Errorf("architect persona not registered")
	}

	packet := e.buildPacket(ws, state.PersonaArchitect, snap)
	packet.IsRemediation = true

	archCtx, archCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(archCtx, packet)
	archCancel()
	if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return fmt.Errorf("architect remediation: %w", err)
	}

	// Validate: Architect must only produce Pod-assigned tasks during
	// remediation.  Any task assigned to another persona is dropped with a warning.
	validTasks := make([]state.Task, 0, len(out.Tasks))
	for _, t := range out.Tasks {
		// Normalise assigned_to to lowercase so that model responses with
		// "Pod" (capital I) or other case variants are accepted.
		normAssigned := state.PersonaKind(strings.ToLower(strings.TrimSpace(string(t.AssignedTo))))
		if normAssigned != state.PersonaPod && normAssigned != "" {
			// Emit a suggestion so the issue is visible without aborting the cycle.
			ws.AllSuggestions = append(ws.AllSuggestions,
				fmt.Sprintf("[warning][remediation] Architect emitted task %q assigned to %q; only 'pod' is valid during remediation — task dropped", t.Title, t.AssignedTo))
			continue
		}
		if normAssigned == "" {
			t.AssignedTo = state.PersonaPod
		} else {
			t.AssignedTo = normAssigned // store normalised lowercase form
		}
		t.Attempt = qaCycle
		t.RemediationSource = "qa_remediation"
		validTasks = append(validTasks, t)
	}

	if len(validTasks) == 0 {
		return fmt.Errorf("architect produced no valid pod tasks for remediation cycle %d (blocking issues: %v)", qaCycle, ws.BlockingIssues)
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
	if summary := strings.TrimSpace(out.Summary); summary != "" {
		e.appendReviewThreadEntries(ws, out, fmt.Sprintf("[remediation cycle %d] %s", qaCycle, summary))
	}

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return err
	}
	return e.appendPlanRemediation(ctx, ws, qaCycle)
}

func (e *Engine) runRemediationMatriarch(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot, qaCycle int) error {
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(state.PersonaMatriarch)
	if !ok {
		return fmt.Errorf("matriarch persona not registered")
	}

	packet := e.buildPacket(ws, state.PersonaMatriarch, snap)
	packet.IsRemediation = true

	matCtx, matCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(matCtx, packet)
	matCancel()
	if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return fmt.Errorf("matriarch remediation: %w", err)
	}

	if ws.Summaries == nil {
		ws.Summaries = make(map[state.PersonaKind]string)
	}
	if summary := strings.TrimSpace(out.Summary); summary != "" {
		prefixed := fmt.Sprintf("[remediation cycle %d] %s", qaCycle, summary)
		ws.Summaries[state.PersonaMatriarch] += prefixed + "\n"
		e.appendReviewThreadEntries(ws, out, prefixed)
	} else {
		e.appendReviewThreadEntries(ws, out, "")
	}
	if len(out.Suggestions) > 0 {
		ws.AllSuggestions = append(ws.AllSuggestions, out.Suggestions...)
	}

	return e.store.SaveWorkflow(ctx, ws)
}

// runRemediationTriage routes QA failures through the Project Manager before
// Architect remediation. The PM's structured requirements are not applied here;
// this pass is used as a decision brief so the Architect receives PM-owned
// classification without mutating the original acceptance baseline mid-loop.
func (e *Engine) runRemediationTriage(ctx context.Context, ws *state.WorkflowState, snap *customization.Snapshot, qaCycle int) error {
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(state.PersonaProjectMgr)
	if !ok {
		return fmt.Errorf("project manager persona not registered")
	}

	packet := e.buildPacket(ws, state.PersonaProjectMgr, snap)
	packet.IsRemediation = true

	pmCtx, pmCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(pmCtx, packet)
	pmCancel()
	if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return fmt.Errorf("pm remediation triage: %w", err)
	}

	if ws.Summaries == nil {
		ws.Summaries = make(map[state.PersonaKind]string)
	}
	ws.Summaries[state.PersonaProjectMgr] += fmt.Sprintf("[remediation cycle %d] %s\n", qaCycle, out.Summary)
	if len(out.Suggestions) > 0 {
		ws.AllSuggestions = append(ws.AllSuggestions, out.Suggestions...)
	}
	if summary := strings.TrimSpace(out.Summary); summary != "" {
		e.appendReviewThreadEntries(ws, out, fmt.Sprintf("[remediation cycle %d] %s", qaCycle, summary))
	}

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return err
	}
	// Append the triage section to plan.md (and conditionally to
	// constitution.md when the brief flags a "requirement gap"). Done after
	// the save so that even if the doc-write fails the PM summary is durable.
	return e.appendPlanTriage(ctx, ws, qaCycle, out.Summary, ws.BlockingIssues)
}

func (e *Engine) ensureWorkspaceAndToolchain(ctx context.Context, ws *state.WorkflowState) error {
	if !workflowNeedsToolchain(ws.Mode) || len(e.opts.Toolchains) == 0 {
		return nil
	}
	if ws.Execution.Workspace == nil && strings.TrimSpace(e.opts.WorkspaceRoot) != "" {
		workspacePath := filepath.Join(e.opts.WorkspaceRoot, ws.ID)
		// Mode 0o775 (group-write) so toolchain MCP servers running under the
		// shared fsGroup uid can also write into the directory created here.
		// gofmt's atomic-write tempfile and `git init`'s .git creation both
		// require directory write permission.
		if err := os.MkdirAll(workspacePath, 0o775); err != nil {
			return fmt.Errorf("create workspace %q: %w", workspacePath, err)
		}
		// Defensive chmod: MkdirAll honours umask so the actual mode may be
		// 0o755 even when 0o775 is requested. Force the group-write bit.
		if err := os.Chmod(workspacePath, 0o775); err != nil {
			return fmt.Errorf("chmod workspace %q: %w", workspacePath, err)
		}
		ws.Execution.Workspace = &state.WorkspaceInfo{
			Path:      workspacePath,
			Branch:    "workflow/" + ws.ID,
			CreatedBy: "engine",
		}
	}
	if ws.Execution.Workspace != nil && ws.Execution.Workspace.RepoURL == "" {
		if err := e.ensureRequestedRepository(ctx, ws); err != nil {
			return err
		}
	}
	if ws.Execution.Toolchain == nil {
		if tc, language, ok := e.selectToolchain(ws); ok {
			profile := "default"
			if _, exists := tc.ValidationProfiles[profile]; !exists {
				for name := range tc.ValidationProfiles {
					profile = name
					break
				}
			}
			ws.Execution.Toolchain = &state.ToolchainSelection{
				ID:       tc.ID,
				Language: language,
				Profile:  profile,
				Tools:    tc.Capabilities,
			}
		}
	}

	// Refuse to proceed when the selected toolchain's MCP server is required
	// but unreachable — without it, every validation/checkpoint call would
	// fail and the workflow would burn QA retries before discovering the
	// underlying infrastructure problem.
	if ws.Execution.Toolchain != nil && e.opts.MCPRegistry != nil {
		if err := e.opts.MCPRegistry.ToolchainReachable(ws.Execution.Toolchain.ID); err != nil {
			e.emitMCPCapabilityMissing(ctx, ws,
				ToolchainConfig{ID: ws.Execution.Toolchain.ID},
				"workflow_start", "preflight", err.Error())
			return fmt.Errorf("toolchain preflight: %w", err)
		}
	}
	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) ensureRequestedRepository(ctx context.Context, ws *state.WorkflowState) error {
	cfg, ok := requestedRepositoryConfig(ws)
	if !ok {
		return nil
	}
	fullName := requestedRepositoryFullName(cfg)
	repoURL := "https://github.com/" + fullName
	rawCfg, _ := json.Marshal(cfg)
	out, err := actions.Global.Execute(ctx, actions.ActionCreateRepo, actions.Input{
		Workflow: ws,
		Config:   rawCfg,
	})
	if err != nil {
		// Treat "already exists" as attach-to-existing. GitHub returns 422 for
		// duplicate repository creation, and attaching is safer than burning the
		// workflow before implementation has a chance to checkpoint locally.
		if strings.Contains(err.Error(), "422") || strings.Contains(strings.ToLower(err.Error()), "already exists") {
			ws.Execution.Workspace.RepoURL = repoURL
			ws.AllSuggestions = append(ws.AllSuggestions, fmt.Sprintf("[workspace] attached existing repository %s", repoURL))
			return nil
		}
		return fmt.Errorf("create requested repository %s: %w", fullName, err)
	}
	if out != nil && out.Metadata != nil && out.Metadata["repo_url"] != "" {
		repoURL = out.Metadata["repo_url"]
	}
	ws.Execution.Workspace.RepoURL = repoURL
	ws.AllSuggestions = append(ws.AllSuggestions, fmt.Sprintf("[workspace] created repository %s before implementation", repoURL))
	return nil
}

type repositoryConfig struct {
	Name        string `json:"name"`
	Org         string `json:"org,omitempty"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private,omitempty"`
	Owner       string `json:"-"`
}

func requestedRepositoryConfig(ws *state.WorkflowState) (repositoryConfig, bool) {
	if ws == nil {
		return repositoryConfig{}, false
	}
	if strings.EqualFold(ws.DeliveryAction, string(actions.ActionCreateRepo)) && len(ws.DeliveryConfig) > 0 {
		var cfg repositoryConfig
		if err := json.Unmarshal(ws.DeliveryConfig, &cfg); err == nil && cfg.Name != "" {
			cfg.Owner = cfg.Org
			if cfg.Description == "" {
				cfg.Description = firstNonEmpty(ws.Title, "go-orca workflow "+ws.ID)
			}
			return cfg, true
		}
	}
	owner, repo, ok := githubRepoFromText(ws.Request)
	if !ok {
		return repositoryConfig{}, false
	}
	return repositoryConfig{
		Name:        repo,
		Owner:       owner,
		Description: firstNonEmpty(ws.Title, "go-orca workflow "+ws.ID),
		Private:     mentionsPrivateRepo(ws.Request),
	}, true
}

func requestedRepositoryFullName(cfg repositoryConfig) string {
	owner := firstNonEmpty(cfg.Owner, cfg.Org)
	if owner == "" {
		return cfg.Name
	}
	return owner + "/" + cfg.Name
}

var githubRepoPattern = regexp.MustCompile(`(?i)github\.com[:/]+([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)`)

func githubRepoFromText(s string) (string, string, bool) {
	m := githubRepoPattern.FindStringSubmatch(s)
	if len(m) != 3 {
		return "", "", false
	}
	repo := strings.TrimSuffix(m[2], ".git")
	repo = strings.TrimRight(repo, ".,);]}")
	if m[1] == "" || repo == "" {
		return "", "", false
	}
	return m[1], repo, true
}

func mentionsPrivateRepo(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "private repo") || strings.Contains(lower, "private repository")
}

func workflowNeedsToolchain(mode state.WorkflowMode) bool {
	switch mode {
	case state.WorkflowModeSoftware, state.WorkflowModeMixed, state.WorkflowModeOps:
		return true
	default:
		return false
	}
}

func (e *Engine) selectToolchain(ws *state.WorkflowState) (ToolchainConfig, string, bool) {
	haystack := strings.ToLower(ws.Request + " " + ws.Title)
	if ws.Design != nil {
		haystack += " " + strings.ToLower(strings.Join(ws.Design.TechStack, " "))
	}
	for _, tc := range e.opts.Toolchains {
		for _, lang := range tc.Languages {
			needle := strings.ToLower(strings.TrimSpace(lang))
			if needle != "" && strings.Contains(haystack, needle) {
				return tc, needle, true
			}
		}
	}
	if len(e.opts.Toolchains) > 0 {
		return e.opts.Toolchains[0], "", true
	}
	return ToolchainConfig{}, "", false
}

// writeArtifactToWorkspace persists a code or config artifact to the
// engine-owned workspace directory on disk.  It is called after each pod task
// so that the go-toolchain MCP (and git checkpoint) can see the produced files
// even when the LLM chose not to call write_file itself during Phase A.
//
// Safety constraints:
//   - Only code and config artifact kinds are written.
//   - The artifact name must be a relative, clean path with no whitespace.
//   - Path traversal attempts (e.g. "../etc/passwd") are rejected.
func (e *Engine) writeArtifactToWorkspace(ws *state.WorkflowState, art state.Artifact) error {
	if ws.Execution.Workspace == nil {
		return nil
	}
	if art.Kind != state.ArtifactKindCode && art.Kind != state.ArtifactKindConfig {
		return nil
	}
	name := strings.TrimSpace(art.Name)
	// Skip descriptive names like "fixed package structure" or "updated go.mod".
	// Valid file paths must not contain whitespace.
	if name == "" || strings.ContainsAny(name, " \t\n\r") {
		return nil
	}
	clean := filepath.Clean(name)
	// Reject absolute paths and upward traversal.
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("writeArtifactToWorkspace: artifact name %q would escape workspace", name)
	}
	dest := filepath.Join(ws.Execution.Workspace.Path, clean)
	// Final containment check.
	if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator),
		filepath.Clean(ws.Execution.Workspace.Path)+string(filepath.Separator)) {
		return fmt.Errorf("writeArtifactToWorkspace: resolved path %q escapes workspace %q", dest, ws.Execution.Workspace.Path)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("writeArtifactToWorkspace: mkdir %q: %w", filepath.Dir(dest), err)
	}
	return os.WriteFile(dest, []byte(art.Content), 0o644)
}

func (e *Engine) runToolchainValidation(ctx context.Context, ws *state.WorkflowState, phase string) ([]string, error) {
	if ws.Execution.Toolchain == nil || e.opts.ToolRegistry == nil {
		return nil, nil
	}
	tc, ok := e.toolchainByID(ws.Execution.Toolchain.ID)
	if !ok {
		return []string{fmt.Sprintf("toolchain %q is selected but not configured", ws.Execution.Toolchain.ID)}, nil
	}
	profileName := ws.Execution.Toolchain.Profile
	if profileName == "" {
		profileName = "default"
	}
	profile := tc.ValidationProfiles[profileName]
	if len(profile) == 0 {
		return nil, nil
	}

	run := state.ValidationRun{
		ID:          fmt.Sprintf("validation-%d", len(ws.Execution.ValidationRuns)+1),
		Phase:       phase,
		ToolchainID: tc.ID,
		Profile:     profileName,
		Passed:      true,
		StartedAt:   time.Now().UTC(),
	}
	issues := make([]string, 0)

	for _, capability := range profile {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		toolName := tc.CapabilityTools[capability]
		if toolName == "" {
			toolName = capability
		}
		step := state.ValidationStep{Capability: capability, Tool: toolName, Passed: true}
		args := e.toolchainArgs(ws, tc, phase, capability, false)
		callStart := time.Now()

		if e.opts.MCPRegistry != nil {
			cr, rerr := e.opts.MCPRegistry.CallCapability(ctx, tc.ID, capability, args)
			if rerr != nil {
				step.Passed = false
				step.Error = rerr.Error()
				e.emitMCPCapabilityMissing(ctx, ws, tc, capability, phase, rerr.Error())
			} else {
				step.Tool = cr.ToolName
				step.Passed = cr.Passed
				step.Output = trimValidationOutput(firstNonEmpty(cr.Output, cr.Stdout))
				if cr.Stderr != "" && step.Output != "" {
					step.Output += "\n" + cr.Stderr
				} else if cr.Stderr != "" {
					step.Output = trimValidationOutput(cr.Stderr)
				}
				step.Error = cr.Error
			}
			e.emitMCPToolEvent(ctx, ws, tc, capability, step.Tool, phase, step.Passed, step.Error, time.Since(callStart))
		} else if _, ok := e.opts.ToolRegistry.Get(toolName); !ok {
			step.Passed = false
			step.Error = fmt.Sprintf("tool %q not registered", toolName)
		} else {
			res := e.opts.ToolRegistry.Call(ctx, toolName, args)
			if res.Error != "" {
				step.Passed = false
				step.Error = res.Error
			} else {
				toolPassed, output, errMsg := parseToolchainResult(res.Output)
				step.Passed = toolPassed
				step.Output = output
				step.Error = errMsg
			}
		}
		if !step.Passed {
			run.Passed = false
			issue := fmt.Sprintf("validation %s failed via %s: %s", capability, toolName, firstNonEmpty(step.Error, step.Output))
			issues = append(issues, issue)
		}
		run.Steps = append(run.Steps, step)
	}
	run.CompletedAt = time.Now().UTC()
	if run.Passed {
		run.Summary = fmt.Sprintf("%s validation passed (%d step(s))", tc.ID, len(run.Steps))
	} else {
		run.Summary = fmt.Sprintf("%s validation failed (%d issue(s))", tc.ID, len(issues))
	}
	ws.Execution.ValidationRuns = append(ws.Execution.ValidationRuns, run)

	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventValidationRun, "",
		events.ValidationRunPayload{Phase: phase, ToolchainID: tc.ID, Profile: profileName, Passed: run.Passed, Issues: issues})
	_ = e.store.AppendEvents(ctx, evt)

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return issues, err
	}
	return issues, nil
}

func (e *Engine) runToolchainCheckpoint(ctx context.Context, ws *state.WorkflowState, phase string) error {
	if ws.Execution.Toolchain == nil || e.opts.ToolRegistry == nil {
		return nil
	}
	tc, ok := e.toolchainByID(ws.Execution.Toolchain.ID)
	if !ok || strings.TrimSpace(tc.CheckpointCapability) == "" {
		return nil
	}
	capability := strings.TrimSpace(tc.CheckpointCapability)
	toolName := tc.CapabilityTools[capability]
	if toolName == "" {
		toolName = capability
	}
	args := e.toolchainArgs(ws, tc, phase, capability, tc.PushCheckpoints)
	callStart := time.Now()
	var (
		commitSHA, branch, message string
		pushed                     bool
	)
	if e.opts.MCPRegistry != nil {
		checkpointToolchainID := tc.ID
		cr, rerr := e.opts.MCPRegistry.CallCapability(ctx, checkpointToolchainID, capability, args)
		if rerr != nil && tc.ID != "git" && strings.HasPrefix(capability, "git_") {
			checkpointToolchainID = "git"
			cr, rerr = e.opts.MCPRegistry.CallCapability(ctx, checkpointToolchainID, capability, args)
		}
		if rerr != nil {
			eventTC := tc
			eventTC.ID = checkpointToolchainID
			e.emitMCPCapabilityMissing(ctx, ws, eventTC, capability, phase, rerr.Error())
			msg := fmt.Sprintf("[checkpoint] %s", rerr.Error())
			ws.AllSuggestions = append(ws.AllSuggestions, msg)
			// When the operator explicitly requested checkpoints to be pushed,
			// surface the failure as a blocking issue so QA flags it and the
			// workflow doesn't silently complete with an empty remote repo.
			if tc.PushCheckpoints {
				ws.BlockingIssues = append(ws.BlockingIssues,
					fmt.Sprintf("checkpoint %s failed for toolchain %s: %s — code was not committed/pushed to the repository",
						capability, checkpointToolchainID, rerr.Error()))
			}
			return e.store.SaveWorkflow(ctx, ws)
		}
		if cr.Error != "" {
			eventTC := tc
			eventTC.ID = checkpointToolchainID
			e.emitMCPToolEvent(ctx, ws, eventTC, capability, cr.ToolName, phase, false, cr.Error, time.Since(callStart))
			msg := fmt.Sprintf("[checkpoint] %s failed: %s", cr.ToolName, cr.Error)
			ws.AllSuggestions = append(ws.AllSuggestions, msg)
			if tc.PushCheckpoints {
				ws.BlockingIssues = append(ws.BlockingIssues,
					fmt.Sprintf("checkpoint %s via %s failed: %s — code was not committed/pushed to the repository",
						capability, cr.ToolName, cr.Error))
			}
			return e.store.SaveWorkflow(ctx, ws)
		}
		commitSHA, branch, message, pushed = parseCheckpointResult(cr.Raw)
		eventTC := tc
		eventTC.ID = checkpointToolchainID
		e.emitMCPToolEvent(ctx, ws, eventTC, capability, cr.ToolName, phase, true, "", time.Since(callStart))
	} else {
		if _, ok := e.opts.ToolRegistry.Get(toolName); !ok {
			ws.AllSuggestions = append(ws.AllSuggestions, fmt.Sprintf("[checkpoint] tool %q not registered; checkpoint skipped", toolName))
			return e.store.SaveWorkflow(ctx, ws)
		}
		res := e.opts.ToolRegistry.Call(ctx, toolName, args)
		if res.Error != "" {
			ws.AllSuggestions = append(ws.AllSuggestions, fmt.Sprintf("[checkpoint] %s failed: %s", toolName, res.Error))
			return e.store.SaveWorkflow(ctx, ws)
		}
		commitSHA, branch, message, pushed = parseCheckpointResult(res.Output)
	}
	checkpoint := state.Checkpoint{
		ID:          fmt.Sprintf("checkpoint-%d", len(ws.Execution.Checkpoints)+1),
		Phase:       phase,
		ToolchainID: tc.ID,
		CommitSHA:   commitSHA,
		Branch:      firstNonEmpty(branch, workspaceBranch(ws)),
		Message:     message,
		Pushed:      pushed,
		CreatedAt:   time.Now().UTC(),
	}
	ws.Execution.Checkpoints = append(ws.Execution.Checkpoints, checkpoint)

	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventCheckpointCreated, "",
		events.CheckpointCreatedPayload{Phase: phase, ToolchainID: tc.ID, CommitSHA: commitSHA, Branch: checkpoint.Branch, Pushed: pushed})
	_ = e.store.AppendEvents(ctx, evt)

	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) toolchainByID(id string) (ToolchainConfig, bool) {
	for _, tc := range e.opts.Toolchains {
		if tc.ID == id {
			return tc, true
		}
	}
	return ToolchainConfig{}, false
}

func (e *Engine) ensureToolchainBootstrap(ctx context.Context, ws *state.WorkflowState, phase string) error {
	if ws == nil || ws.Execution.Toolchain == nil || ws.Execution.Workspace == nil {
		return nil
	}
	tc, ok := e.toolchainByID(ws.Execution.Toolchain.ID)
	if !ok || !toolchainSupportsCapability(tc, capabilities.InitProject) {
		return nil
	}
	manifestPath, ok := bootstrapManifestPath(ws.Execution.Workspace.Path, tc, ws.Execution.Toolchain)
	if !ok {
		return nil
	}
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("bootstrap stat %q: %w", manifestPath, err)
	}

	args := e.toolchainArgs(ws, tc, phase, capabilities.InitProject, false)
	callStart := time.Now()
	if e.opts.MCPRegistry != nil {
		cr, rerr := e.opts.MCPRegistry.CallCapability(ctx, tc.ID, capabilities.InitProject, args)
		if rerr != nil {
			e.emitMCPCapabilityMissing(ctx, ws, tc, capabilities.InitProject, phase, rerr.Error())
			return fmt.Errorf("toolchain bootstrap: %w", rerr)
		}
		e.emitMCPToolEvent(ctx, ws, tc, capabilities.InitProject, cr.ToolName, phase, cr.Passed, cr.Error, time.Since(callStart))
		if cr.Error != "" {
			return fmt.Errorf("toolchain bootstrap via %s: %s", cr.ToolName, cr.Error)
		}
		if !cr.Passed {
			return fmt.Errorf("toolchain bootstrap via %s reported failure: %s", cr.ToolName, firstNonEmpty(cr.Output, cr.Stdout, cr.Stderr))
		}
	} else {
		toolName := tc.CapabilityTools[capabilities.InitProject]
		if toolName == "" {
			toolName = capabilities.InitProject
		}
		if _, ok := e.opts.ToolRegistry.Get(toolName); !ok {
			return fmt.Errorf("toolchain bootstrap tool %q not registered", toolName)
		}
		res := e.opts.ToolRegistry.Call(ctx, toolName, args)
		if res.Error != "" {
			return fmt.Errorf("toolchain bootstrap via %s: %s", toolName, res.Error)
		}
		passed, output, errMsg := parseToolchainResult(res.Output)
		if errMsg != "" {
			return fmt.Errorf("toolchain bootstrap via %s: %s", toolName, errMsg)
		}
		if !passed {
			return fmt.Errorf("toolchain bootstrap via %s reported failure: %s", toolName, output)
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("toolchain bootstrap completed but %q is still missing: %w", manifestPath, err)
	}
	ws.AllSuggestions = append(ws.AllSuggestions, fmt.Sprintf("[bootstrap] initialized workspace via %s", capabilities.InitProject))
	return e.store.SaveWorkflow(ctx, ws)
}

func toolchainSupportsCapability(tc ToolchainConfig, capability string) bool {
	if strings.TrimSpace(tc.CapabilityTools[capability]) != "" {
		return true
	}
	for _, candidate := range tc.Capabilities {
		if strings.EqualFold(strings.TrimSpace(candidate), capability) {
			return true
		}
	}
	return false
}

func bootstrapManifestPath(workspacePath string, tc ToolchainConfig, selected *state.ToolchainSelection) (string, bool) {
	if strings.TrimSpace(workspacePath) == "" {
		return "", false
	}
	if strings.EqualFold(tc.ID, "go") {
		return filepath.Join(workspacePath, "go.mod"), true
	}
	if selected != nil && strings.EqualFold(selected.Language, "go") {
		return filepath.Join(workspacePath, "go.mod"), true
	}
	for _, lang := range tc.Languages {
		if strings.EqualFold(strings.TrimSpace(lang), "go") || strings.EqualFold(strings.TrimSpace(lang), "golang") {
			return filepath.Join(workspacePath, "go.mod"), true
		}
	}
	return "", false
}

func (e *Engine) toolchainArgs(ws *state.WorkflowState, tc ToolchainConfig, phase, capability string, push bool) json.RawMessage {
	args := map[string]any{
		"workflow_id":  ws.ID,
		"phase":        phase,
		"capability":   capability,
		"toolchain_id": tc.ID,
		"push":         push,
	}
	if ws.Execution.Workspace != nil {
		args["workspace_path"] = ws.ID
		args["repo_url"] = ws.Execution.Workspace.RepoURL
		args["branch"] = ws.Execution.Workspace.Branch
	}
	b, _ := json.Marshal(args)
	return b
}

func parseToolchainResult(raw json.RawMessage) (bool, string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return true, "", ""
	}
	var obj struct {
		Success *bool  `json:"success"`
		Passed  *bool  `json:"passed"`
		Output  string `json:"output"`
		Stdout  string `json:"stdout"`
		Stderr  string `json:"stderr"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		passed := true
		if obj.Success != nil {
			passed = *obj.Success
		}
		if obj.Passed != nil {
			passed = *obj.Passed
		}
		output := trimValidationOutput(firstNonEmpty(obj.Output, obj.Stdout, obj.Message))
		if obj.Stderr != "" && output != "" {
			output += "\n" + obj.Stderr
		} else if obj.Stderr != "" {
			output = obj.Stderr
		}
		if obj.Error != "" {
			passed = false
		}
		return passed, trimValidationOutput(output), obj.Error
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return true, trimValidationOutput(text), ""
	}
	return true, trimValidationOutput(string(raw)), ""
}

func parseCheckpointResult(raw json.RawMessage) (commitSHA, branch, message string, pushed bool) {
	var obj struct {
		CommitSHA string `json:"commit_sha"`
		SHA       string `json:"sha"`
		Branch    string `json:"branch"`
		Message   string `json:"message"`
		Pushed    bool   `json:"pushed"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return firstNonEmpty(obj.CommitSHA, obj.SHA), obj.Branch, obj.Message, obj.Pushed
	}
	return "", "", trimValidationOutput(string(raw)), false
}

// emitMCPCapabilityMissing records a workflow event when registry resolution
// fails for (toolchain, capability).  Best-effort; storage errors are logged
// by the underlying store and not returned.
func (e *Engine) emitMCPCapabilityMissing(ctx context.Context, ws *state.WorkflowState, tc ToolchainConfig, capability, phase, reason string) {
	evt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventMCPCapabilityMissing, "",
		events.MCPCapabilityMissingPayload{
			ToolchainID: tc.ID,
			Capability:  capability,
			MCPServer:   tc.MCPServer,
			Reason:      reason,
			Phase:       phase,
		})
	if err != nil {
		return
	}
	_ = e.store.AppendEvents(ctx, evt)
}

// emitMCPToolEvent records mcp.tool.invoked or mcp.tool.failed depending on
// whether the call passed.
func (e *Engine) emitMCPToolEvent(ctx context.Context, ws *state.WorkflowState, tc ToolchainConfig, capability, tool, phase string, passed bool, errMsg string, dur time.Duration) {
	if passed {
		evt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventMCPToolInvoked, "",
			events.MCPToolInvokedPayload{
				ToolchainID: tc.ID,
				Capability:  capability,
				Tool:        tool,
				MCPServer:   tc.MCPServer,
				Phase:       phase,
				Passed:      true,
				DurationMS:  dur.Milliseconds(),
			})
		if err == nil {
			_ = e.store.AppendEvents(ctx, evt)
		}
		return
	}
	evt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventMCPToolFailed, "",
		events.MCPToolFailedPayload{
			ToolchainID: tc.ID,
			Capability:  capability,
			Tool:        tool,
			MCPServer:   tc.MCPServer,
			Phase:       phase,
			Error:       errMsg,
		})
	if err == nil {
		_ = e.store.AppendEvents(ctx, evt)
	}
}

// lastValidationFailed reports whether the most recent validation run on the
// workflow failed.  Returns (false, nil) when there are no validation runs
// (no toolchain configured, or pod never reached) so that workflows
// without toolchains are not blocked by the gate.
func lastValidationFailed(ws *state.WorkflowState) (bool, *state.ValidationRun) {
	if ws == nil || len(ws.Execution.ValidationRuns) == 0 {
		return false, nil
	}
	last := &ws.Execution.ValidationRuns[len(ws.Execution.ValidationRuns)-1]
	return !last.Passed, last
}

func workspaceBranch(ws *state.WorkflowState) string {
	if ws.Execution.Workspace == nil {
		return ""
	}
	return ws.Execution.Workspace.Branch
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func trimValidationOutput(s string) string {
	s = strings.TrimSpace(s)
	const max = 4000
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[... truncated: %d bytes omitted ...]", len(s)-max)
}

// ─── State helpers ────────────────────────────────────────────────────────────

// applyOutput merges a PersonaOutput into the WorkflowState.
// Role contracts are enforced here:
//   - Only Pod may append Artifacts (other personas use task-level path).
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
	// Only Pod may append Artifacts via applyOutput; other personas
	// that legitimately produce artifacts (e.g. Refiner) use direct ws.Artifacts
	// writes in their own runner paths.  QA must not create artifacts.
	if len(out.Artifacts) > 0 {
		if out.Persona == state.PersonaPod {
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
	e.appendReviewThreadEntries(ws, out, out.Summary)

	// Update live execution progress after every persona phase.
	ws.Execution.CurrentPersona = out.Persona

	// Director sets provider/model.
	if out.Persona == state.PersonaDirector {
		explicitMode := ws.Mode != ""
		explicitProvider := ws.ProviderName != ""
		explicitModel := ws.ModelName != ""

		// Parse the raw JSON output from the Director to extract its decisions.
		directorPacket := e.buildPacket(ws, state.PersonaDirector, nil)
		dirOut := director.OutputFromRaw(out.RawContent, directorPacket)

		provider := directorPacket.ProviderName
		if !explicitProvider {
			provider = e.normalizeProviderSelection(dirOut.Provider, directorPacket.ProviderName, ws.ProviderCatalogs)
		}
		ws.ProviderName = provider

		fallbackModel := e.providerFallbackModel(provider, ws.ProviderCatalogs)
		if explicitModel {
			ws.ModelName = e.normalizeModelSelection(provider, ws.ModelName, fallbackModel, ws.ProviderCatalogs)
		} else {
			ws.ModelName = e.normalizeModelSelection(provider, dirOut.Model, fallbackModel, ws.ProviderCatalogs)
		}
		ws.PersonaModels = e.normalizePersonaModels(provider, dirOut.PersonaModels, ws.ModelName, ws.ProviderCatalogs)

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

func (e *Engine) appendReviewThreadEntries(ws *state.WorkflowState, out *state.PersonaOutput, summary string) {
	if ws == nil || out == nil {
		return
	}
	timestamp := out.CompletedAt
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	if msg := strings.TrimSpace(summary); msg != "" {
		appendReviewThreadEntry(ws, state.ReviewThreadEntry{
			Persona:            out.Persona,
			Kind:               "summary",
			Message:            msg,
			QACycle:            ws.Execution.QACycle,
			RemediationAttempt: ws.Execution.RemediationAttempt,
			CreatedAt:          timestamp,
		})
	}
	for _, issue := range out.BlockingIssues {
		if msg := strings.TrimSpace(issue); msg != "" {
			appendReviewThreadEntry(ws, state.ReviewThreadEntry{
				Persona:            out.Persona,
				Kind:               "blocking_issue",
				Message:            msg,
				QACycle:            ws.Execution.QACycle,
				RemediationAttempt: ws.Execution.RemediationAttempt,
				CreatedAt:          timestamp,
			})
		}
	}
	if out.Persona != state.PersonaMatriarch {
		return
	}
	for _, suggestion := range out.Suggestions {
		msg := strings.TrimSpace(suggestion)
		kind := ""
		switch {
		case strings.HasPrefix(msg, "[matriarch][decision] "):
			kind = "decision"
			msg = strings.TrimPrefix(msg, "[matriarch][decision] ")
		case strings.HasPrefix(msg, "[matriarch][escalate] "):
			kind = "question"
			msg = strings.TrimPrefix(msg, "[matriarch][escalate] ")
		}
		if kind == "" || strings.TrimSpace(msg) == "" {
			continue
		}
		appendReviewThreadEntry(ws, state.ReviewThreadEntry{
			Persona:            out.Persona,
			Kind:               kind,
			Message:            strings.TrimSpace(msg),
			QACycle:            ws.Execution.QACycle,
			RemediationAttempt: ws.Execution.RemediationAttempt,
			CreatedAt:          timestamp,
		})
	}
}

func appendReviewThreadEntry(ws *state.WorkflowState, entry state.ReviewThreadEntry) {
	if ws == nil || strings.TrimSpace(entry.Message) == "" {
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	ws.ReviewThread = append(ws.ReviewThread, entry)
}

// buildPacket constructs the HandoffPacket for the given persona from the
// current WorkflowState snapshot.
func (e *Engine) buildPacket(ws *state.WorkflowState, kind state.PersonaKind, snap *customization.Snapshot) state.HandoffPacket {
	provider := e.resolveProviderName(ws)
	model := e.resolvePersonaModel(ws, kind, provider)
	artifacts := ws.Artifacts
	if kind == state.PersonaQA {
		artifacts = latestArtifactsByLogicalFile(ws.Artifacts)
	}

	packet := state.HandoffPacket{
		WorkflowID:                 ws.ID,
		TenantID:                   ws.TenantID,
		ScopeID:                    ws.ScopeID,
		Mode:                       ws.Mode,
		Request:                    ws.Request,
		Constitution:               ws.Constitution,
		Requirements:               ws.Requirements,
		Design:                     ws.Design,
		Tasks:                      ws.Tasks,
		Artifacts:                  artifacts,
		Summaries:                  ws.Summaries,
		ReviewThread:               ws.ReviewThread,
		CurrentPersona:             kind,
		ProviderName:               provider,
		ModelName:                  model,
		ProviderCatalogs:           ws.ProviderCatalogs,
		PersonaModels:              ws.PersonaModels,
		BlockingIssues:             ws.BlockingIssues,
		AllSuggestions:             ws.AllSuggestions,
		ImprovementsPath:           e.opts.ImprovementsRoot,
		PersonaPromptSnapshot:      ws.PersonaPromptSnapshot,
		FinalizerAction:            ws.FinalizerAction,
		DeliveryAction:             ws.DeliveryAction,
		DeliveryConfig:             ws.DeliveryConfig,
		QACycle:                    ws.Execution.QACycle,
		RemediationAttempt:         ws.Execution.RemediationAttempt,
		InputDocuments:             ws.InputDocuments,
		InputDocumentCorpusSummary: ws.InputDocumentCorpusSummary,
		Workspace:                  ws.Execution.Workspace,
		Toolchain:                  ws.Execution.Toolchain,
		ValidationRuns:             ws.Execution.ValidationRuns,
		Checkpoints:                ws.Execution.Checkpoints,
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

	packet.EmitPersonaStarted = func(ctx context.Context, personaKind state.PersonaKind, providerName, modelName string) {
		evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaStarted, personaKind,
			events.PersonaStartedPayload{
				Persona:      personaKind,
				ProviderName: providerName,
				ModelName:    modelName,
			})
		_ = e.store.AppendEvents(ctx, evt)
		ws.Execution.CurrentPersona = personaKind
		_ = e.store.SaveWorkflow(ctx, ws)
	}

	packet.EmitPersonaCompleted = func(ctx context.Context, personaKind state.PersonaKind, durationMs int64, summary string, blockingIssues []string) {
		evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaCompleted, personaKind,
			events.PersonaCompletedPayload{
				Persona:        personaKind,
				DurationMs:     durationMs,
				Summary:        summary,
				BlockingIssues: blockingIssues,
			})
		_ = e.store.AppendEvents(ctx, evt)
		ws.Execution.CurrentPersona = kind
		_ = e.store.SaveWorkflow(ctx, ws)
	}

	packet.EmitPersonaFailed = func(ctx context.Context, personaKind state.PersonaKind, errMsg string) {
		evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaFailed, personaKind,
			events.PersonaFailedPayload{Persona: personaKind, Error: errMsg})
		_ = e.store.AppendEvents(ctx, evt)
		ws.Execution.CurrentPersona = kind
		_ = e.store.SaveWorkflow(ctx, ws)
	}

	return packet
}

func latestArtifactsByLogicalFile(artifacts []state.Artifact) []state.Artifact {
	if len(artifacts) < 2 {
		return artifacts
	}

	order := make([]string, 0, len(artifacts))
	latest := make(map[string]state.Artifact, len(artifacts))
	for _, art := range artifacts {
		key := logicalArtifactKey(art)
		if _, exists := latest[key]; !exists {
			order = append(order, key)
		}
		latest[key] = art
	}

	out := make([]state.Artifact, 0, len(latest))
	for _, key := range order {
		out = append(out, latest[key])
	}
	return out
}

func logicalArtifactKey(art state.Artifact) string {
	if path := strings.TrimSpace(art.Path); path != "" {
		return "path:" + path
	}
	return "name:" + strings.TrimSpace(art.Name) + "|kind:" + string(art.Kind)
}

// isContentSynthesisKind returns true for artifact kinds that represent a
// single evolving document in content-mode workflows.
func isContentSynthesisKind(kind state.ArtifactKind) bool {
	switch kind {
	case state.ArtifactKindBlogPost, state.ArtifactKindMarkdown, state.ArtifactKindDocument:
		return true
	}
	return false
}

// mergeOrAppendArtifact handles content-mode artifact accumulation.
//
// In content-mode workflows the pod executes multiple tasks (e.g.
// "write intro", "write body", "write conclusion") that all contribute to a
// single evolving document.  Rather than storing each task's output as a
// separate artifact, we find the existing synthesis artifact and **replace**
// its content with the new (cumulative) output.  The pod prompt
// already injects the latest document so the model appends to / patches it.
//
// For non-content modes, or non-synthesis artifact kinds, the artifact is
// appended as before.
func mergeOrAppendArtifact(mode state.WorkflowMode, artifacts []state.Artifact, art state.Artifact) []state.Artifact {
	if mode != state.WorkflowModeContent || !isContentSynthesisKind(art.Kind) {
		return append(artifacts, art)
	}

	// Look for an existing synthesis artifact to update in-place.
	for i := range artifacts {
		if isContentSynthesisKind(artifacts[i].Kind) {
			artifacts[i].Content = art.Content
			artifacts[i].TaskID = art.TaskID
			artifacts[i].CreatedAt = art.CreatedAt
			if art.Name != "" {
				artifacts[i].Name = art.Name
			}
			if art.Description != "" {
				artifacts[i].Description = art.Description
			}
			return artifacts
		}
	}

	// No existing synthesis artifact — first task, just append.
	return append(artifacts, art)
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
