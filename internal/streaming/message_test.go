package streaming

import (
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/events"
)

func TestMarshalAndParseWorkflowEvent(t *testing.T) {
	evt := &events.Event{
		ID:         "evt-1",
		WorkflowID: "wf-1",
		TenantID:   "tenant-1",
		ScopeID:    "scope-1",
		Type:       events.EventPersonaStarted,
		OccurredAt: time.Now().UTC(),
	}

	raw, err := MarshalWorkflowEvent(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	parsed, err := ParseWorkflowEvent(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.ID != evt.ID || parsed.WorkflowID != evt.WorkflowID {
		t.Fatalf("parsed mismatch: %+v", parsed)
	}
}
