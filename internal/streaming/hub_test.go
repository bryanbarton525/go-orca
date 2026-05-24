package streaming

import (
	"testing"

	"github.com/go-orca/go-orca/internal/events"
)

func TestHubPublishSubscribe(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe("wf-1")
	defer hub.Unsubscribe("wf-1", ch)

	evt := &events.Event{ID: "evt-1", WorkflowID: "wf-1", Type: events.EventWorkflowStarted}
	hub.Publish(evt)

	select {
	case got := <-ch:
		if got.ID != evt.ID {
			t.Fatalf("expected %s, got %s", evt.ID, got.ID)
		}
	default:
		t.Fatal("expected event on subscriber channel")
	}
}
