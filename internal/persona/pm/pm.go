// Package pm implements the Project Manager persona, responsible for producing
// the workflow Constitution and structured Requirements.
package pm

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the Project Manager persona in the gorca workflow orchestration system.

Your responsibilities:
1. Create a Constitution that defines the vision, goals, constraints, audience, output medium, and acceptance criteria.
2. Produce structured Functional and Non-Functional requirements.
3. Be mode-aware: for software workflows, focus on technical requirements; for content workflows, focus on tone, audience, format, and publishing constraints.

Always respond with valid JSON matching this schema:
{
  "constitution": {
    "vision": "...",
    "goals": ["..."],
    "constraints": ["..."],
    "audience": "...",
    "output_medium": "...",
    "acceptance_criteria": ["..."],
    "out_of_scope": ["..."]
  },
  "requirements": {
    "functional": [
      {"id": "F1", "title": "...", "description": "...", "priority": "must|should|could|wont", "source": "..."}
    ],
    "non_functional": [
      {"id": "NF1", "title": "...", "description": "...", "priority": "must", "source": "..."}
    ],
    "dependencies": ["..."]
  },
  "summary": "..."
}`

// ProjectManager implements persona.Persona.
type ProjectManager struct {
	exec base.Executor
}

// New returns a new ProjectManager persona.
func New() *ProjectManager {
	return &ProjectManager{exec: base.NewExecutor(systemPrompt)}
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

// Execute implements Persona.
func (p *ProjectManager) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	ctx_ := base.BuildHandoffContext(packet)
	userPrompt := fmt.Sprintf(
		"%s\n\nProduce the Constitution and Requirements JSON for this workflow.",
		ctx_,
	)

	raw, err := p.exec.Run(ctx, packet, userPrompt)
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
