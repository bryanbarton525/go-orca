// Package state defines the canonical workflow state model, task graph,
// HandoffPacket, and typed persona output structures.
package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-orca/go-orca/internal/tools"
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

// DownstreamPersonaKinds returns the personas whose models are selected by the
// Director after its own bootstrap execution has completed.
func DownstreamPersonaKinds() []PersonaKind {
	return []PersonaKind{
		PersonaProjectMgr,
		PersonaArchitect,
		PersonaImplementer,
		PersonaQA,
		PersonaFinalizer,
	}
}

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

// PersonaModelAssignments stores the resolved model per persona.
type PersonaModelAssignments map[PersonaKind]string

// ProviderModelInfo is a provider-advertised model that was allowed by policy
// at workflow start.
type ProviderModelInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ProviderModelCatalog is the filtered model catalog captured for one provider
// at workflow start so retries/resume do not depend on live provider state.
type ProviderModelCatalog struct {
	ProviderName   string              `json:"provider_name"`
	DefaultModel   string              `json:"default_model,omitempty"`
	Models         []ProviderModelInfo `json:"models,omitempty"`
	Degraded       bool                `json:"degraded,omitempty"`
	DiscoveryError string              `json:"discovery_error,omitempty"`
}

// ─── Core models ─────────────────────────────────────────────────────────────

// Execution holds the live execution progress for a running workflow.
// It is overwritten by the engine at every persona/task transition and
// persisted so that GET /api/v1/workflows/{id} reflects current in-flight state.
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
	// WorkflowKind distinguishes standard workflow runs from improvement workflows
	// spawned by the self-improvement pipeline.
	// Values: "standard" | "improvement"
	WorkflowKind string `json:"workflow_kind,omitempty"`
	// ParentWorkflowID is set on improvement child workflows and points to the
	// workflow whose Refiner produced this improvement.
	ParentWorkflowID string `json:"parent_workflow_id,omitempty"`
	// ImprovementDepth is 0 for standard workflows and 1 for improvement child
	// workflows.  The recursion guard prevents the dispatcher from spawning
	// further improvement workflows when this value is >= 1.
	ImprovementDepth int `json:"improvement_depth,omitempty"`
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

	// ProviderCatalogs is the filtered provider model inventory snapshot shown
	// to the Director at workflow start.
	ProviderCatalogs map[string]ProviderModelCatalog `json:"provider_catalogs,omitempty"`

	// PersonaModels stores the Director-selected downstream model per persona.
	PersonaModels PersonaModelAssignments `json:"persona_models,omitempty"`

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
	// (from the POST /api/v1/workflows request body delivery.action field).
	// When set it overrides FinalizerAction, giving callers full control over
	// how the Finalizer delivers its output.
	DeliveryAction string `json:"delivery_action,omitempty"`

	// DeliveryConfig is the action-specific non-secret configuration submitted
	// with the workflow (e.g. target repo, base branch, webhook URL).
	// Secrets (tokens, passwords) must come from environment variables — never
	// persisted here.  Stored as raw JSON so each action can define its own shape.
	DeliveryConfig json.RawMessage `json:"delivery_config,omitempty"`

	// InputDocuments is the compact per-document manifest built by the
	// pre-Director attachment ingestion stage.  It is immutable once set and
	// included in every HandoffPacket so all personas see the same view.
	InputDocuments []InputDocument `json:"input_documents,omitempty"`

	// InputDocumentCorpusSummary is a single LLM-generated summary of all
	// input documents, produced by the ingestion stage.
	InputDocumentCorpusSummary string `json:"input_document_corpus_summary,omitempty"`

	// AttachmentProcessing tracks aggregate ingestion progress.
	// Nil when no attachments were submitted.
	AttachmentProcessing *AttachmentProcessing `json:"attachment_processing,omitempty"`

	// UploadSessionID is set when the workflow was created with staged uploads.
	UploadSessionID string `json:"upload_session_id,omitempty"`

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
// WorkflowKind defaults to "standard"; child improvement workflows override
// this to "improvement" in workflow_launcher.go.
func NewWorkflowState(tenantID, scopeID, request string) *WorkflowState {
	now := time.Now().UTC()
	return &WorkflowState{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		ScopeID:   scopeID,
		Status:    WorkflowStatusPending,
		Request:   request,
		Summaries: make(map[PersonaKind]string),
		Execution: Execution{WorkflowKind: "standard"},
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

// ImprovementFile is a single file within a multi-file improvement bundle.
type ImprovementFile struct {
	// Path is the relative file path within the improvements active/ directory.
	// e.g. "skills/my-skill/SKILL.md" or "agents/my-agent.agent.md"
	Path string `json:"path"`
	// Content is the verbatim file content.
	Content string `json:"content"`
}

// RefinerImprovement is a single structured improvement proposed by the Refiner.
type RefinerImprovement struct {
	ComponentType string `json:"component_type"` // agent | skill | prompt | persona
	ComponentName string `json:"component_name"`
	Problem       string `json:"problem"`
	ProposedFix   string `json:"proposed_fix"`
	// ChangeType classifies whether this is a new component (create), update to
	// an existing one (update), or advisory-only with no files (advisory).
	ChangeType string `json:"change_type,omitempty"` // create | update | advisory
	// ApplyMode hints at how the improvement should be applied.
	// "direct"   → write straight to artifacts/improvements/active/ (low-risk only)
	// "workflow" → open a GitHub PR via a child improvement workflow
	// "advisory" → emit event only, no files written
	ApplyMode string `json:"apply_mode,omitempty"` // direct | workflow | advisory
	// Files is the list of file path+content pairs for multi-file improvements
	// (e.g. a full Anthropic skill package with SKILL.md + references/).
	// When populated the dispatcher uses this slice; it falls back to Content
	// for single-file backward-compatible improvements.
	Files []ImprovementFile `json:"files,omitempty"`
	// Content is the verbatim file content to persist (legacy single-file field).
	// Empty means the improvement is advisory only.
	Content  string `json:"content,omitempty"`
	Priority string `json:"priority"` // high | medium | low
}

// ImprovementApplyResult records the outcome of applying a single RefinerImprovement.
type ImprovementApplyResult struct {
	ComponentType string `json:"component_type"`
	ComponentName string `json:"component_name"`
	// Status is one of: applied | dispatched | skipped | error
	Status          string `json:"status"`
	AppliedPath     string `json:"applied_path,omitempty"`
	ChildWorkflowID string `json:"child_workflow_id,omitempty"`
	Message         string `json:"message,omitempty"`
}

// FinalizationResult holds the Finalizer's output.
type FinalizationResult struct {
	Action      string         `json:"action"` // github-pr | markdown-export | etc.
	Summary     string         `json:"summary"`
	Links       []string       `json:"links,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Suggestions []string       `json:"suggestions,omitempty"` // from Refiner pass
	// RefinerImprovements holds the structured set of improvements from the
	// inline Refiner retrospective.  The improvement dispatcher processes these
	// after the Finalizer completes, routing them to direct-apply or a child PR
	// workflow based on the routing policy.
	RefinerImprovements []RefinerImprovement `json:"refiner_improvements,omitempty"`
	// ImprovementResults records the outcome of each improvement apply attempt
	// as returned by the ImprovementDispatcher.
	ImprovementResults []ImprovementApplyResult `json:"improvement_results,omitempty"`
	CompletedAt        time.Time                `json:"completed_at"`
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
	CurrentPersona   PersonaKind                     `json:"current_persona"`
	ProviderName     string                          `json:"provider_name"`
	ModelName        string                          `json:"model_name"`
	ProviderCatalogs map[string]ProviderModelCatalog `json:"provider_catalogs,omitempty"`
	PersonaModels    PersonaModelAssignments         `json:"persona_models,omitempty"`

	// Snapshot of resolved customizations for this run.
	CustomAgentMD  string `json:"custom_agent_md,omitempty"` // loaded .agent.md
	SkillsContext  string `json:"skills_context,omitempty"`  // loaded SKILL.md content
	PromptsContext string `json:"prompts_context,omitempty"` // loaded .prompt.md

	// InputDocuments is the compact per-document manifest from ingestion.
	InputDocuments []InputDocument `json:"input_documents,omitempty"`
	// InputDocumentCorpusSummary is the merged summary of all input documents.
	InputDocumentCorpusSummary string `json:"input_document_corpus_summary,omitempty"`

	// ToolsContext is a formatted markdown description of available tools,
	// injected into every persona's system prompt.  Populated by the engine
	// from the global tool registry at workflow start.
	ToolsContext string `json:"tools_context,omitempty"`

	// PersonaPromptSnapshot is the workflow-start snapshot of all base persona
	// prompt file contents.  Each persona reads its system prompt from here so
	// prompt changes on disk do not affect in-flight workflows.
	PersonaPromptSnapshot map[string]string `json:"persona_prompt_snapshot,omitempty"`

	// ToolRegistry is the live tool registry available for this workflow run.
	// It is set by the engine at packet-build time and is intentionally
	// excluded from JSON serialisation (HandoffPacket is never persisted as-is).
	ToolRegistry *tools.Registry `json:"-"`
	// Nested persona event hooks are runtime-only callbacks used by personas
	// that execute internal sub-phases, such as the Finalizer's inline Refiner
	// pass. They allow those sub-phases to surface progress in the workflow
	// journal without coupling persona packages to the storage layer.
	EmitPersonaStarted   func(ctx context.Context, persona PersonaKind, providerName, modelName string)                            `json:"-"`
	EmitPersonaCompleted func(ctx context.Context, persona PersonaKind, durationMs int64, summary string, blockingIssues []string) `json:"-"`
	EmitPersonaFailed    func(ctx context.Context, persona PersonaKind, err string)                                                `json:"-"`

	// FinalizerAction is the delivery action chosen by the Director, forwarded
	// to the Finalizer so it can be enforced in code rather than inferred by
	// the LLM.  Empty means no Director preference was set.
	FinalizerAction string `json:"finalizer_action,omitempty"`

	// DeliveryAction is the caller-supplied delivery action (from the
	// POST /api/v1/workflows request body), forwarded to the Finalizer so it can be
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

// ─── Attachment ingestion ─────────────────────────────────────────────────────

// UploadSessionStatus represents the lifecycle of a staged upload session.
type UploadSessionStatus string

const (
	UploadSessionOpen     UploadSessionStatus = "open"
	UploadSessionConsumed UploadSessionStatus = "consumed"
	UploadSessionExpired  UploadSessionStatus = "expired"
	UploadSessionAborted  UploadSessionStatus = "aborted"
)

// UploadSession is a staged upload session created before workflow submission.
type UploadSession struct {
	ID         string              `json:"id"`
	TenantID   string              `json:"tenant_id"`
	ScopeID    string              `json:"scope_id"`
	Status     UploadSessionStatus `json:"status"`
	WorkflowID string              `json:"workflow_id,omitempty"` // set when consumed
	ExpiresAt  time.Time           `json:"expires_at"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

// AttachmentStatus represents the processing state of a single attachment.
type AttachmentStatus string

const (
	AttachmentPending       AttachmentStatus = "pending"
	AttachmentStatusRunning AttachmentStatus = "processing"
	AttachmentCompleted     AttachmentStatus = "completed"
	AttachmentFailed        AttachmentStatus = "failed"
)

// Attachment is a user-uploaded file associated with a workflow.
type Attachment struct {
	ID              string           `json:"id"`
	UploadSessionID string           `json:"upload_session_id"`
	WorkflowID      string           `json:"workflow_id,omitempty"`
	TenantID        string           `json:"tenant_id"`
	ScopeID         string           `json:"scope_id"`
	Filename        string           `json:"filename"`
	RelativePath    string           `json:"relative_path,omitempty"`
	ContentType     string           `json:"content_type"`
	SizeBytes       int64            `json:"size_bytes"`
	StoragePath     string           `json:"storage_path"`
	Status          AttachmentStatus `json:"status"`
	Summary         string           `json:"summary,omitempty"`
	ChunkCount      int              `json:"chunk_count,omitempty"`
	ErrorMessage    string           `json:"error_message,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// AttachmentChunk is a segment of a chunked attachment for retrieval.
type AttachmentChunk struct {
	ID           string `json:"id"`
	AttachmentID string `json:"attachment_id"`
	WorkflowID   string `json:"workflow_id"`
	Index        int    `json:"index"`
	Content      string `json:"content"`
}

// AttachmentProcessingStatus is the workflow-level attachment ingestion state.
type AttachmentProcessingStatus string

const (
	AttachmentProcessingPending   AttachmentProcessingStatus = "pending"
	AttachmentProcessingRunning   AttachmentProcessingStatus = "running"
	AttachmentProcessingCompleted AttachmentProcessingStatus = "completed"
	AttachmentProcessingFailed    AttachmentProcessingStatus = "failed"
	AttachmentProcessingCancelled AttachmentProcessingStatus = "cancelled"
)

// AttachmentProcessing tracks aggregate ingestion progress on a workflow.
type AttachmentProcessing struct {
	Status       AttachmentProcessingStatus `json:"status"`
	TotalCount   int                        `json:"total_count"`
	DoneCount    int                        `json:"done_count"`
	FailedCount  int                        `json:"failed_count"`
	ErrorMessage string                     `json:"error_message,omitempty"`
	StartedAt    *time.Time                 `json:"started_at,omitempty"`
	CompletedAt  *time.Time                 `json:"completed_at,omitempty"`
}

// InputDocument is the compact per-document metadata persisted on the workflow
// for inclusion in handoff packets.
type InputDocument struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path,omitempty"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Summary      string `json:"summary"`
	ChunkCount   int    `json:"chunk_count"`
}
