// Package pm implements the Project Manager persona, responsible for producing
// the workflow Constitution and structured Requirements.
package pm

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

// ProjectManager implements persona.Persona.
type ProjectManager struct {
	exec base.Executor
}

// New returns a new ProjectManager persona.
func New() *ProjectManager {
	return &ProjectManager{exec: base.NewExecutor("pm_output", outputSchema)}
}

// Kind implements Persona.
func (p *ProjectManager) Kind() state.PersonaKind { return state.PersonaProjectMgr }

// Name implements Persona.
func (p *ProjectManager) Name() string { return "Project Manager" }

// Description implements Persona.
func (p *ProjectManager) Description() string {
	return "Defines the project constitution and structured requirements that all subsequent personas must respect."
}

// pmOutput is the expected JSON shape from the PM.
type pmOutput struct {
	Constitution state.Constitution `json:"constitution"`
	Requirements state.Requirements `json:"requirements"`
	Summary      string             `json:"summary"`
}

// outputSchema defines the structured JSON shape for ProjectManager responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"constitution": map[string]any{"type": "object"},
		"requirements": map[string]any{"type": "object"},
		"summary":      map[string]any{"type": "string"},
	},
	"required": []string{"constitution", "requirements", "summary"},
}

// Execute implements Persona.
func (p *ProjectManager) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyProjectManager]

	ctx_ := base.BuildHandoffContext(packet)
	instruction := "Produce the Constitution and Requirements JSON for this workflow."
	if packet.IsRemediation {
		instruction = "This is a QA remediation triage pass. Review the blocking issues and classify whether they are requirement gaps, design gaps, implementation defects, or validation/environment failures. Keep the original acceptance baseline intact unless a requirement is genuinely missing. In the summary, provide a concise remediation brief for the Architect."
	}
	userPrompt := fmt.Sprintf("%s\n\n%s", ctx_, instruction)

	raw, err := p.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("pm: execution error: %w", err)
	}

	var out pmOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("pm: parse error: %w", err)
	}

	return &state.PersonaOutput{
		Persona:      state.PersonaProjectMgr,
		Summary:      out.Summary,
		RawContent:   raw,
		Constitution: &out.Constitution,
		Requirements: &out.Requirements,
		CompletedAt:  base.Timestamp(),
	}, nil
}
