// Package refiner implements the Refiner persona as a standalone async persona.
// In most workflows it runs as a synchronous retrospective pass embedded inside
// the Finalizer.  This package provides the standalone Refiner for cases where
// it is promoted to a true background persona (e.g. tenant-level continuous
// improvement loops over historical workflow events).
package refiner

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the Refiner persona in the gorca workflow orchestration system.

You perform retrospective analysis over one or more completed workflows to surface
systemic improvement opportunities for agents, skills, prompts, and persona behavior.

Your responsibilities:
1. Analyze workflow history: summaries, blocking issues, suggestions, and artifact quality.
2. Identify recurring patterns of failure or inefficiency across phases.
3. Propose concrete, actionable improvements — not vague observations.
4. Reference exact component names (persona kind, skill name, agent file, prompt file).
5. Prioritize improvements by impact.

Improvement component types: agent | skill | prompt | persona | workflow | provider

Always respond with valid JSON matching this schema:
{
  "improvements": [
    {
      "component_type": "agent|skill|prompt|persona|workflow|provider",
      "component_name": "...",
      "problem": "...",
      "proposed_fix": "...",
      "example": "...",
      "priority": "high|medium|low"
    }
  ],
  "overall_assessment": "...",
  "health_score": 0-100,
  "summary": "..."
}`

// Improvement is a single actionable improvement proposal from the Refiner.
type Improvement struct {
	ComponentType string `json:"component_type"`
	ComponentName string `json:"component_name"`
	Problem       string `json:"problem"`
	ProposedFix   string `json:"proposed_fix"`
	Example       string `json:"example,omitempty"`
	Priority      string `json:"priority"`
}

// Output is the full structured output from a Refiner run.
type Output struct {
	Improvements      []Improvement `json:"improvements"`
	OverallAssessment string        `json:"overall_assessment"`
	HealthScore       int           `json:"health_score"`
	Summary           string        `json:"summary"`
}

// refinerOutput mirrors Output for JSON parsing.
type refinerOutput = Output

// outputSchema defines the structured output shape for standalone Refiner runs.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"improvements": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"component_type": map[string]any{"type": "string"},
					"component_name": map[string]any{"type": "string"},
					"problem":        map[string]any{"type": "string"},
					"proposed_fix":   map[string]any{"type": "string"},
					"example":        map[string]any{"type": "string"},
					"priority":       map[string]any{"type": "string"},
				},
			},
		},
		"overall_assessment": map[string]any{"type": "string"},
		"health_score":       map[string]any{"type": "integer"},
		"summary":            map[string]any{"type": "string"},
	},
	"required": []string{"improvements", "overall_assessment", "health_score", "summary"},
}

// Refiner implements persona.Persona.
type Refiner struct {
	exec base.Executor
}

// New returns a new Refiner persona.
func New() *Refiner {
	return &Refiner{exec: base.NewExecutor(systemPrompt, "refiner_output", outputSchema)}
}

// Kind implements Persona.
func (r *Refiner) Kind() state.PersonaKind { return state.PersonaRefiner }

// Name implements Persona.
func (r *Refiner) Name() string { return "Refiner" }

// Description implements Persona.
func (r *Refiner) Description() string {
	return "Retrospective persona that identifies systemic improvements across workflows."
}

// Execute implements Persona.
//
// When running as a standalone async persona, the HandoffPacket should include
// a concatenated history of workflow events/summaries in AllSuggestions and
// BlockingIssues, aggregated by the scheduler/engine over recent history.
func (r *Refiner) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	handoffCtx := base.BuildHandoffContext(packet)

	userPrompt := fmt.Sprintf(
		`%s

## Accumulated Blocking Issues
%s

## Accumulated Suggestions From All Phases
%s

Analyze the above workflow history and produce your retrospective improvement JSON.`,
		handoffCtx,
		formatList(packet.BlockingIssues),
		formatList(packet.AllSuggestions),
	)

	raw, err := r.exec.Run(ctx, packet, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("refiner: execution error: %w", err)
	}

	var out refinerOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("refiner: parse error: %w", err)
	}

	suggestions := make([]string, 0, len(out.Improvements))
	for _, imp := range out.Improvements {
		suggestions = append(suggestions, fmt.Sprintf(
			"[refiner][%s][%s/%s] %s → %s",
			imp.Priority, imp.ComponentType, imp.ComponentName,
			imp.Problem, imp.ProposedFix,
		))
	}

	now := base.Timestamp()
	return &state.PersonaOutput{
		Persona:     state.PersonaRefiner,
		Summary:     out.Summary,
		RawContent:  raw,
		Suggestions: suggestions,
		CompletedAt: now,
	}, nil
}

// ParseOutput is a convenience helper for callers that want the typed Output
// (e.g. the engine writing RefinerSuggestion events to the journal).
func ParseOutput(raw string) (*Output, error) {
	var out Output
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("refiner: parse output: %w", err)
	}
	return &out, nil
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, "\n")
}
