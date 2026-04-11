// Package director implements the Director persona, responsible for analysing
// the incoming request, classifying the workflow mode, selecting the provider
// and model, and deciding which downstream personas are required.
package director

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
)

// Output is the Director's typed JSON output.
type Output struct {
	Mode             state.WorkflowMode            `json:"mode"`
	Title            string                        `json:"title"`
	Provider         string                        `json:"provider"`
	Model            string                        `json:"model"`
	PersonaModels    state.PersonaModelAssignments `json:"persona_models"`
	FinalizerAction  string                        `json:"finalizer_action"`
	RequiredPersonas []state.PersonaKind           `json:"required_personas"`
	Rationale        string                        `json:"rationale"`
	Summary          string                        `json:"summary"`
}

// outputSchema defines the structured JSON shape for Director responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"mode":     map[string]any{"type": "string"},
		"title":    map[string]any{"type": "string"},
		"provider": map[string]any{"type": "string"},
		"model":    map[string]any{"type": "string"},
		"persona_models": map[string]any{
			"type":                 "object",
			"additionalProperties": map[string]any{"type": "string"},
		},
		"finalizer_action":  map[string]any{"type": "string"},
		"required_personas": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"rationale":         map[string]any{"type": "string"},
		"summary":           map[string]any{"type": "string"},
	},
	"required": []string{"mode", "title", "provider", "model", "persona_models", "finalizer_action", "required_personas", "rationale", "summary"},
}

// Director implements persona.Persona.
type Director struct {
	exec base.Executor
}

// New returns a new Director persona.
func New() *Director {
	return &Director{exec: base.NewExecutor("director_output", outputSchema)}
}

// Kind implements Persona.
func (d *Director) Kind() state.PersonaKind { return state.PersonaDirector }

// Name implements Persona.
func (d *Director) Name() string { return "Director" }

// Description implements Persona.
func (d *Director) Description() string {
	return "Analyses requests, classifies workflow mode, selects provider/model, and plans the persona pipeline."
}

// Execute implements Persona.
func (d *Director) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	started := time.Now()

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyDirector]

	providerHint := buildProviderHint(packet.ProviderCatalogs)
	if providerHint == "" {
		providerHint = fmt.Sprintf("  - %s\n    default model: %s\n    allowed models:\n      - %s",
			packet.ProviderName,
			packet.ModelName,
			packet.ModelName,
		)
	}

	modeHint := ""
	if packet.Mode != "" {
		modeHint = fmt.Sprintf(
			"\nRequested workflow mode override: %s\nThis mode was preselected before the Director ran. You MUST keep the \"mode\" field set to exactly %q and choose downstream personas/finalizer accordingly.\n",
			packet.Mode,
			packet.Mode,
		)
	}

	userPrompt := fmt.Sprintf(
		"Analyse this request and produce your JSON plan.\n\nAvailable providers and allowed models:\n%s\n\nYou are currently running on the bootstrap/default model shown below. Select the best provider for the workflow and choose downstream models for these personas only: project_manager, architect, implementer, qa, finalizer.\nYou MUST use one of the provider names exactly as listed above.\nEvery model you choose MUST exactly match an allowed model listed for that provider. Models not listed are unavailable or excluded by policy and MUST NOT be selected.\nUse the selected provider's default model as the fallback when a persona does not need a specialized model.\nBootstrap provider: %s\nBootstrap model: %s\n%s\nRequest:\n%s",
		providerHint,
		packet.ProviderName,
		packet.ModelName,
		modeHint,
		packet.Request,
	)

	raw, err := d.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("director: execution error: %w", err)
	}

	out := OutputFromRaw(raw, packet)

	_ = started // logged by the engine
	return &state.PersonaOutput{
		Persona:     state.PersonaDirector,
		Summary:     out.Summary,
		RawContent:  raw,
		CompletedAt: base.Timestamp(),
		// Director packs its structured output into the Design field temporarily;
		// the engine uses Director's output to set mode/provider/model on the
		// workflow state directly.
	}, nil
}

// OutputFromRaw parses a Director raw response into its structured Output.
// Exported so the workflow engine can read mode/provider decisions.
func OutputFromRaw(raw string, packet state.HandoffPacket) Output {
	var out Output
	if err := base.ParseJSON(raw, &out); err != nil {
		return defaultOutput(packet)
	}
	return normalizeOutput(out, packet)
}

func defaultOutput(packet state.HandoffPacket) Output {
	mode := defaultMode(packet)
	return Output{
		Mode:             mode,
		Title:            truncate(packet.Request, 60),
		Provider:         packet.ProviderName,
		Model:            packet.ModelName,
		PersonaModels:    make(state.PersonaModelAssignments),
		FinalizerAction:  defaultFinalizerAction(mode),
		RequiredPersonas: normalizeRequiredPersonas(mode, nil),
		Rationale:        "Defaulted due to parse failure.",
		Summary:          packet.Request,
	}
}

func normalizeOutput(out Output, packet state.HandoffPacket) Output {
	if packet.Mode != "" {
		out.Mode = packet.Mode
	} else if out.Mode == "" {
		out.Mode = defaultMode(packet)
	}
	if out.Provider == "" {
		out.Provider = packet.ProviderName
	}
	if out.Model == "" {
		out.Model = packet.ModelName
	}
	out.PersonaModels = normalizePersonaModels(out.PersonaModels)
	out.RequiredPersonas = normalizeRequiredPersonas(out.Mode, out.RequiredPersonas)
	if out.FinalizerAction == "" {
		out.FinalizerAction = defaultFinalizerAction(out.Mode)
	}
	if out.Title == "" {
		out.Title = truncate(packet.Request, 60)
	}
	return out
}

func normalizeRequiredPersonas(mode state.WorkflowMode, requested []state.PersonaKind) []state.PersonaKind {
	standard := defaultPersonas()
	seen := make(map[state.PersonaKind]bool, len(standard)+len(requested))

	if len(requested) == 0 || mode == state.WorkflowModeSoftware || mode == state.WorkflowModeContent {
		for _, kind := range standard {
			seen[kind] = true
		}
	}
	for _, kind := range requested {
		if kind != "" {
			seen[kind] = true
		}
	}

	out := make([]state.PersonaKind, 0, len(seen))
	for _, kind := range standard {
		if seen[kind] {
			out = append(out, kind)
			delete(seen, kind)
		}
	}
	for _, kind := range requested {
		if seen[kind] {
			out = append(out, kind)
			delete(seen, kind)
		}
	}
	return out
}

func defaultFinalizerAction(mode state.WorkflowMode) string {
	switch mode {
	case state.WorkflowModeContent:
		return "blog-draft"
	case state.WorkflowModeDocs, state.WorkflowModeResearch:
		return "doc-draft"
	default:
		// api-response requires no external config and returns all artifacts
		// inline — safe default for software/ops/mixed when no delivery target
		// has been explicitly configured.
		return "api-response"
	}
}

func defaultMode(packet state.HandoffPacket) state.WorkflowMode {
	if packet.Mode != "" {
		return packet.Mode
	}
	return state.WorkflowModeSoftware
}

func defaultPersonas() []state.PersonaKind {
	return []state.PersonaKind{
		state.PersonaProjectMgr,
		state.PersonaArchitect,
		state.PersonaImplementer,
		state.PersonaQA,
		state.PersonaFinalizer,
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func buildProviderHint(catalogs map[string]state.ProviderModelCatalog) string {
	if len(catalogs) == 0 {
		return ""
	}
	names := make([]string, 0, len(catalogs))
	for name, catalog := range catalogs {
		if catalog.DefaultModel == "" && len(catalog.Models) == 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var lines []string
	for _, name := range names {
		catalog := catalogs[name]
		lines = append(lines, fmt.Sprintf("  - %s", name))
		lines = append(lines, fmt.Sprintf("    default model: %s", catalog.DefaultModel))
		lines = append(lines, "    allowed models:")
		for _, model := range catalog.Models {
			lines = append(lines, fmt.Sprintf("      - %s", formatModelHint(model)))
		}
	}
	return strings.Join(lines, "\n")
}

func formatModelHint(model state.ProviderModelInfo) string {
	label := model.ID
	if name := strings.TrimSpace(model.Name); name != "" && name != model.ID {
		label = fmt.Sprintf("%s (%s)", model.ID, name)
	}
	var extras []string
	if family := strings.TrimSpace(model.Metadata["family"]); family != "" {
		extras = append(extras, "family="+family)
	}
	if paramSize := strings.TrimSpace(model.Metadata["parameter_size"]); paramSize != "" {
		extras = append(extras, "params="+paramSize)
	}
	if toolsVal := strings.TrimSpace(model.Metadata["tools"]); toolsVal != "" {
		extras = append(extras, "tools="+toolsVal)
	}
	if len(extras) == 0 {
		return label
	}
	return fmt.Sprintf("%s [%s]", label, strings.Join(extras, ", "))
}

func normalizePersonaModels(in state.PersonaModelAssignments) state.PersonaModelAssignments {
	out := make(state.PersonaModelAssignments)
	if in == nil {
		return out
	}
	for _, kind := range state.DownstreamPersonaKinds() {
		if model := strings.TrimSpace(in[kind]); model != "" {
			out[kind] = model
		}
	}
	return out
}
