// Package qa implements the QA persona, responsible for validating all
// artifacts against the constitution, requirements, and design.
package qa

import (
	"context"
	"fmt"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the QA persona in the gorca workflow orchestration system.

Your responsibilities:
1. Validate every artifact produced by the Implementer against:
   - The constitution (vision, goals, constraints, acceptance criteria)
   - The requirements (functional and non-functional)
   - The design (architecture, components, decisions)
2. Identify blocking issues that MUST be resolved before delivery.
3. Identify non-blocking suggestions that are improvements but not blockers.
4. Assess overall quality and readiness for finalization.
5. Be thorough but fair — do not invent issues that do not exist.

Severity levels:
- "blocking": workflow cannot proceed to Finalizer until resolved
- "warning": should be addressed but does not block delivery
- "info": informational, low-priority improvement

Always respond with valid JSON matching this schema:
{
  "passed": true|false,
  "blocking_issues": [
    {"severity": "blocking", "component": "...", "description": "...", "recommendation": "..."}
  ],
  "warnings": [
    {"severity": "warning", "component": "...", "description": "...", "recommendation": "..."}
  ],
  "suggestions": ["..."],
  "coverage_score": 0-100,
  "quality_score": 0-100,
  "summary": "..."
}`

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

// QA implements persona.Persona.
type QA struct {
	exec base.Executor
}

// New returns a new QA persona.
func New() *QA {
	return &QA{exec: base.NewExecutor(systemPrompt)}
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

	raw, err := q.exec.Run(ctx, packet, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("qa: execution error: %w", err)
	}

	var out qaOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("qa: parse error: %w", err)
	}

	// Collect blocking issue descriptions for the WorkflowState.
	blocking := make([]string, 0, len(out.BlockingIssues))
	for _, iss := range out.BlockingIssues {
		blocking = append(blocking, fmt.Sprintf("[%s] %s: %s", iss.Component, iss.Description, iss.Recommendation))
	}

	// Merge warnings into suggestions list.
	allSuggestions := make([]string, 0, len(out.Suggestions)+len(out.Warnings))
	for _, w := range out.Warnings {
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
