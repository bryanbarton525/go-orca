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
	userPrompt := fmt.Sprintf(
		"%s\n\nProduce the Design and Task graph JSON for this workflow.",
		ctx_,
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
	for _, ts := range out.Tasks {
		// Normalise assigned_to to lowercase so that model responses with
		// "Implementer" (capital I) or other case variants are treated the same
		// as the canonical "pod" value.  Without this, tasks are silently
		// skipped in runImplementerPhase and ws.Artifacts stays empty.
		assigned := state.PersonaKind(strings.ToLower(strings.TrimSpace(string(ts.AssignedTo))))
		if assigned == "" {
			assigned = state.PersonaPod
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
