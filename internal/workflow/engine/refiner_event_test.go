package engine_test

// refiner_event_test.go validates the refiner.suggestion SSE event emission
// path introduced to fix the blank-name / leading-[] / missing-applied_path
// defects found in QA report workflow eb72e916:
//
//  1. Every emitted refiner.suggestion event has a non-empty Name field.
//  2. The Suggestion string never starts with "[]" (blank priority guard).
//  3. When ImprovementsRoot is set and imp.Content is non-empty, AppliedPath
//     is populated and contains the expected directory/file name components.
//  4. No refiner.suggestion events are emitted when Finalization is nil.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// refinerSuggestions filters ms.events for EventRefinerSuggestion and
// unmarshals each payload into a RefinerSuggestionPayload.
func refinerSuggestions(t *testing.T, ms *mockStore) []events.RefinerSuggestionPayload {
	t.Helper()
	var out []events.RefinerSuggestionPayload
	for _, ev := range ms.events {
		if ev.Type != events.EventRefinerSuggestion {
			continue
		}
		var p events.RefinerSuggestionPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("unmarshal RefinerSuggestionPayload: %v", err)
		}
		out = append(out, p)
	}
	return out
}

// finalizerWithImprovements returns a fixedPersona for the Finalizer that
// reports the supplied improvements in its FinalizationResult.
func finalizerWithImprovements(imps []state.RefinerImprovement) *fixedPersona {
	return &fixedPersona{
		kind: state.PersonaFinalizer,
		out: &state.PersonaOutput{
			Persona: state.PersonaFinalizer,
			Summary: "finalized",
			Finalization: &state.FinalizationResult{
				Summary:             "all done",
				RefinerImprovements: imps,
			},
		},
	}
}

// ─── Test 1: populated Name and non-empty Suggestion ─────────────────────────

func TestRefinerSuggestionEvents_NameAndSuggestionPopulated(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	imps := []state.RefinerImprovement{
		{
			ComponentType: "persona",
			ComponentName: "project_manager",
			Problem:       "uses emoji headers",
			ProposedFix:   "remove emoji headers",
			Priority:      "medium",
		},
		{
			ComponentType: "skill",
			ComponentName: "content-style",
			Problem:       "marketing framing bleeds in",
			ProposedFix:   "add technical-clarity constraint",
			Priority:      "high",
		},
	}

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, finalizerWithImprovements(imps))
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != len(imps) {
		t.Fatalf("expected %d refiner.suggestion events, got %d", len(imps), len(suggestions))
	}

	for i, s := range suggestions {
		if s.Name == "" {
			t.Errorf("suggestion[%d]: Name is empty — normalization may not have run or ComponentName was blank", i)
		}
		if strings.HasPrefix(s.Suggestion, "[]") {
			t.Errorf("suggestion[%d]: Suggestion starts with '[]' — Priority field was blank: %q", i, s.Suggestion)
		}
		if s.Suggestion == "" {
			t.Errorf("suggestion[%d]: Suggestion is empty", i)
		}
	}
}

// ─── Test 2: AppliedPath is populated when ImprovementsRoot + Content set ────

func TestRefinerSuggestionEvents_AppliedPathPopulatedForSkill(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	imps := []state.RefinerImprovement{
		{
			ComponentType: "skill",
			ComponentName: "content-style",
			Problem:       "missing technical-clarity section",
			ProposedFix:   "add ## Technical Clarity section",
			Priority:      "high",
			Content:       "# content-style\n\n## Technical Clarity\n...",
		},
	}

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, finalizerWithImprovements(imps))
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{
		DefaultProvider:  "mock",
		DefaultModel:     "mock",
		ImprovementsRoot: "/tmp/improvements",
	})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 refiner.suggestion event, got %d", len(suggestions))
	}

	s := suggestions[0]
	if s.AppliedPath == "" {
		t.Errorf("AppliedPath is empty — ImprovementsRoot is set and Content is non-empty, expected path to be derived")
	}
	if !strings.Contains(s.AppliedPath, "skills") {
		t.Errorf("AppliedPath %q does not contain 'skills' directory component", s.AppliedPath)
	}
	if !strings.Contains(s.AppliedPath, "content-style") {
		t.Errorf("AppliedPath %q does not contain component name 'content-style'", s.AppliedPath)
	}
	if !strings.HasSuffix(s.AppliedPath, "SKILL.md") {
		t.Errorf("AppliedPath %q does not end in SKILL.md", s.AppliedPath)
	}
}

func TestRefinerSuggestionEvents_AppliedPathPopulatedForPrompt(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	imps := []state.RefinerImprovement{
		{
			ComponentType: "prompt",
			ComponentName: "system",
			Problem:       "too permissive",
			ProposedFix:   "add constraints section",
			Priority:      "low",
			Content:       "# system prompt\n## Constraints\n...",
		},
	}

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, finalizerWithImprovements(imps))
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{
		DefaultProvider:  "mock",
		DefaultModel:     "mock",
		ImprovementsRoot: "/tmp/improvements",
	})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 refiner.suggestion event, got %d", len(suggestions))
	}

	s := suggestions[0]
	if s.AppliedPath == "" {
		t.Errorf("AppliedPath is empty for prompt component type")
	}
	if !strings.HasSuffix(s.AppliedPath, "system.prompt.md") {
		t.Errorf("AppliedPath %q does not end in system.prompt.md", s.AppliedPath)
	}
}

// ─── Test 3: no AppliedPath when Content is empty ────────────────────────────

func TestRefinerSuggestionEvents_NoAppliedPathWhenContentEmpty(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	// Advisory-only improvement: Content is empty.
	imps := []state.RefinerImprovement{
		{
			ComponentType: "persona",
			ComponentName: "pod",
			Problem:       "uses CTA language",
			ProposedFix:   "remove CTA phrases",
			Priority:      "medium",
			// Content intentionally left empty — advisory only.
		},
	}

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, finalizerWithImprovements(imps))
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{
		DefaultProvider:  "mock",
		DefaultModel:     "mock",
		ImprovementsRoot: "/tmp/improvements",
	})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 refiner.suggestion event, got %d", len(suggestions))
	}

	if suggestions[0].AppliedPath != "" {
		t.Errorf("AppliedPath should be empty for advisory-only improvement (empty Content), got %q", suggestions[0].AppliedPath)
	}
}

// ─── Test 4: no events when Finalization is nil ───────────────────────────────

func TestRefinerSuggestionEvents_NoneWhenFinalizationNil(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaFinalizer)
	// Finalizer returns output with nil Finalization.
	cleanup := registerPersonas(t, &fixedPersona{
		kind: state.PersonaFinalizer,
		out: &state.PersonaOutput{
			Persona:      state.PersonaFinalizer,
			Summary:      "finalized",
			Finalization: nil,
		},
	})
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != 0 {
		t.Errorf("expected 0 refiner.suggestion events when Finalization is nil, got %d", len(suggestions))
	}
}

// ─── Test 5: no events when RefinerImprovements list is empty ────────────────

func TestRefinerSuggestionEvents_NoneWhenImprovementsEmpty(t *testing.T) {
	ms := newMockStore()
	ctx := context.Background()

	ws := baseWorkflow(state.PersonaFinalizer)
	cleanup := registerPersonas(t, finalizerWithImprovements(nil))
	defer cleanup()

	ms.workflows[ws.ID] = ws
	eng := engine.New(ms, engine.Options{DefaultProvider: "mock", DefaultModel: "mock"})
	_ = eng.Run(ctx, ws.ID)

	suggestions := refinerSuggestions(t, ms)
	if len(suggestions) != 0 {
		t.Errorf("expected 0 refiner.suggestion events for empty improvements list, got %d", len(suggestions))
	}
}
