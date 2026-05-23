package streaming

import (
	"encoding/json"
	"fmt"

	"github.com/go-orca/go-orca/internal/events"
)

const (
	// KindWorkflowEvent marks workflow journal records in the shared topic.
	KindWorkflowEvent = "workflow.event"
)

// Envelope is the canonical on-wire format for workflow journal messages.
type Envelope struct {
	Kind  string        `json:"kind"`
	Event *events.Event `json:"event,omitempty"`
}

// MarshalWorkflowEvent serializes a workflow journal event for Redpanda.
func MarshalWorkflowEvent(evt *events.Event) ([]byte, error) {
	if evt == nil {
		return nil, fmt.Errorf("workflow event is nil")
	}
	return json.Marshal(Envelope{
		Kind:  KindWorkflowEvent,
		Event: evt,
	})
}

// ParseWorkflowEvent decodes a workflow journal message from a record value.
func ParseWorkflowEvent(value []byte) (*events.Event, error) {
	var env Envelope
	if err := json.Unmarshal(value, &env); err != nil {
		return nil, err
	}
	if env.Kind != KindWorkflowEvent || env.Event == nil {
		return nil, fmt.Errorf("not a workflow event envelope")
	}
	return env.Event, nil
}
