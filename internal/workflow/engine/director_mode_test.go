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
