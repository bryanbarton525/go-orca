// Package engineer implements the Matriarch persona, a pragmatic stand-in
// for the user's engineering preferences during design planning.
package engineer

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

type output struct {
	Decisions []string `json:"decisions"`
	Questions []string `json:"questions"`
	Summary   string   `json:"summary"`
}

var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"decisions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"summary":   map[string]any{"type": "string"},
	},
	"required": []string{"decisions", "questions", "summary"},
}

// EngineerProxy implements persona.Persona.
type Matriarch struct {
	exec base.Executor
}

// New returns a new Matriarch persona.
func New() *Matriarch {
	return &Matriarch{exec: base.NewExecutor("matriarch_output", outputSchema)}
}

func (e *Matriarch) Kind() state.PersonaKind { return state.PersonaMatriarch }
func (e *Matriarch) Name() string            { return "Matriarch" }
func (e *Matriarch) Description() string {
	return "Captures pragmatic engineering defaults, questions shaky decisions, and re-enters remediation to challenge Architect and QA tradeoffs."
}

func (e *Matriarch) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyMatriarch]
	handoffCtx := base.BuildHandoffContext(packet)
	instruction := "Act as the user's pragmatic matriarch. Continue the review thread by challenging weak assumptions, supplying concrete design defaults for the Architect, and escalating product-sensitive questions that need a real user answer."
	if packet.IsRemediation {
		instruction = "Act as the user's pragmatic matriarch during remediation. Review the QA blockers, the current review thread, and the Architect's direction; question weak remediation decisions, suggest safer defaults, and escalate any missing context that blocks a sound fix."
	}
	userPrompt := fmt.Sprintf(
		"%s\n\n%s",
		handoffCtx, instruction,
	)

	raw, err := e.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("matriarch: execution error: %w", err)
	}

	var out output
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("matriarch: parse error: %w", err)
	}

	suggestions := make([]string, 0, len(out.Decisions)+len(out.Questions))
	for _, d := range out.Decisions {
		if strings.TrimSpace(d) != "" {
			suggestions = append(suggestions, "[matriarch][decision] "+strings.TrimSpace(d))
		}
	}
	for _, q := range out.Questions {
		if strings.TrimSpace(q) != "" {
			suggestions = append(suggestions, "[matriarch][escalate] "+strings.TrimSpace(q))
		}
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaMatriarch,
		Summary:     out.Summary,
		RawContent:  raw,
		Suggestions: suggestions,
		CompletedAt: base.Timestamp(),
	}, nil
}
