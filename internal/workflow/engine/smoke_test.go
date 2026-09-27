package engine_test

import (
	"context"
	"testing"

	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// The credential-free sandbox smoke must persist the precise missing-provider
// failure, rather than accepting an arbitrary startup or storage failure.
func TestSmokeMissingProviderPersistsTerminalState(t *testing.T) {
	previous, existed := persona.Get(state.PersonaDirector)
	persona.Register(&noopPersona{kind: state.PersonaDirector})
	t.Cleanup(func() {
		persona.Unregister(state.PersonaDirector)
		if existed {
			persona.Register(previous)
		}
	})
	store := newMockStore()
	ws := state.NewWorkflowState("default", "global", "Return the text ORCA_SMOKE_OK. Do not use tools or access external data.")
	store.workflows[ws.ID] = ws
	eng := engine.New(store, engine.Options{PersonaPromptRoot: "../../../prompts/personas"})
	err := eng.Run(context.Background(), ws.ID)
	const expected = "director phase: engine: no provider resolved for persona \"director\""
	if err == nil || err.Error() != expected {
		t.Fatalf("expected %q, got %v", expected, err)
	}
	saved, err := store.GetWorkflow(context.Background(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != state.WorkflowStatusFailed || saved.ErrorMessage != expected {
		t.Fatalf("unexpected persisted state: status=%q error=%q", saved.Status, saved.ErrorMessage)
	}
}
