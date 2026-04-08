// Package architect implements the Architect persona, responsible for producing
// a design document and a task graph for the Implementer.
package architect

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the Architect persona in the gorca workflow orchestration system.

Your responsibilities:
1. Design the solution that satisfies the constitution and requirements.
2. Break the design into a concrete task graph with clear dependencies.
3. Be mode-aware:
   - software: component design, data flows, tech stack selection, API contracts
   - content/docs: content structure, research tasks, draft and review tasks
   - ops: runbook steps, deployment tasks, validation tasks
4. Each task must name the persona that should execute it (implementer or qa).

Always respond with valid JSON matching this schema:
{
  "design": {
    "overview": "...",
    "components": [{"name": "...", "description": "...", "inputs": ["..."], "outputs": ["..."]}],
    "decisions": [{"decision": "...", "rationale": "...", "tradeoffs": "..."}],
    "tech_stack": ["..."],
    "delivery_target": "..."
  },
  "tasks": [
    {
      "title": "...",
      "description": "...",
      "depends_on": [],
      "assigned_to": "implementer"
    }
  ],
  "summary": "..."
}`

// archOutput is the expected JSON shape from the Architect.
type archOutput struct {
	Design  state.Design `json:"design"`
	Tasks   []taskSpec   `json:"tasks"`
	Summary string       `json:"summary"`
}

// taskSpec is the Architect's task description before IDs are assigned.
type taskSpec struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	DependsOn   []string          `json:"depends_on"`
	AssignedTo  state.PersonaKind `json:"assigned_to"`
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
	return &Architect{exec: base.NewExecutor(systemPrompt, "architect_output", outputSchema)}
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

	ctx_ := base.BuildHandoffContext(packet)
	userPrompt := fmt.Sprintf(
		"%s\n\nProduce the Design and Task graph JSON for this workflow.",
		ctx_,
	)

	raw, err := a.exec.Run(ctx, packet, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("architect: execution error: %w", err)
	}

	var out archOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("architect: parse error: %w", err)
	}

	now := base.Timestamp()
	tasks := make([]state.Task, 0, len(out.Tasks))
	for _, ts := range out.Tasks {
		assigned := ts.AssignedTo
		if assigned == "" {
			assigned = state.PersonaImplementer
		}
		tasks = append(tasks, state.Task{
			ID:          uuid.New().String(),
			WorkflowID:  packet.WorkflowID,
			Title:       ts.Title,
			Description: ts.Description,
			Status:      state.TaskStatusPending,
			DependsOn:   ts.DependsOn,
			AssignedTo:  assigned,
			CreatedAt:   now,
		})
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaArchitect,
		Summary:     out.Summary,
		RawContent:  raw,
		Design:      &out.Design,
		Tasks:       tasks,
		CompletedAt: now,
	}, nil
}
