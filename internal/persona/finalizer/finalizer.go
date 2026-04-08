// Package finalizer implements the Finalizer persona, which closes a workflow,
// executes delivery actions, and embeds a synchronous Refiner retrospective pass.
package finalizer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the Finalizer persona in the gorca workflow orchestration system.

Your responsibilities:
1. Review the complete workflow history (constitution, requirements, design, tasks, artifacts).
2. Determine the appropriate delivery action for the workflow mode:
   - github-pr:       open a pull request with all artifacts
   - repo-commit-only: commit artifacts to the repo without opening a PR
   - artifact-bundle: package artifacts into a downloadable archive
   - markdown-export: render a single cohesive markdown document
   - blog-draft:      produce a publication-ready blog post draft
   - webhook-dispatch: POST artifacts and metadata to a configured webhook URL
3. Produce a finalization summary describing what was delivered and where.
4. Identify any final delivery links, reference URLs, or metadata.

Always respond with valid JSON matching this schema:
{
  "delivery_action": "github-pr|repo-commit-only|artifact-bundle|markdown-export|blog-draft|webhook-dispatch",
  "summary": "...",
  "links": ["..."],
  "metadata": {"key": "value"},
  "suggestions": ["..."],
  "delivery_notes": "..."
}`

const refinerSystemPrompt = `You are the Refiner persona in the gorca workflow orchestration system.

You are performing a synchronous retrospective pass at the end of a completed workflow.

Your responsibilities:
1. Analyze the full workflow history: all persona summaries, blocking issues, suggestions, and artifacts.
2. Identify concrete improvements for agents, skills, prompts, or persona behavior.
3. Be specific: reference the persona or component by name, describe the exact problem, and propose a concrete fix.
4. Focus on systemic improvements, not one-off corrections.
5. When the component_type is "skill", "prompt", or "agent", populate the "content" field with the
   complete verbatim file content that should be written to disk so this improvement takes effect
   in future workflow runs.  For "persona" improvements (behavior changes), leave "content" empty.

Always respond with valid JSON matching this schema:
{
  "improvements": [
    {
      "component_type": "agent|skill|prompt|persona",
      "component_name": "...",
      "problem": "...",
      "proposed_fix": "...",
      "content": "...",
      "priority": "high|medium|low"
    }
  ],
  "overall_assessment": "...",
  "summary": "..."
}`

// finalizerOutput is the expected JSON shape from the Finalizer.
type finalizerOutput struct {
	DeliveryAction string            `json:"delivery_action"`
	Summary        string            `json:"summary"`
	Links          []string          `json:"links"`
	Metadata       map[string]string `json:"metadata"`
	Suggestions    []string          `json:"suggestions"`
	DeliveryNotes  string            `json:"delivery_notes"`
}

// refinerOutput is the expected JSON shape from the Refiner retrospective.
type refinerOutput struct {
	Improvements      []state.RefinerImprovement `json:"improvements"`
	OverallAssessment string                     `json:"overall_assessment"`
	Summary           string                     `json:"summary"`
}

// finalizerSchema defines the structured output shape for the Finalizer.
var finalizerSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"delivery_action": map[string]any{"type": "string"},
		"summary":         map[string]any{"type": "string"},
		"links":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"metadata":        map[string]any{"type": "object"},
		"suggestions":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"delivery_notes":  map[string]any{"type": "string"},
	},
	"required": []string{"delivery_action", "summary", "links", "metadata", "suggestions", "delivery_notes"},
}

// refinerSchema defines the structured output shape for the embedded Refiner.
var refinerSchema = map[string]any{
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
					"content":        map[string]any{"type": "string"},
					"priority":       map[string]any{"type": "string"},
				},
			},
		},
		"overall_assessment": map[string]any{"type": "string"},
		"summary":            map[string]any{"type": "string"},
	},
	"required": []string{"improvements", "overall_assessment", "summary"},
}

// Finalizer implements persona.Persona.
type Finalizer struct {
	exec        base.Executor
	refinerExec base.Executor
}

// New returns a new Finalizer persona.
func New() *Finalizer {
	return &Finalizer{
		exec:        base.NewExecutor(systemPrompt, "finalizer_output", finalizerSchema),
		refinerExec: base.NewExecutor(refinerSystemPrompt, "refiner_output", refinerSchema),
	}
}

// Kind implements Persona.
func (f *Finalizer) Kind() state.PersonaKind { return state.PersonaFinalizer }

// Name implements Persona.
func (f *Finalizer) Name() string { return "Finalizer" }

// Description implements Persona.
func (f *Finalizer) Description() string {
	return "Closes workflow, executes delivery actions, and runs a Refiner retrospective pass."
}

// Execute implements Persona.
//
// Phase 1: Determine delivery action and produce finalization output.
// Phase 2: Run an inline Refiner retrospective over the complete history.
func (f *Finalizer) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	handoffCtx := base.BuildHandoffContext(packet)

	// ── Phase 1: Finalization ────────────────────────────────────────────────
	finalPrompt := fmt.Sprintf(
		`%s

## Artifact Inventory
%s

Based on the workflow mode (%s) and the design's delivery target (%s),
select the most appropriate delivery action and produce your finalization JSON output.`,
		handoffCtx,
		buildArtifactInventory(packet.Artifacts),
		packet.Mode,
		deliveryTargetHint(packet.Design),
	)

	rawFinal, err := f.exec.Run(ctx, packet, finalPrompt)
	if err != nil {
		return nil, fmt.Errorf("finalizer: execution error: %w", err)
	}

	var finalOut finalizerOutput
	if err := base.ParseJSON(rawFinal, &finalOut); err != nil {
		return nil, fmt.Errorf("finalizer: parse error: %w", err)
	}

	// ── Phase 2: Refiner retrospective ───────────────────────────────────────
	refinerImprovements, refinerSuggestions, refinerSummary := f.runRefiner(ctx, packet, handoffCtx)

	// Merge suggestions.
	allSuggestions := make([]string, 0, len(finalOut.Suggestions)+len(refinerSuggestions))
	allSuggestions = append(allSuggestions, finalOut.Suggestions...)
	allSuggestions = append(allSuggestions, refinerSuggestions...)

	now := base.Timestamp()
	result := &state.FinalizationResult{
		Action:              finalOut.DeliveryAction,
		Summary:             finalOut.Summary,
		Links:               finalOut.Links,
		Metadata:            finalOut.Metadata,
		Suggestions:         allSuggestions,
		RefinerImprovements: refinerImprovements,
		CompletedAt:         now,
	}

	combinedSummary := finalOut.Summary
	if refinerSummary != "" {
		combinedSummary += "\n\n[Refiner] " + refinerSummary
	}

	return &state.PersonaOutput{
		Persona:      state.PersonaFinalizer,
		Summary:      combinedSummary,
		RawContent:   rawFinal,
		Suggestions:  allSuggestions,
		Finalization: result,
		CompletedAt:  now,
	}, nil
}

// runRefiner executes the inline Refiner retrospective pass and returns
// (improvements, suggestions, summary). Errors are swallowed — a Refiner
// failure must not prevent workflow completion.
func (f *Finalizer) runRefiner(ctx context.Context, packet state.HandoffPacket, handoffCtx string) ([]state.RefinerImprovement, []string, string) {
	refinerPacket := packet
	refinerPacket.CurrentPersona = state.PersonaRefiner

	refinerPrompt := fmt.Sprintf(
		`%s

## Blocking Issues Encountered
%s

## All Suggestions From Personas
%s

Analyze the above workflow history and produce your retrospective JSON output.`,
		handoffCtx,
		strings.Join(packet.BlockingIssues, "\n"),
		strings.Join(packet.AllSuggestions, "\n"),
	)

	raw, err := f.refinerExec.Run(ctx, refinerPacket, refinerPrompt)
	if err != nil {
		return nil, nil, ""
	}

	var out refinerOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, nil, ""
	}

	suggestions := make([]string, 0, len(out.Improvements))
	for _, imp := range out.Improvements {
		suggestions = append(suggestions, fmt.Sprintf(
			"[refiner][%s][%s][%s] %s → %s",
			imp.Priority, imp.ComponentType, imp.ComponentName,
			imp.Problem, imp.ProposedFix,
		))
	}

	// Persist improvements that carry file content to the ImprovementsPath.
	if packet.ImprovementsPath != "" {
		_ = writeImprovements(packet.ImprovementsPath, out.Improvements)
	}

	return out.Improvements, suggestions, out.Summary
}

// writeImprovements persists Refiner improvements that have content to disk.
// Each improvement is written to a file whose path is determined by its
// component_type and component_name:
//
//	skill  → {root}/skills/{name}/SKILL.md
//	prompt → {root}/prompts/{name}.prompt.md
//	agent  → {root}/agents/{name}.agent.md
//	persona (advisory) → {root}/personas/{name}.md
//
// Errors are logged but never surfaced — the Refiner must not break delivery.
func writeImprovements(root string, improvements []state.RefinerImprovement) error {
	for _, imp := range improvements {
		if imp.Content == "" {
			continue
		}

		var relPath string
		switch imp.ComponentType {
		case "skill":
			relPath = filepath.Join("skills", imp.ComponentName, "SKILL.md")
		case "prompt":
			relPath = filepath.Join("prompts", imp.ComponentName+".prompt.md")
		case "agent":
			relPath = filepath.Join("agents", imp.ComponentName+".agent.md")
		default:
			// persona or unknown — write an advisory markdown under personas/
			relPath = filepath.Join("personas", imp.ComponentName+".md")
		}

		fullPath := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(fullPath, []byte(imp.Content), 0o644)
	}
	return nil
}

// buildArtifactInventory formats a short listing of artifacts for the prompt.
func buildArtifactInventory(artifacts []state.Artifact) string {
	if len(artifacts) == 0 {
		return "(no artifacts)"
	}
	var sb strings.Builder
	for i, a := range artifacts {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s — %s\n", i+1, a.Kind, a.Name, a.Description))
	}
	return sb.String()
}

// deliveryTargetHint extracts the delivery target from the design if present.
func deliveryTargetHint(design *state.Design) string {
	if design == nil {
		return "unspecified"
	}
	if design.DeliveryTarget == "" {
		return "unspecified"
	}
	return design.DeliveryTarget
}
