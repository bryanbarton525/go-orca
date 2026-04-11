// Package finalizer implements the Finalizer persona, which closes a workflow,
// executes delivery actions, and embeds a synchronous Refiner retrospective pass.
package finalizer

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/improvements"
	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

// finalizerOutput is the expected JSON shape from the Finalizer.
type finalizerOutput struct {
	DeliveryAction string         `json:"delivery_action"`
	Summary        string         `json:"summary"`
	Links          []string       `json:"links"`
	Metadata       map[string]any `json:"metadata"`
	Suggestions    []string       `json:"suggestions"`
	DeliveryNotes  string         `json:"delivery_notes"`
}

// refinerOutput is the expected JSON shape from the Refiner retrospective.
type refinerOutput struct {
	Improvements      []state.RefinerImprovement `json:"improvements"`
	OverallAssessment string                     `json:"overall_assessment"`
	HealthScore       float64                    `json:"health_score"`
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
// It includes change_type, apply_mode, and files fields for the self-improvement pipeline.
var refinerSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"improvements": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"component_type": map[string]any{"type": "string", "minLength": 1},
					"component_name": map[string]any{"type": "string", "minLength": 1},
					"problem":        map[string]any{"type": "string", "minLength": 1},
					"proposed_fix":   map[string]any{"type": "string", "minLength": 1},
					"change_type":    map[string]any{"type": "string", "enum": []any{"create", "update", "advisory"}},
					"apply_mode":     map[string]any{"type": "string"},
					"files": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path":    map[string]any{"type": "string"},
								"content": map[string]any{"type": "string"},
							},
							"required": []string{"path", "content"},
						},
					},
					"content":  map[string]any{"type": "string"},
					"priority": map[string]any{"type": "string", "enum": []any{"high", "medium", "low"}},
				},
				"required": []string{"component_type", "component_name", "problem", "proposed_fix", "change_type", "priority"},
			},
		},
		"overall_assessment": map[string]any{"type": "string"},
		"health_score":       map[string]any{"type": "number", "minimum": 0, "maximum": 100},
		"summary":            map[string]any{"type": "string"},
	},
	"required": []string{"improvements", "overall_assessment", "health_score", "summary"},
}

// Finalizer implements persona.Persona.
type Finalizer struct {
	exec        base.Executor
	refinerExec base.Executor
}

// New returns a new Finalizer persona.
func New() *Finalizer {
	return &Finalizer{
		exec:        base.NewExecutor("finalizer_output", finalizerSchema),
		refinerExec: base.NewExecutor("refiner_output", refinerSchema),
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

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyFinalizer]

	// ── Phase 1: Finalization ────────────────────────────────────────────────
	// Build the finalizer user prompt, including the Director-preferred action
	// when one was set so the LLM understands the intent.
	preferredActionHint := ""
	if packet.FinalizerAction != "" {
		preferredActionHint = fmt.Sprintf("\nPreferred delivery action (selected by Director): %s\n", packet.FinalizerAction)
	}

	finalPrompt := fmt.Sprintf(
		`%s

## Artifact Inventory
%s
%s
Based on the workflow mode (%s) and the design's delivery target (%s),
select the most appropriate delivery action and produce your finalization JSON output.`,
		handoffCtx,
		buildArtifactInventory(packet.Artifacts),
		preferredActionHint,
		packet.Mode,
		deliveryTargetHint(packet.Design),
	)

	rawFinal, err := f.exec.Run(ctx, packet, systemPrompt, finalPrompt)
	if err != nil {
		return nil, fmt.Errorf("finalizer: execution error: %w", err)
	}

	var finalOut finalizerOutput
	if err := base.ParseJSON(rawFinal, &finalOut); err != nil {
		return nil, fmt.Errorf("finalizer: parse error: %w", err)
	}

	// Director-selected action overrides the LLM's choice immediately so the
	// delivery action cannot drift from the Director's intent.
	if packet.FinalizerAction != "" {
		finalOut.DeliveryAction = packet.FinalizerAction
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

## Current Persona Prompt Files
These are the current verbatim contents of the persona prompt files.
If you propose a "persona" improvement with change_type "update", you MUST include
a modified version of the relevant file below in the "files" array.

%s

## Blocking Issues Encountered
%s

## All Suggestions From Personas
%s

Analyze the above workflow history and produce your retrospective JSON output.`,
		handoffCtx,
		buildPersonaPromptContext(packet.PersonaPromptSnapshot),
		strings.Join(packet.BlockingIssues, "\n"),
		strings.Join(packet.AllSuggestions, "\n"),
	)

	raw, err := f.refinerExec.Run(ctx, refinerPacket, packet.PersonaPromptSnapshot[prompts.KeyFinalizerRefiner], refinerPrompt)
	if err != nil {
		return nil, nil, ""
	}

	var out refinerOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, nil, ""
	}
	out.Improvements = normalizeImprovements(out.Improvements)

	suggestions := make([]string, 0, len(out.Improvements))
	for _, imp := range out.Improvements {
		suggestions = append(suggestions, fmt.Sprintf(
			"[refiner][%s][%s][%s] %s → %s",
			imp.Priority, imp.ComponentType, imp.ComponentName,
			imp.Problem, imp.ProposedFix,
		))
	}

	return out.Improvements, suggestions, out.Summary
}

// buildPersonaPromptContext formats the workflow personas' current prompt content
// so the Refiner can produce updated file content when proposing persona improvements.
// The self-referential refiner prompts are excluded to avoid confusion.
func buildPersonaPromptContext(snapshot map[string]string) string {
	if len(snapshot) == 0 {
		return "(no persona prompt files available)"
	}
	// Only include the main workflow personas in a stable order.
	order := []struct{ key, file string }{
		{"director", "director.md"},
		{"project_manager", "project_manager.md"},
		{"architect", "architect.md"},
		{"implementer", "implementer.md"},
		{"qa", "qa.md"},
		{"finalizer", "finalizer.md"},
	}
	var sb strings.Builder
	for _, entry := range order {
		content, ok := snapshot[entry.key]
		if !ok || content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("### prompts/personas/%s\n```\n%s\n```\n\n", entry.file, content))
	}
	if sb.Len() == 0 {
		return "(no persona prompt files available)"
	}
	return sb.String()
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

// normalizeImprovements trims whitespace from all string fields, drops
// improvements that have any blank required field (component_type,
// component_name, problem, proposed_fix, priority) or an invalid priority, and
// deduplicates by (component_type, component_name) — keeping the highest-
// priority entry when duplicates exist.
//
// Deduplication prevents the model from spawning multiple child improvement
// workflows for the same component when it emits repeated suggestions.
// Priority is also normalised to lowercase so that "HIGH" and "High" both
// survive the filter.
func normalizeImprovements(imps []state.RefinerImprovement) []state.RefinerImprovement {
	priorityRank := map[string]int{"high": 0, "medium": 1, "low": 2}

	// First pass: trim, validate, and collect the best entry per component key.
	type key struct{ ctype, cname string }
	best := make(map[key]state.RefinerImprovement)
	order := make([]key, 0, len(imps))

	for _, imp := range imps {
		imp.ComponentType = strings.TrimSpace(imp.ComponentType)
		imp.ComponentName = strings.TrimSpace(imp.ComponentName)
		imp.Problem = strings.TrimSpace(imp.Problem)
		imp.ProposedFix = strings.TrimSpace(imp.ProposedFix)
		imp.Priority = strings.TrimSpace(strings.ToLower(imp.Priority))
		imp.ChangeType = strings.TrimSpace(strings.ToLower(imp.ChangeType))
		imp.ApplyMode = strings.TrimSpace(strings.ToLower(imp.ApplyMode))
		imp.Content = strings.TrimSpace(imp.Content)
		if imp.ComponentType == "" || imp.ComponentName == "" ||
			imp.Problem == "" || imp.ProposedFix == "" || imp.Priority == "" {
			continue
		}
		// Reject placeholder component names that the LLM uses when it cannot
		// identify a specific target.
		switch strings.ToLower(strings.TrimSpace(imp.ComponentName)) {
		case "n/a", "na", "unknown", "placeholder", "tbd":
			continue
		}
		switch imp.Priority {
		case "high", "medium", "low":
			// valid
		default:
			continue
		}

		// Enforce improvement surface: only persona/prompt/skill markdown assets
		// are in scope. Engine code, agents, and repo source are never targets.
		if !improvements.IsSurfaceAllowed(imp) {
			continue
		}

		k := key{imp.ComponentType, imp.ComponentName}
		if existing, seen := best[k]; !seen {
			best[k] = imp
			order = append(order, k)
		} else if priorityRank[imp.Priority] < priorityRank[existing.Priority] {
			// Replace with higher-priority duplicate.
			best[k] = imp
		}
	}

	// Second pass: emit in original first-seen order.
	valid := make([]state.RefinerImprovement, 0, len(order))
	for _, k := range order {
		valid = append(valid, best[k])
	}
	return valid
}
