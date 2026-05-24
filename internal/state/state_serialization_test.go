package state_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/state"
)

func TestExecutionPlanningAndAutoModeJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	exec := state.Execution{
		Planning: &state.PlanningState{
			Mode:      "builder",
			Prompt:    "plan a builder workflow",
			Plan:      "step 1",
			Decisions: []string{"persist planning state"},
			UpdatedAt: now,
		},
		AutoMode: &state.AutoModeState{
			Enabled:            true,
			MaxAttempts:        4,
			DefinitionAttempts: 2,
			ActiveDefinition: &state.AutoDefinition{
				ID:      "builder-v2",
				Summary: "candidate",
				Source:  "generated",
			},
			Attempts: []state.AutoDefinitionAttempt{
				{
					Attempt:      1,
					DefinitionID: "builder-v1",
					Succeeded:    false,
					OccurredAt:   now,
				},
			},
		},
	}

	raw, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded state.Execution
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Planning == nil || decoded.Planning.Mode != "builder" {
		t.Fatalf("planning round-trip failed: %+v", decoded.Planning)
	}
	if decoded.AutoMode == nil || decoded.AutoMode.ActiveDefinition == nil {
		t.Fatalf("auto mode round-trip failed: %+v", decoded.AutoMode)
	}
	if decoded.AutoMode.ActiveDefinition.ID != "builder-v2" {
		t.Fatalf("active definition id: got %q, want %q", decoded.AutoMode.ActiveDefinition.ID, "builder-v2")
	}
}
