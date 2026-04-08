// Package state defines the canonical workflow state model, task graph,
// HandoffPacket, and typed persona output structures.
package state

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Enums ───────────────────────────────────────────────────────────────────

// WorkflowStatus represents the lifecycle state of a workflow run.
type WorkflowStatus string

const (
	WorkflowStatusPending   WorkflowStatus = "pending"
	WorkflowStatusRunning   WorkflowStatus = "running"
	WorkflowStatusPaused    WorkflowStatus = "paused"
	WorkflowStatusCompleted WorkflowStatus = "completed"
	WorkflowStatusFailed    WorkflowStatus = "failed"
	WorkflowStatusCancelled WorkflowStatus = "cancelled"
)

// WorkflowMode classifies the type of work being performed.
type WorkflowMode string

const (
	WorkflowModeSoftware WorkflowMode = "software"
	WorkflowModeContent  WorkflowMode = "content"
	WorkflowModeDocs     WorkflowMode = "docs"
	WorkflowModeResearch WorkflowMode = "research"
	WorkflowModeOps      WorkflowMode = "ops"
	WorkflowModeMixed    WorkflowMode = "mixed"
)

// TaskStatus represents the lifecycle state of a single task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusSkipped   TaskStatus = "skipped"
	TaskStatusBlocked   TaskStatus = "blocked"
)

// PersonaKind names the built-in persona roles.
type PersonaKind string

const (
	PersonaDirector    PersonaKind = "director"
	PersonaProjectMgr  PersonaKind = "project_manager"
	PersonaArchitect   PersonaKind = "architect"
	PersonaImplementer PersonaKind = "implementer"
	PersonaQA          PersonaKind = "qa"
	PersonaFinalizer   PersonaKind = "finalizer"
	PersonaRefiner     PersonaKind = "refiner"
)

// ArtifactKind classifies a produced artifact.
type ArtifactKind string

const (
	ArtifactKindCode      ArtifactKind = "code"
	ArtifactKindDocument  ArtifactKind = "document"
	ArtifactKindDiagram   ArtifactKind = "diagram"
	ArtifactKindMarkdown  ArtifactKind = "markdown"
	ArtifactKindConfig    ArtifactKind = "config"
	ArtifactKindReport    ArtifactKind = "report"
	ArtifactKindBlogPost  ArtifactKind = "blog_post"
	ArtifactKindBundleRef ArtifactKind = "bundle_ref" // reference to exported bundle
)

// ─── Core models ─────────────────────────────────────────────────────────────

// Execution holds the live execution progress for a running workflow.
// It is overwritten by the engine at every persona/task transition and
// persisted so that GET /workflows/:id reflects current in-flight state.
type Execution struct {
	// CurrentPersona is the persona phase currently executing.
	CurrentPersona PersonaKind `json:"current_persona,omitempty"`
	// ActiveTaskID is the ID of the task being executed by the Implementer.
	ActiveTaskID string `json:"active_task_id,omitempty"`
	// ActiveTaskTitle is the title of that task for display purposes.
	ActiveTaskTitle string `json:"active_task_title,omitempty"`
	// QACycle is the current QA/remediation pass number (1-based).
	QACycle int `json:"qa_cycle,omitempty"`
	// RemediationAttempt is the Implementer re-run count within the current QA cycle.
	RemediationAttempt int `json:"remediation_attempt,omitempty"`
}

// WorkflowState is the canonical, persisted state of a single workflow run.
type WorkflowState struct {
	ID       string         `json:"id"`
	TenantID string         `json:"tenant_id"`
	ScopeID  string         `json:"scope_id"`
	Status   WorkflowStatus `json:"status"`
	Mode     WorkflowMode   `json:"mode"`
	Title    string         `json:"title"`

	// Original user request.
	Request string `json:"request"`

	// Structured outputs from each persona phase.
	Constitution *Constitution       `json:"constitution,omitempty"`
	Requirements *Requirements       `json:"requirements,omitempty"`
	Design       *Design             `json:"design,omitempty"`
	Tasks        []Task              `json:"tasks,omitempty"`
	Artifacts    []Artifact          `json:"artifacts,omitempty"`
	Finalization *FinalizationResult `json:"finalization,omitempty"`

	// Per-persona summaries for handoff context.
	Summaries map[PersonaKind]string `json:"summaries,omitempty"`

	// Provider/model selected by Director.
	ProviderName string `json:"provider_name,omitempty"`
	ModelName    string `json:"model_name,omitempty"`

	// Blocking issues raised during QA.
	BlockingIssues []string `json:"blocking_issues,omitempty"`

	// Suggestions accumulated across all persona phases (from Refiner/QA).
	AllSuggestions []string `json:"all_suggestions,omitempty"`

	// PersonaPromptSnapshot holds the content of each persona prompt file as it
	// was at the moment this workflow was first started.  It is persisted so
	// that resume, retry, and replay use identical prompt text regardless of
	// subsequent edits to the files on disk.
	PersonaPromptSnapshot map[string]string `json:"persona_prompt_snapshot,omitempty"`

	// RequiredPersonas is the set of pipeline phases selected by the Director.
	// When non-empty it is used as a filter over the fixed phase order; phases
	// not in this list are skipped.  Director itself is always mandatory.
	RequiredPersonas []PersonaKind `json:"required_personas,omitempty"`

	// FinalizerAction is the delivery action chosen by the Director.
	// The Finalizer honors this value in code after parsing its LLM response
	// so the action cannot drift from the Director's intent.
	FinalizerAction string `json:"finalizer_action,omitempty"`

	// DeliveryAction is the action key selected at workflow creation time
	// (from the POST /workflows request body delivery.action field).
	// When set it overrides FinalizerAction, giving callers full control over
	// how the Finalizer delivers its output.
	DeliveryAction string `json:"delivery_action,omitempty"`

	// DeliveryConfig is the action-specific non-secret configuration submitted
	// with the workflow (e.g. target repo, base branch, webhook URL).
	// Secrets (tokens, passwords) must come from environment variables — never
	// persisted here.  Stored as raw JSON so each action can define its own shape.
	DeliveryConfig json.RawMessage `json:"delivery_config,omitempty"`

	// Execution holds live progress metadata for in-flight workflows.
	// Updated by the engine at every persona/task boundary.
	Execution Execution `json:"execution,omitempty"`

	// Execution metadata.
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

// NewWorkflowState constructs an empty WorkflowState with a generated ID.
func NewWorkflowState(tenantID, scopeID, request string) *WorkflowState {
	now := time.Now().UTC()
	return &WorkflowState{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		ScopeID:   scopeID,
		Status:    WorkflowStatusPending,
		Request:   request,
		Summaries: make(map[PersonaKind]string),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Constitution is the PM's foundational document for a workflow.
type Constitution struct {
	Vision             string   `json:"vision"`
	Goals              []string `json:"goals"`
	Constraints        []string `json:"constraints"`
	Audience           string   `json:"audience"`
	OutputMedium       string   `json:"output_medium"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	OutOfScope         []string `json:"out_of_scope"`
}

// Requirements is the PM's structured requirements list.
type Requirements struct {
	Functional    []Requirement `json:"functional"`
	NonFunctional []Requirement `json:"non_functional"`
	Dependencies  []string      `json:"dependencies"`
}

// Requirement is a single requirement entry.
type Requirement struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // must | should | could | wont
	Source      string `json:"source"`
}

// Design is the Architect's design artifact.
type Design struct {
	Overview       string            `json:"overview"`
	Components     []DesignComponent `json:"components"`
	Decisions      []DesignDecision  `json:"decisions"`
	Diagrams       []string          `json:"diagrams,omitempty"`
	TechStack      []string          `json:"tech_stack"`
	DeliveryTarget string            `json:"delivery_target"`
}

// DesignComponent describes a logical component in the design.
type DesignComponent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Inputs      []string `json:"inputs"`
	Outputs     []string `json:"outputs"`
}

// DesignDecision records an architectural decision with rationale.
type DesignDecision struct {
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
	Tradeoffs string `json:"tradeoffs,omitempty"`
}

// Task is a unit of work in the Architect's task graph.
type Task struct {
	ID          string      `json:"id"`
	WorkflowID  string      `json:"workflow_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Status      TaskStatus  `json:"status"`
	DependsOn   []string    `json:"depends_on,omitempty"` // Task IDs
	AssignedTo  PersonaKind `json:"assigned_to"`
	Output      string      `json:"output,omitempty"`
	// Attempt is the QA/remediation cycle that created this task (0 = initial).
	Attempt int `json:"attempt,omitempty"`
	// RemediationSource is "qa_remediation" when the task was created by the
	// Architect during a post-QA remediation pass.
	RemediationSource string     `json:"remediation_source,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// Artifact is a produced output from any persona.
type Artifact struct {
	ID          string       `json:"id"`
	WorkflowID  string       `json:"workflow_id"`
	TaskID      string       `json:"task_id,omitempty"`
	Kind        ArtifactKind `json:"kind"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Path        string       `json:"path,omitempty"`    // on-disk path
	Content     string       `json:"content,omitempty"` // inline for small artifacts
	CreatedBy   PersonaKind  `json:"created_by"`
	CreatedAt   time.Time    `json:"created_at"`
}

// RefinerImprovement is a single structured improvement proposed by the Refiner.
type RefinerImprovement struct {
	ComponentType string `json:"component_type"` // agent | skill | prompt | persona
	ComponentName string `json:"component_name"`
	Problem       string `json:"problem"`
	ProposedFix   string `json:"proposed_fix"`
	// Content is the verbatim file content to persist (e.g. a SKILL.md or
	// .prompt.md body). Empty means the improvement is advisory only.
	Content  string `json:"content,omitempty"`
	Priority string `json:"priority"` // high | medium | low
}

// FinalizationResult holds the Finalizer's output.
type FinalizationResult struct {
	Action      string            `json:"action"` // github-pr | markdown-export | etc.
	Summary     string            `json:"summary"`
	Links       []string          `json:"links,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Suggestions []string          `json:"suggestions,omitempty"` // from Refiner pass
	// RefinerImprovements holds the structured set of improvements from the
	// inline Refiner retrospective.  The engine persists these as files under
	// ImprovementsRoot so future workflow runs can incorporate them.
	RefinerImprovements []RefinerImprovement `json:"refiner_improvements,omitempty"`
	CompletedAt         time.Time            `json:"completed_at"`
}

// ─── HandoffPacket ────────────────────────────────────────────────────────────

// HandoffPacket is the context packet passed to each persona at execution time.
// It is built from the persisted WorkflowState and contains everything the
// persona needs to make informed decisions without accessing global state.
type HandoffPacket struct {
	WorkflowID string       `json:"workflow_id"`
	TenantID   string       `json:"tenant_id"`
	ScopeID    string       `json:"scope_id"`
	Mode       WorkflowMode `json:"mode"`
	Request    string       `json:"request"`

	// Phase inputs from earlier personas.
	Constitution *Constitution `json:"constitution,omitempty"`
	Requirements *Requirements `json:"requirements,omitempty"`
	Design       *Design       `json:"design,omitempty"`
	Tasks        []Task        `json:"tasks,omitempty"`
	Artifacts    []Artifact    `json:"artifacts,omitempty"`

	// Compressed summaries from earlier phases.
	Summaries map[PersonaKind]string `json:"summaries,omitempty"`

	// Active context.
	CurrentPersona PersonaKind `json:"current_persona"`
	ProviderName   string      `json:"provider_name"`
	ModelName      string      `json:"model_name"`

	// Snapshot of resolved customizations for this run.
	CustomAgentMD  string `json:"custom_agent_md,omitempty"` // loaded .agent.md
	SkillsContext  string `json:"skills_context,omitempty"`  // loaded SKILL.md content
	PromptsContext string `json:"prompts_context,omitempty"` // loaded .prompt.md

	// PersonaPromptSnapshot is the workflow-start snapshot of all base persona
	// prompt file contents.  Each persona reads its system prompt from here so
	// prompt changes on disk do not affect in-flight workflows.
	PersonaPromptSnapshot map[string]string `json:"persona_prompt_snapshot,omitempty"`

	// FinalizerAction is the delivery action chosen by the Director, forwarded
	// to the Finalizer so it can be enforced in code rather than inferred by
	// the LLM.  Empty means no Director preference was set.
	FinalizerAction string `json:"finalizer_action,omitempty"`

	// DeliveryAction is the caller-supplied delivery action (from the
	// POST /workflows request body), forwarded to the Finalizer so it can be
	// executed with the caller's config.  Overrides FinalizerAction when set.
	DeliveryAction string `json:"delivery_action,omitempty"`

	// DeliveryConfig is the caller-supplied action configuration (non-secret
	// fields only; tokens come from env vars).  The Finalizer passes this to
	// actions.Global.Execute so the action has full context at runtime.
	DeliveryConfig json.RawMessage `json:"delivery_config,omitempty"`

	// Accumulated issues and suggestions from prior phases (populated by engine).
	BlockingIssues []string `json:"blocking_issues,omitempty"`
	AllSuggestions []string `json:"all_suggestions,omitempty"`

	// ImprovementsPath is the directory where the Refiner may write improvement
	// files (SKILL.md, .prompt.md, .agent.md).  Set by the engine from Options.
	ImprovementsPath string `json:"improvements_path,omitempty"`

	// QACycle and RemediationAttempt are populated during the QA remediation
	// loop so persona prompts can reference the current pass number.
	QACycle            int `json:"qa_cycle,omitempty"`
	RemediationAttempt int `json:"remediation_attempt,omitempty"`
	// IsRemediation signals to the Architect that this invocation is a
	// targeted remediation pass (not the initial planning phase).
	IsRemediation bool `json:"is_remediation,omitempty"`
}

// ─── Persona output ───────────────────────────────────────────────────────────

// PersonaOutput is the typed result returned by any persona execution.
type PersonaOutput struct {
	Persona        PersonaKind `json:"persona"`
	Summary        string      `json:"summary"`
	RawContent     string      `json:"raw_content"`
	BlockingIssues []string    `json:"blocking_issues,omitempty"`
	Suggestions    []string    `json:"suggestions,omitempty"`

	// Typed phase outputs; only one should be set per persona.
	Constitution *Constitution       `json:"constitution,omitempty"`
	Requirements *Requirements       `json:"requirements,omitempty"`
	Design       *Design             `json:"design,omitempty"`
	Tasks        []Task              `json:"tasks,omitempty"`
	Artifacts    []Artifact          `json:"artifacts,omitempty"`
	Finalization *FinalizationResult `json:"finalization,omitempty"`

	CompletedAt time.Time `json:"completed_at"`
}

// ─── Scope / Tenant references ───────────────────────────────────────────────

// ScopeKind distinguishes the three allowed scope types.
type ScopeKind string

const (
	ScopeKindGlobal ScopeKind = "global"
	ScopeKindOrg    ScopeKind = "org"
	ScopeKindTeam   ScopeKind = "team"
)

// Tenant is the top-level isolation boundary.
type Tenant struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Scope is the configuration/customization boundary within a tenant.
type Scope struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Kind          ScopeKind `json:"kind"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	ParentScopeID string    `json:"parent_scope_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
