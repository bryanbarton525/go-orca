package director

import (
	"encoding/json"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestOutputFromRawFallbackPreservesExplicitMode(t *testing.T) {
	out := OutputFromRaw("not-json", state.HandoffPacket{
		Mode:    state.WorkflowModeContent,
		Request: "Write a blog post about Go generics",
	})

	if out.Mode != state.WorkflowModeContent {
		t.Fatalf("Mode: got %q, want %q", out.Mode, state.WorkflowModeContent)
	}
	if out.Title == "" {
		t.Fatal("expected fallback title to be populated from request")
	}
}

func TestOutputFromRawNormalizesPersonaModels(t *testing.T) {
	rawBody, err := json.Marshal(map[string]any{
		"mode":     "software",
		"title":    "Build routing",
		"provider": "ollama",
		"model":    "qwen3.5:9b",
		"persona_models": map[string]string{
			"director":        "ignored",
			"implementer":     "qwen3.5-coder:14b",
			"project_manager": "qwen3.5:9b",
		},
		"finalizer_action":  "artifact-bundle",
		"required_personas": []string{"project_manager", "implementer", "finalizer"},
		"rationale":         "Route code generation differently.",
		"summary":           "Use a stronger coding model for implementation.",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	out := OutputFromRaw(string(rawBody), state.HandoffPacket{})
	if _, found := out.PersonaModels[state.PersonaDirector]; found {
		t.Fatal("director persona model should be dropped from normalized output")
	}
	if got := out.PersonaModels[state.PersonaImplementer]; got != "qwen3.5-coder:14b" {
		t.Fatalf("implementer model: got %q", got)
	}
	if got := out.PersonaModels[state.PersonaProjectMgr]; got != "qwen3.5:9b" {
		t.Fatalf("project manager model: got %q", got)
	}
}
