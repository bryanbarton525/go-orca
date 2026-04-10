package engine

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestApplyOutputDirectorPreservesExplicitMode(t *testing.T) {
	ws := state.NewWorkflowState("t1", "s1", "Write a technical blog post")
	ws.Mode = state.WorkflowModeContent

	eng := New(nil, Options{})
	eng.applyOutput(ws, &state.PersonaOutput{
		Persona:    state.PersonaDirector,
		Summary:    "classified",
		RawContent: `{"mode":"software","title":"Wrong"}`,
	})

	if ws.Mode != state.WorkflowModeContent {
		t.Fatalf("Mode: got %q, want %q", ws.Mode, state.WorkflowModeContent)
	}
}

func TestApplyOutputDirectorUsesRequestOnParseFallback(t *testing.T) {
	ws := state.NewWorkflowState("t1", "s1", "Write a technical blog post")
	ws.Mode = state.WorkflowModeContent

	eng := New(nil, Options{})
	eng.applyOutput(ws, &state.PersonaOutput{
		Persona:    state.PersonaDirector,
		Summary:    "fallback",
		RawContent: "not-json",
	})

	if ws.Mode != state.WorkflowModeContent {
		t.Fatalf("Mode: got %q, want %q", ws.Mode, state.WorkflowModeContent)
	}
	if ws.Title == "" {
		t.Fatal("expected fallback title to be populated from request")
	}
}

func TestApplyOutputDirectorEnforcesSoftwarePipeline(t *testing.T) {
	ws := state.NewWorkflowState("t1", "s1", "Build a software workflow")

	eng := New(nil, Options{})
	eng.applyOutput(ws, &state.PersonaOutput{
		Persona: state.PersonaDirector,
		Summary: "classified",
		RawContent: `{
			"mode":"software",
			"title":"Software workflow",
			"required_personas":["implementer"],
			"summary":"handoff"
		}`,
	})

	want := []state.PersonaKind{
		state.PersonaProjectMgr,
		state.PersonaArchitect,
		state.PersonaImplementer,
		state.PersonaQA,
		state.PersonaFinalizer,
	}
	if len(ws.RequiredPersonas) != len(want) {
		t.Fatalf("RequiredPersonas len: got %d, want %d (%v)", len(ws.RequiredPersonas), len(want), ws.RequiredPersonas)
	}
	for i, kind := range want {
		if ws.RequiredPersonas[i] != kind {
			t.Fatalf("RequiredPersonas[%d]: got %q, want %q", i, ws.RequiredPersonas[i], kind)
		}
	}
	if ws.FinalizerAction != "artifact-bundle" {
		t.Fatalf("FinalizerAction: got %q, want %q", ws.FinalizerAction, "artifact-bundle")
	}
}

func TestApplyOutputDirectorDefaultsContentPipeline(t *testing.T) {
	ws := state.NewWorkflowState("t1", "s1", "Write a technical blog post")
	ws.Mode = state.WorkflowModeContent

	eng := New(nil, Options{})
	eng.applyOutput(ws, &state.PersonaOutput{
		Persona: state.PersonaDirector,
		Summary: "classified",
		RawContent: `{
			"mode":"software",
			"title":"Wrong mode",
			"required_personas":["implementer"],
			"summary":"handoff"
		}`,
	})

	want := []state.PersonaKind{
		state.PersonaProjectMgr,
		state.PersonaArchitect,
		state.PersonaImplementer,
		state.PersonaQA,
		state.PersonaFinalizer,
	}
	if len(ws.RequiredPersonas) != len(want) {
		t.Fatalf("RequiredPersonas len: got %d, want %d (%v)", len(ws.RequiredPersonas), len(want), ws.RequiredPersonas)
	}
	for i, kind := range want {
		if ws.RequiredPersonas[i] != kind {
			t.Fatalf("RequiredPersonas[%d]: got %q, want %q", i, ws.RequiredPersonas[i], kind)
		}
	}
	if ws.FinalizerAction != "blog-draft" {
		t.Fatalf("FinalizerAction: got %q, want %q", ws.FinalizerAction, "blog-draft")
	}
	if ws.Mode != state.WorkflowModeContent {
		t.Fatalf("Mode: got %q, want %q", ws.Mode, state.WorkflowModeContent)
	}
}
