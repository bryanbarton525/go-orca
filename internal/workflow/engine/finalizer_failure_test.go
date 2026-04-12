package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/finalizer/actions"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

func TestFinalizerActionFailure_EmitsFailedAndClearsFinalization(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaFinalizer)
	ws.Artifacts = []state.Artifact{{Kind: state.ArtifactKindCode, Name: "main.go", Content: "package main"}}

	cleanup := registerPersonas(t, &fixedPersona{
		kind: state.PersonaFinalizer,
		out: &state.PersonaOutput{
			Persona: state.PersonaFinalizer,
			Summary: "finalized",
			Finalization: &state.FinalizationResult{
				Action:  string(actions.ActionBlogDraft),
				Summary: "publishable draft ready",
			},
		},
	})
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock-model"})
	err := eng.Run(ctx, ws.ID)
	if err == nil {
		t.Fatal("expected Run to fail when finalizer delivery action fails")
	}
	if !strings.Contains(err.Error(), "delivery action") {
		t.Fatalf("Run error = %q, want delivery action failure", err.Error())
	}

	stored, err := ms.GetWorkflow(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if stored.Status != state.WorkflowStatusFailed {
		t.Fatalf("workflow status = %q, want %q", stored.Status, state.WorkflowStatusFailed)
	}
	if stored.Finalization != nil {
		t.Fatalf("expected finalization to be cleared on delivery failure, got %+v", stored.Finalization)
	}
	if stored.ErrorMessage == "" {
		t.Fatal("expected workflow error_message to be populated")
	}
	if stored.Summaries[state.PersonaFinalizer] != "" {
		t.Fatalf("finalizer summary = %q, want empty after failure", stored.Summaries[state.PersonaFinalizer])
	}

	var sawCompleted bool
	var sawFailed bool
	for _, ev := range ms.events {
		if ev.Persona != state.PersonaFinalizer {
			continue
		}
		if ev.Type == events.EventPersonaCompleted {
			sawCompleted = true
		}
		if ev.Type == events.EventPersonaFailed {
			sawFailed = true
		}
	}
	if sawCompleted {
		t.Fatal("unexpected persona.completed event for failed finalizer")
	}
	if !sawFailed {
		t.Fatal("expected persona.failed event for failed finalizer")
	}
}
