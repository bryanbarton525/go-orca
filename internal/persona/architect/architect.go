// Package architect implements the Architect persona, responsible for producing
// a design document and a task graph for the Implementer.
package architect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/google/uuid"
)

// archOutput is the expected JSON shape from the Architect.
type archOutput struct {
	Design  state.Design `json:"design"`
	Tasks   []taskSpec   `json:"tasks"`
	Summary string       `json:"summary"`
}

// defaultSpecialtyForMode returns the canonical pod specialty for a workflow
// mode.  This is the Director-side hint surfaced to the Architect — for
// software workflows default to backend, content/docs to writer, ops to ops.
// Returns "" for mixed/unknown modes so the Architect picks per-task without
// a blanket default.
func defaultSpecialtyForMode(mode state.WorkflowMode) string {
	switch mode {
	case state.WorkflowModeSoftware:
		return "backend"
	case state.WorkflowModeContent, state.WorkflowModeDocs, state.WorkflowModeResearch:
		return "writer"
	case state.WorkflowModeOps:
		return "ops"
	default:
		return ""
	}
}

// taskSpec is the Architect's task description before IDs are assigned.
type taskSpec struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	DependsOn   []string          `json:"depends_on"`
	AssignedTo  state.PersonaKind `json:"assigned_to"`
	// Specialty selects which pod specialist runs this task: backend,
	// frontend, writer, ops, or data (or their orca-themed aliases).  An
	// empty string falls back to the generic pod prompt.
	Specialty string `json:"specialty,omitempty"`
}

// outputSchema defines the structured JSON shape for Architect responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"design":  map[string]any{"type": "object"},
		"tasks":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"summary": map[string]any{"type": "string"},
	},
	"required": []string{"design", "tasks", "summary"},
}

// Architect implements persona.Persona.
type Architect struct {
	exec base.Executor
}

// New returns a new Architect persona.
func New() *Architect {
	return &Architect{exec: base.NewExecutor("architect_output", outputSchema)}
}

// Kind implements Persona.
func (a *Architect) Kind() state.PersonaKind { return state.PersonaArchitect }

// Name implements Persona.
func (a *Architect) Name() string { return "Architect" }

// Description implements Persona.
func (a *Architect) Description() string {
	return "Designs the solution and breaks it into a concrete task graph with explicit dependencies."
}

// Execute implements Persona.
func (a *Architect) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyArchitect]

	ctx_ := base.BuildHandoffContext(packet)
	specialtyHint := defaultSpecialtyForMode(packet.Mode)
	hintBlock := ""
	if specialtyHint != "" {
		hintBlock = fmt.Sprintf(
			"\n\n## Pod Specialty Hint (from Director's mode)\nWorkflow mode is %q. The default specialty for this mode is %q. "+
				"Use it for tasks that don't clearly belong to a different specialist. Mixed workflows commonly produce tasks "+
				"with different specialties — pick per-task, don't blanket-apply.\n",
			packet.Mode, specialtyHint,
		)
	}
	userPrompt := fmt.Sprintf(
		"%s%s\n\nProduce the Design and Task graph JSON for this workflow.",
		ctx_, hintBlock,
	)

	raw, err := a.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("architect: execution error: %w", err)
	}

	var out archOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("architect: parse error: %w", err)
	}

	now := base.Timestamp()
	tasks := make([]state.Task, 0, len(out.Tasks))
	var specialtyWarnings []string
	for _, ts := range out.Tasks {
		// Normalise assigned_to to lowercase so that model responses with
		// "Implementer" (capital I) or other case variants are treated the same
		// as the canonical "pod" value.  Without this, tasks are silently
		// skipped in runImplementerPhase and ws.Artifacts stays empty.
		assigned := state.PersonaKind(strings.ToLower(strings.TrimSpace(string(ts.AssignedTo))))
		if assigned == "" {
			assigned = state.PersonaPod
		}
		// Validate specialty: an unknown value would silently fall back to the
		// generic pod prompt, which is fine — but we surface it as a warning so
		// the operator can see when the model invented a non-existent
		// specialist (e.g. "qa", "designer") and either teach it the right
		// value or add a new overlay file.
		specialty := strings.TrimSpace(ts.Specialty)
		if specialty != "" && prompts.KeyForPodSpecialty(specialty) == "" {
			specialtyWarnings = append(specialtyWarnings,
				fmt.Sprintf("[architect][specialty] task %q used unknown specialty %q — falling back to generic pod (valid: backend, frontend, writer, ops, data)",
					ts.Title, specialty))
			specialty = "" // clear so the runtime path is unambiguous
		}
		tasks = append(tasks, state.Task{
			ID:          uuid.New().String(),
			WorkflowID:  packet.WorkflowID,
			Title:       ts.Title,
			Description: ts.Description,
			Status:      state.TaskStatusPending,
			DependsOn:   ts.DependsOn,
			AssignedTo:  assigned,
			Specialty:   specialty,
			CreatedAt:   now,
		})
	}

	// Resolve depends_on entries from task titles to task IDs. The model
	// cannot know the UUIDs assigned above, so it naturally uses titles.
	// Build a title→ID index and rewrite any entry that matches a title.
	titleToID := make(map[string]string, len(tasks))
	for _, t := range tasks {
		titleToID[strings.ToLower(strings.TrimSpace(t.Title))] = t.ID
	}
	for i := range tasks {
		resolved := make([]string, 0, len(tasks[i].DependsOn))
		for _, dep := range tasks[i].DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if id, ok := titleToID[strings.ToLower(dep)]; ok {
				resolved = append(resolved, id)
			} else {
				// Already an ID or unresolvable — keep as-is.
				resolved = append(resolved, dep)
			}
		}
		tasks[i].DependsOn = resolved
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaArchitect,
		Summary:     out.Summary,
		RawContent:  raw,
		Design:      &out.Design,
		Tasks:       tasks,
		Suggestions: specialtyWarnings,
		CompletedAt: now,
	}, nil
}
