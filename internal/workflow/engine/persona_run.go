package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

// PersonaRunRequest is the input for a one-shot persona execution (API / MCP bridge).
type PersonaRunRequest struct {
	TenantID      string
	ScopeID       string
	Persona       state.PersonaKind
	Request       string
	Mode          state.WorkflowMode
	Provider      string
	Model         string
	WorkflowID    string
	Persist       bool
	ToolsScope    string
	Timeout       time.Duration
	WorkspacePath string

	Constitution *state.Constitution
	Requirements *state.Requirements
	Design       *state.Design
	Tasks        []state.Task
	Artifacts    []state.Artifact

	TaskRun *PersonaTaskRunInput
}

// PersonaTaskRunInput describes a single task for orca_task_run style calls.
type PersonaTaskRunInput struct {
	Title       string
	Description string
	Specialty   string
	Tier        state.TaskTier
}

// PersonaRunResponse is returned after a successful one-shot persona run.
type PersonaRunResponse struct {
	WorkflowID string               `json:"workflow_id"`
	Persona    state.PersonaKind    `json:"persona"`
	Provider   string               `json:"provider_name"`
	Model      string               `json:"model_name"`
	Output     *state.PersonaOutput `json:"output"`
	Events     []*events.Event      `json:"events,omitempty"`
	DurationMs int64                `json:"duration_ms"`
}

// RunPersonaOnce executes a single persona without running the full pipeline.
func (e *Engine) RunPersonaOnce(ctx context.Context, req PersonaRunRequest) (*PersonaRunResponse, error) {
	kind := state.PersonaKind(strings.ToLower(strings.TrimSpace(string(req.Persona))))
	if kind == "" {
		return nil, fmt.Errorf("persona is required")
	}
	if strings.TrimSpace(req.Request) == "" && req.TaskRun == nil {
		return nil, fmt.Errorf("request or task_run is required")
	}
	if _, ok := persona.Get(kind); !ok {
		return nil, fmt.Errorf("persona %q not registered", kind)
	}
	if req.TenantID == "" || req.ScopeID == "" {
		return nil, fmt.Errorf("tenant_id and scope_id are required")
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = e.opts.HandoffTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ws, err := e.buildPersonaRunWorkflow(runCtx, req, kind)
	if err != nil {
		return nil, err
	}

	if err := e.ensurePersonaRunPrompts(runCtx, ws); err != nil {
		return nil, err
	}
	if err := e.ensureProviderCatalogs(runCtx, ws); err != nil {
		return nil, fmt.Errorf("provider catalogs: %w", err)
	}

	if req.Provider != "" {
		ws.ProviderName = strings.TrimSpace(req.Provider)
	}
	if req.Model != "" {
		ws.ModelName = strings.TrimSpace(req.Model)
	}

	snap := e.personaRunCustomizationSnapshot(runCtx, ws)

	start := time.Now()
	var runErr error
	if req.TaskRun != nil && (kind == state.PersonaPod || kind == state.PersonaQA) {
		runErr = e.runPersonaTaskOnce(runCtx, ws, kind, snap, req.TaskRun)
	} else {
		runErr = e.runPersona(runCtx, ws, kind, snap)
	}
	if runErr != nil {
		if !req.Persist {
			_ = e.transition(runCtx, ws, state.WorkflowStatusCancelled)
		}
		return nil, runErr
	}

	provider := e.resolveProviderName(ws)
	model := e.resolvePersonaModel(ws, kind, provider)
	out := synthesizePersonaRunOutput(ws, kind)

	var evts []*events.Event

	if !req.Persist {
		_ = e.transition(runCtx, ws, state.WorkflowStatusCancelled)
	}

	return &PersonaRunResponse{
		WorkflowID: ws.ID,
		Persona:    kind,
		Provider:   provider,
		Model:      model,
		Output:     out,
		Events:     evts,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func (e *Engine) buildPersonaRunWorkflow(ctx context.Context, req PersonaRunRequest, kind state.PersonaKind) (*state.WorkflowState, error) {
	if id := strings.TrimSpace(req.WorkflowID); id != "" {
		ws, err := e.store.GetWorkflow(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load workflow: %w", err)
		}
		if ws.TenantID != req.TenantID {
			return nil, fmt.Errorf("workflow does not belong to tenant")
		}
		if strings.TrimSpace(req.Request) != "" {
			ws.Request = req.Request
		}
		if scope := strings.TrimSpace(req.ToolsScope); scope != "" {
			ws.Execution.PersonaRunToolsScope = scope
		}
		return ws, nil
	}

	request := strings.TrimSpace(req.Request)
	if request == "" && req.TaskRun != nil {
		request = strings.TrimSpace(req.TaskRun.Description)
		if request == "" {
			request = strings.TrimSpace(req.TaskRun.Title)
		}
	}

	ws := state.NewWorkflowState(req.TenantID, req.ScopeID, request)
	ws.Status = state.WorkflowStatusRunning
	if req.Mode != "" {
		ws.Mode = req.Mode
	}
	if req.Constitution != nil {
		ws.Constitution = req.Constitution
	}
	if req.Requirements != nil {
		ws.Requirements = req.Requirements
	}
	if req.Design != nil {
		ws.Design = req.Design
	}
	if len(req.Tasks) > 0 {
		ws.Tasks = append([]state.Task(nil), req.Tasks...)
	}
	if len(req.Artifacts) > 0 {
		ws.Artifacts = append([]state.Artifact(nil), req.Artifacts...)
	}
	if req.WorkspacePath != "" {
		ws.Execution.Workspace = &state.WorkspaceInfo{Path: req.WorkspacePath}
	}
	if scope := strings.TrimSpace(req.ToolsScope); scope != "" {
		ws.Execution.PersonaRunToolsScope = scope
	}

	if req.TaskRun != nil {
		tier := req.TaskRun.Tier
		if tier == "" {
			tier = state.TaskTierLight
		}
		ws.Tasks = []state.Task{{
			ID:          uuid.NewString(),
			Title:       strings.TrimSpace(req.TaskRun.Title),
			Description: strings.TrimSpace(req.TaskRun.Description),
			Specialty:   strings.TrimSpace(req.TaskRun.Specialty),
			Tier:        tier,
			AssignedTo:  kind,
			Status:      state.TaskStatusPending,
		}}
	}

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return nil, fmt.Errorf("create workflow: %w", err)
	}
	return ws, nil
}

func (e *Engine) ensurePersonaRunPrompts(ctx context.Context, ws *state.WorkflowState) error {
	if len(ws.PersonaPromptSnapshot) > 0 {
		return nil
	}
	promptRoot := e.opts.PersonaPromptRoot
	if promptRoot == "" {
		promptRoot = prompts.DefaultRoot
	}
	snapshot, err := prompts.Load(promptRoot)
	if err != nil {
		return fmt.Errorf("load persona prompts: %w", err)
	}
	ws.PersonaPromptSnapshot = snapshot
	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) personaRunCustomizationSnapshot(ctx context.Context, ws *state.WorkflowState) *customization.Snapshot {
	if e.opts.CustomizationRegistry == nil {
		return nil
	}
	scopeSlug := ""
	if e.opts.ScopeResolver != nil && ws.ScopeID != "" {
		slugs := e.opts.ScopeResolver.ScopeSlugsForID(ctx, ws.ScopeID)
		if len(slugs) > 0 {
			scopeSlug = slugs[0]
		}
	}
	snap, err := e.opts.CustomizationRegistry.Snapshot(scopeSlug)
	if err != nil {
		return nil
	}
	return snap
}

func (e *Engine) runPersonaTaskOnce(ctx context.Context, ws *state.WorkflowState, kind state.PersonaKind, snap *customization.Snapshot, task *PersonaTaskRunInput) error {
	if len(ws.Tasks) == 0 {
		tier := task.Tier
		if tier == "" {
			tier = state.TaskTierLight
		}
		ws.Tasks = []state.Task{{
			ID:          uuid.NewString(),
			Title:       strings.TrimSpace(task.Title),
			Description: strings.TrimSpace(task.Description),
			Specialty:   strings.TrimSpace(task.Specialty),
			Tier:        tier,
			AssignedTo:  kind,
			Status:      state.TaskStatusPending,
		}}
		_ = e.store.SaveWorkflow(ctx, ws)
	}

	p, ok := persona.Get(kind)
	if !ok {
		return fmt.Errorf("persona %q not registered", kind)
	}

	if kind == state.PersonaPod {
		return e.executePodTask(ctx, ws, snap, p, e.buildPacket(ws, kind, snap), 0, nil)
	}

	return e.runPersona(ctx, ws, kind, snap)
}

func synthesizePersonaRunOutput(ws *state.WorkflowState, kind state.PersonaKind) *state.PersonaOutput {
	if ws == nil {
		return nil
	}
	out := &state.PersonaOutput{Persona: kind}
	if ws.Summaries != nil {
		out.Summary = ws.Summaries[kind]
	}
	out.Constitution = ws.Constitution
	out.Requirements = ws.Requirements
	out.Design = ws.Design
	if len(ws.Tasks) > 0 {
		out.Tasks = append([]state.Task(nil), ws.Tasks...)
	}
	if len(ws.Artifacts) > 0 {
		out.Artifacts = append([]state.Artifact(nil), ws.Artifacts...)
	}
	out.Finalization = ws.Finalization
	out.BlockingIssues = append([]string(nil), ws.BlockingIssues...)
	return out
}
