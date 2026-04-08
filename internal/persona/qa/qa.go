// Package qa implements the QA persona, responsible for validating all
// artifacts against the constitution, requirements, and design.
package qa

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

// qaIssue is a single issue found by the QA persona.
type qaIssue struct {
	Severity       string `json:"severity"`
	Component      string `json:"component"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

// qaOutput is the expected JSON shape from the QA persona.
type qaOutput struct {
	Passed         bool      `json:"passed"`
	BlockingIssues []qaIssue `json:"blocking_issues"`
	Warnings       []qaIssue `json:"warnings"`
	Suggestions    []string  `json:"suggestions"`
	CoverageScore  int       `json:"coverage_score"`
	QualityScore   int       `json:"quality_score"`
	Summary        string    `json:"summary"`
}

// outputSchema defines the structured JSON shape for QA responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"passed": map[string]any{"type": "boolean"},
		"blocking_issues": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"severity":       map[string]any{"type": "string"},
					"component":      map[string]any{"type": "string"},
					"description":    map[string]any{"type": "string"},
					"recommendation": map[string]any{"type": "string"},
				},
				"required": []string{"severity", "component", "description", "recommendation"},
			},
		},
		"warnings": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"severity":       map[string]any{"type": "string"},
					"component":      map[string]any{"type": "string"},
					"description":    map[string]any{"type": "string"},
					"recommendation": map[string]any{"type": "string"},
				},
				"required": []string{"severity", "component", "description", "recommendation"},
			},
		},
		"suggestions":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"coverage_score": map[string]any{"type": "integer"},
		"quality_score":  map[string]any{"type": "integer"},
		"summary":        map[string]any{"type": "string"},
	},
	"required": []string{"passed", "blocking_issues", "warnings", "suggestions", "coverage_score", "quality_score", "summary"},
}

// QA implements persona.Persona.
type QA struct {
	exec base.Executor
}

// New returns a new QA persona.
func New() *QA {
	return &QA{exec: base.NewExecutor("qa_output", outputSchema)}
}

// Kind implements Persona.
func (q *QA) Kind() state.PersonaKind { return state.PersonaQA }

// Name implements Persona.
func (q *QA) Name() string { return "QA" }

// Description implements Persona.
func (q *QA) Description() string {
	return "Validates all artifacts against constitution, requirements, and design."
}

// Execute implements Persona.
func (q *QA) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyQA]

	handoffCtx := base.BuildHandoffContext(packet)

	artifactSummary := buildArtifactSummary(packet.Artifacts)

	userPrompt := fmt.Sprintf(
		`%s

## Artifacts to Validate
%s

Review every artifact above against the constitution, requirements, and design.
Identify all blocking issues, warnings, and suggestions.
Respond with your JSON output.`,
		handoffCtx,
		artifactSummary,
	)

	raw, err := q.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("qa: execution error: %w", err)
	}

	var out qaOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("qa: parse error: %w", err)
	}

	// Collect blocking issue descriptions for the WorkflowState.
	// Filter out empty objects that the LLM may emit when the schema's
	// "required" enforcement is not honoured — an empty blocker would
	// otherwise produce a "[]: " string that trips the engine's
	// len(BlockingIssues) > 0 guard and causes a spurious remediation pass.
	blocking := make([]string, 0, len(out.BlockingIssues))
	for _, iss := range out.BlockingIssues {
		if iss.Component == "" && iss.Description == "" {
			continue // discard phantom empty blocker
		}
		blocking = append(blocking, fmt.Sprintf("[%s] %s: %s", iss.Component, iss.Description, iss.Recommendation))
	}

	// Merge warnings into suggestions list (also skip empty items).
	allSuggestions := make([]string, 0, len(out.Suggestions)+len(out.Warnings))
	for _, w := range out.Warnings {
		if w.Component == "" && w.Description == "" {
			continue
		}
		allSuggestions = append(allSuggestions, fmt.Sprintf("[warning][%s] %s: %s", w.Component, w.Description, w.Recommendation))
	}
	allSuggestions = append(allSuggestions, out.Suggestions...)

	now := base.Timestamp()
	return &state.PersonaOutput{
		Persona:        state.PersonaQA,
		Summary:        out.Summary,
		RawContent:     raw,
		BlockingIssues: blocking,
		Suggestions:    allSuggestions,
		CompletedAt:    now,
	}, nil
}

// buildArtifactSummary formats artifacts for inclusion in the QA prompt.
func buildArtifactSummary(artifacts []state.Artifact) string {
	if len(artifacts) == 0 {
		return "(no artifacts produced)"
	}
	var sb fmt.Stringer
	_ = sb
	out := ""
	for i, a := range artifacts {
		out += fmt.Sprintf("### Artifact %d: %s\nKind: %s\nDescription: %s\n\n```\n%s\n```\n\n",
			i+1, a.Name, a.Kind, a.Description, a.Content)
	}
	return out
}
