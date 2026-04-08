// Package qa implements the QA persona, responsible for validating all
// artifacts against the constitution, requirements, and design.
package qa

import (
	"context"
	"fmt"
	"strings"
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
					"severity": map[string]any{
						"type": "string",
						"enum": []any{"blocking", "warning", "info"},
					},
					"component":      map[string]any{"type": "string", "minLength": 1},
					"description":    map[string]any{"type": "string", "minLength": 1},
					"recommendation": map[string]any{"type": "string", "minLength": 1},
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

	// For content-mode workflows build the artifact section focused on the
	// delivery candidate (latest blog_post, else latest markdown).  All other
	// artifacts are supplied as supporting context only, not as acceptance targets.
	var artifactSummary string
	if packet.Mode == state.WorkflowModeContent {
		artifactSummary = buildContentArtifactSummary(packet.Artifacts)
	} else {
		artifactSummary = buildArtifactSummary(packet.Artifacts)
	}

	userPrompt := fmt.Sprintf(
		`%s

## Artifacts to Validate
%s

Review the artifact(s) above against the constitution, requirements, and design.
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

	// Normalise: separate truly blocking issues from advisory ones.
	// A "blocking" issue must have severity == "blocking" AND a concrete,
	// actionable recommendation (not "None", "N/A", or blank).
	// Everything else is downgraded to a warning/suggestion.
	blocking, downgraded := classifyIssues(out.BlockingIssues)

	// Merge warnings: declared warnings + downgraded + skip empty items.
	allSuggestions := make([]string, 0, len(out.Suggestions)+len(out.Warnings)+len(downgraded))
	for _, w := range out.Warnings {
		if w.Component == "" && w.Description == "" {
			continue
		}
		allSuggestions = append(allSuggestions, fmt.Sprintf("[warning][%s] %s: %s", w.Component, w.Description, w.Recommendation))
	}
	for _, d := range downgraded {
		allSuggestions = append(allSuggestions, fmt.Sprintf("[downgraded][%s] %s: %s", d.Component, d.Description, d.Recommendation))
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

// classifyIssues separates a slice of qaIssues into:
//   - truly blocking: severity == "blocking" AND recommendation is concrete
//   - downgraded: everything else (wrong severity, phantom empty, non-actionable)
//
// The returned blocking slice contains formatted strings ready for the engine's
// len(BlockingIssues) > 0 guard.  Downgraded items are returned as-is for
// conversion to suggestions by the caller.
func classifyIssues(issues []qaIssue) (blocking []string, downgraded []qaIssue) {
	for _, iss := range issues {
		// Drop phantom empty objects the LLM emits when schema enforcement fails.
		if iss.Component == "" && iss.Description == "" {
			continue
		}

		sev := strings.ToLower(strings.TrimSpace(iss.Severity))

		// Non-blocking severity values go straight to downgraded.
		if sev != "blocking" {
			downgraded = append(downgraded, iss)
			continue
		}

		// Blocking severity but non-actionable recommendation — downgrade.
		rec := strings.TrimSpace(iss.Recommendation)
		recLower := strings.ToLower(rec)
		if rec == "" || recLower == "none" || recLower == "n/a" || recLower == "no recommendation" {
			downgraded = append(downgraded, iss)
			continue
		}

		// Purely editorial notes that should never halt delivery.
		// Heuristic: if description mentions only title wording, tone of title,
		// or "next step" / "concluding challenge" with no structural defect,
		// downgrade rather than block.
		if isEditorialOnly(iss) {
			downgraded = append(downgraded, iss)
			continue
		}

		blocking = append(blocking, fmt.Sprintf("[%s] %s: %s", iss.Component, iss.Description, iss.Recommendation))
	}
	return blocking, downgraded
}

// isEditorialOnly returns true for issues that are stylistic or polish
// suggestions that should never block publication of a technical article.
// These map to the exact failure patterns observed in workflow a2ffa163.
func isEditorialOnly(iss qaIssue) bool {
	desc := strings.ToLower(iss.Description)
	rec := strings.ToLower(iss.Recommendation)

	editorialPatterns := []string{
		"title",
		"slightly promotional",
		"more academic",
		"more neutral title",
		"tone of the title",
		"next step",
		"concluding challenge",
		"concluding paragraph",
		"suggest an advanced topic",
		"overall quality",
		"technically excellent",
		"passes the technical bar",
		"content depth",
	}
	for _, p := range editorialPatterns {
		if strings.Contains(desc, p) || strings.Contains(rec, p) {
			return true
		}
	}
	return false
}

// buildContentArtifactSummary builds the QA prompt section for content-mode
// workflows. The delivery candidate (latest blog_post, else latest markdown)
// is presented as the primary acceptance target. All other artifacts are
// listed as supporting context that is NOT under evaluation.
func buildContentArtifactSummary(artifacts []state.Artifact) string {
	if len(artifacts) == 0 {
		return "(no artifacts produced)"
	}

	// Find delivery candidate: latest blog_post, else latest markdown.
	candidateIdx := -1
	for i := len(artifacts) - 1; i >= 0; i-- {
		if artifacts[i].Kind == state.ArtifactKindBlogPost {
			candidateIdx = i
			break
		}
	}
	if candidateIdx == -1 {
		for i := len(artifacts) - 1; i >= 0; i-- {
			if artifacts[i].Kind == state.ArtifactKindMarkdown {
				candidateIdx = i
				break
			}
		}
	}

	if candidateIdx == -1 {
		// No recognisable content artifact — fall back to full list.
		return buildArtifactSummary(artifacts)
	}

	candidate := artifacts[candidateIdx]
	out := fmt.Sprintf(
		"### DELIVERY CANDIDATE (this is the artifact under evaluation)\n"+
			"### Artifact: %s\nKind: %s\nDescription: %s\n\n```\n%s\n```\n\n",
		candidate.Name, candidate.Kind, candidate.Description, candidate.Content,
	)

	// Append supporting artifacts as read-only context.
	var supporting []string
	for i, a := range artifacts {
		if i == candidateIdx {
			continue
		}
		supporting = append(supporting, fmt.Sprintf("- [%d] [%s] %s — %s", i+1, a.Kind, a.Name, a.Description))
	}
	if len(supporting) > 0 {
		out += "### Supporting artifacts (context only — NOT under evaluation)\n"
		for _, s := range supporting {
			out += s + "\n"
		}
		out += "\n"
	}
	return out
}

// buildArtifactSummary formats artifacts for inclusion in the QA prompt.
func buildArtifactSummary(artifacts []state.Artifact) string {
	if len(artifacts) == 0 {
		return "(no artifacts produced)"
	}
	out := ""
	for i, a := range artifacts {
		out += fmt.Sprintf("### Artifact %d: %s\nKind: %s\nDescription: %s\n\n```\n%s\n```\n\n",
			i+1, a.Name, a.Kind, a.Description, a.Content)
	}
	return out
}
