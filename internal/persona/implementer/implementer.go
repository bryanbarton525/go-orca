// Package implementer implements the Implementer persona, responsible for
// executing individual tasks from the task graph and producing artifacts.
package implementer

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/google/uuid"
)

// implOutput is the expected JSON shape from the Implementer.
type implOutput struct {
	ArtifactKind        string   `json:"artifact_kind"`
	ArtifactName        string   `json:"artifact_name"`
	ArtifactDescription string   `json:"artifact_description"`
	Content             string   `json:"content"`
	Summary             string   `json:"summary"`
	Issues              []string `json:"issues"`
}

// outputSchema defines the structured JSON shape for Implementer responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"artifact_kind":        map[string]any{"type": "string"},
		"artifact_name":        map[string]any{"type": "string"},
		"artifact_description": map[string]any{"type": "string"},
		"content":              map[string]any{"type": "string"},
		"summary":              map[string]any{"type": "string"},
		"issues":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"artifact_kind", "artifact_name", "artifact_description", "content", "summary", "issues"},
}

// Implementer implements persona.Persona.
type Implementer struct {
	exec base.Executor
}

// New returns a new Implementer persona.
func New() *Implementer {
	return &Implementer{exec: base.NewExecutor("implementer_output", outputSchema)}
}

// Kind implements Persona.
func (im *Implementer) Kind() state.PersonaKind { return state.PersonaImplementer }

// Name implements Persona.
func (im *Implementer) Name() string { return "Implementer" }

// Description implements Persona.
func (im *Implementer) Description() string {
	return "Executes tasks from the task graph and produces typed artifacts."
}

// Execute implements Persona.
//
// The Implementer runs once per ready task.  The engine is responsible for
// calling Execute repeatedly until all implementer tasks are complete.
// The HandoffPacket.Tasks slice should contain the single task being executed.
func (im *Implementer) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	if len(packet.Tasks) == 0 {
		return nil, fmt.Errorf("implementer: no task in handoff packet")
	}
	task := packet.Tasks[0]

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyImplementer]

	ctx_ := base.BuildHandoffContext(packet)
	userPrompt := fmt.Sprintf(
		"%s\n\n## Current Task\nTitle: %s\nDescription: %s\n\nImplement this task and produce your JSON output.",
		ctx_, task.Title, task.Description,
	)

	raw, err := im.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("implementer: execution error: %w", err)
	}

	var out implOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("implementer: parse error: %w", err)
	}

	now := base.Timestamp()
	artifact := state.Artifact{
		ID:          uuid.New().String(),
		WorkflowID:  packet.WorkflowID,
		TaskID:      task.ID,
		Kind:        state.ArtifactKind(out.ArtifactKind),
		Name:        out.ArtifactName,
		Description: out.ArtifactDescription,
		Content:     out.Content,
		CreatedBy:   state.PersonaImplementer,
		CreatedAt:   now,
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaImplementer,
		Summary:     out.Summary,
		RawContent:  raw,
		Artifacts:   []state.Artifact{artifact},
		Suggestions: out.Issues,
		CompletedAt: now,
	}, nil
}
