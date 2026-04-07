// Package director implements the Director persona, responsible for analysing
// the incoming request, classifying the workflow mode, selecting the provider
// and model, and deciding which downstream personas are required.
package director

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

const systemPrompt = `You are the Director persona in the gorca workflow orchestration system.

Your responsibilities:
1. Analyse the user's request and classify the workflow mode.
2. Select the most appropriate delivery target and finalizer action.
3. Decide which downstream personas are required (project_manager, architect, implementer, qa, finalizer).
4. Output a structured JSON plan.

Workflow modes:
- software: code, apps, libraries, infra-as-code
- content: blog posts, articles, marketing copy
- docs: technical documentation, wikis, READMEs
- research: analysis, reports, competitive research
- ops: CI/CD, deployment, operational tasks
- mixed: combination of the above

Finalizer actions: github-pr | repo-commit-only | artifact-bundle | markdown-export | blog-draft | webhook-dispatch

Always respond with valid JSON matching this schema:
{
  "mode": "<WorkflowMode>",
  "title": "<short descriptive title>",
  "provider": "<provider name: openai|ollama|copilot>",
  "model": "<model name>",
  "finalizer_action": "<action>",
  "required_personas": ["project_manager", "architect", "implementer", "qa", "finalizer"],
  "rationale": "<brief explanation of decisions>",
  "summary": "<one sentence description for handoff>"
}`

// Output is the Director's typed JSON output.
type Output struct {
	Mode             state.WorkflowMode  `json:"mode"`
	Title            string              `json:"title"`
	Provider         string              `json:"provider"`
	Model            string              `json:"model"`
	FinalizerAction  string              `json:"finalizer_action"`
	RequiredPersonas []state.PersonaKind `json:"required_personas"`
	Rationale        string              `json:"rationale"`
	Summary          string              `json:"summary"`
}

// Director implements persona.Persona.
type Director struct {
	exec base.Executor
}

// New returns a new Director persona.
func New() *Director {
	return &Director{exec: base.NewExecutor(systemPrompt)}
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

	userPrompt := fmt.Sprintf(
		"Analyse this request and produce your JSON plan.\n\nRequest:\n%s",
		packet.Request,
	)

	raw, err := d.exec.Run(ctx, packet, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("director: execution error: %w", err)
	}

	var out Output
	if err := base.ParseJSON(raw, &out); err != nil {
		// Fallback: use defaults rather than failing the whole workflow.
		out = defaultOutput(packet)
	}

	// Normalise.
	if out.Mode == "" {
		out.Mode = state.WorkflowModeSoftware
	}
	if out.Provider == "" {
		out.Provider = packet.ProviderName
	}
	if out.Model == "" {
		out.Model = packet.ModelName
	}
	if len(out.RequiredPersonas) == 0 {
		out.RequiredPersonas = defaultPersonas()
	}
	if out.FinalizerAction == "" {
		out.FinalizerAction = "artifact-bundle"
	}
	if out.Title == "" {
		out.Title = truncate(packet.Request, 60)
	}

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
	return out
}

func defaultOutput(packet state.HandoffPacket) Output {
	return Output{
		Mode:             state.WorkflowModeSoftware,
		Title:            truncate(packet.Request, 60),
		Provider:         packet.ProviderName,
		Model:            packet.ModelName,
		FinalizerAction:  "artifact-bundle",
		RequiredPersonas: defaultPersonas(),
		Rationale:        "Defaulted due to parse failure.",
		Summary:          packet.Request,
	}
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
