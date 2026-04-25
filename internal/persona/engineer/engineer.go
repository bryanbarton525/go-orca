// Package engineer implements the Engineer Proxy persona, a pragmatic stand-in
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
type EngineerProxy struct {
	exec base.Executor
}

// New returns a new Engineer Proxy persona.
func New() *EngineerProxy {
	return &EngineerProxy{exec: base.NewExecutor("engineer_proxy_output", outputSchema)}
}

func (e *EngineerProxy) Kind() state.PersonaKind { return state.PersonaEngineer }
func (e *EngineerProxy) Name() string            { return "Engineer Proxy" }
func (e *EngineerProxy) Description() string {
	return "Captures pragmatic engineering defaults and design tradeoff guidance before architecture planning."
}

func (e *EngineerProxy) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyEngineerProxy]
	handoffCtx := base.BuildHandoffContext(packet)
	userPrompt := fmt.Sprintf(
		"%s\n\nAct as the user's pragmatic engineer proxy. Identify concrete design defaults the Architect should apply, plus any decisions that are too product-sensitive and should be escalated.",
		handoffCtx,
	)

	raw, err := e.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("engineer proxy: execution error: %w", err)
	}

	var out output
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("engineer proxy: parse error: %w", err)
	}

	suggestions := make([]string, 0, len(out.Decisions)+len(out.Questions))
	for _, d := range out.Decisions {
		if strings.TrimSpace(d) != "" {
			suggestions = append(suggestions, "[engineer_proxy][decision] "+strings.TrimSpace(d))
		}
	}
	for _, q := range out.Questions {
		if strings.TrimSpace(q) != "" {
			suggestions = append(suggestions, "[engineer_proxy][escalate] "+strings.TrimSpace(q))
		}
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaEngineer,
		Summary:     out.Summary,
		RawContent:  raw,
		Suggestions: suggestions,
		CompletedAt: base.Timestamp(),
	}, nil
}
