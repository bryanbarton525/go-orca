package engine_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

type nestedProgressPersona struct {
	kind state.PersonaKind
}

func (p *nestedProgressPersona) Kind() state.PersonaKind { return p.kind }
func (p *nestedProgressPersona) Name() string            { return string(p.kind) }
func (p *nestedProgressPersona) Description() string     { return "" }

func (p *nestedProgressPersona) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	if packet.EmitPersonaStarted != nil {
		packet.EmitPersonaStarted(ctx, state.PersonaRefiner, packet.ProviderName, packet.ModelName)
	}
	if packet.EmitPersonaCompleted != nil {
		packet.EmitPersonaCompleted(ctx, state.PersonaRefiner, 42, "inline refiner finished", nil)
	}
	return &state.PersonaOutput{Persona: p.kind, Summary: "finalized"}, nil
}

func personaEventKinds(t *testing.T, ms *mockStore) []string {
	t.Helper()
	var out []string
	for _, ev := range ms.events {
		if ev.Type != events.EventPersonaStarted && ev.Type != events.EventPersonaCompleted {
			continue
		}
		out = append(out, string(ev.Type)+":"+string(ev.Persona))
	}
	return out
}

func personaCompletedPayloads(t *testing.T, ms *mockStore, personaKind state.PersonaKind) []events.PersonaCompletedPayload {
	t.Helper()
	var out []events.PersonaCompletedPayload
	for _, ev := range ms.events {
		if ev.Type != events.EventPersonaCompleted || ev.Persona != personaKind {
			continue
		}
		var payload events.PersonaCompletedPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("unmarshal PersonaCompletedPayload: %v", err)
		}
		out = append(out, payload)
	}
	return out
}

func TestNestedPersonaEvents_EmittedDuringFinalizerPhase(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, &nestedProgressPersona{kind: state.PersonaFinalizer})
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock-model"})
	if err := eng.Run(ctx, ws.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := personaEventKinds(t, ms)
	want := []string{
		"persona.started:finalizer",
		"persona.started:refiner",
		"persona.completed:refiner",
		"persona.completed:finalizer",
	}
	if len(got) != len(want) {
		t.Fatalf("persona event count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (all events: %v)", i, got[i], want[i], got)
		}
	}

	completed := personaCompletedPayloads(t, ms, state.PersonaRefiner)
	if len(completed) != 1 {
		t.Fatalf("expected 1 refiner completion payload, got %d", len(completed))
	}
	if completed[0].Summary != "inline refiner finished" {
		t.Fatalf("refiner completion summary = %q, want %q", completed[0].Summary, "inline refiner finished")
	}
	if completed[0].DurationMs != 42 {
		t.Fatalf("refiner completion duration = %d, want %d", completed[0].DurationMs, 42)
	}

	stored, err := ms.GetWorkflow(ctx, ws.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if stored.Execution.CurrentPersona != state.PersonaFinalizer {
		t.Fatalf("Execution.CurrentPersona = %q, want %q", stored.Execution.CurrentPersona, state.PersonaFinalizer)
	}
}
