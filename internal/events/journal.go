// Package events defines the append-only workflow event journal used for
// auditing, replay, and the Refiner's retrospective analysis.
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/go-orca/go-orca/internal/state"
)

// EventType classifies a workflow event.
type EventType string

const (
	EventWorkflowCreated   EventType = "workflow.created"
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventWorkflowCancelled EventType = "workflow.cancelled"
	EventWorkflowPaused    EventType = "workflow.paused"
	EventWorkflowResumed   EventType = "workflow.resumed"

	EventPersonaStarted   EventType = "persona.started"
	EventPersonaCompleted EventType = "persona.completed"
	EventPersonaFailed    EventType = "persona.failed"

	EventTaskCreated   EventType = "task.created"
	EventTaskStarted   EventType = "task.started"
	EventTaskCompleted EventType = "task.completed"
	EventTaskFailed    EventType = "task.failed"
	EventTaskSkipped   EventType = "task.skipped"

	EventArtifactProduced  EventType = "artifact.produced"
	EventStateTransition   EventType = "state.transition"
	EventProviderCall      EventType = "provider.call"
	EventRefinerSuggestion EventType = "refiner.suggestion"
)

// Event is a single immutable journal entry for a workflow.
type Event struct {
	ID         string            `json:"id"`
	WorkflowID string            `json:"workflow_id"`
	TenantID   string            `json:"tenant_id"`
	ScopeID    string            `json:"scope_id"`
	Type       EventType         `json:"type"`
	Persona    state.PersonaKind `json:"persona,omitempty"`
	Payload    json.RawMessage   `json:"payload,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
}

// NewEvent constructs an Event with a generated ID and current timestamp.
func NewEvent(workflowID, tenantID, scopeID string, evtType EventType, persona state.PersonaKind, payload interface{}) (*Event, error) {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &Event{
		ID:         uuid.New().String(),
		WorkflowID: workflowID,
		TenantID:   tenantID,
		ScopeID:    scopeID,
		Type:       evtType,
		Persona:    persona,
		Payload:    raw,
		OccurredAt: time.Now().UTC(),
	}, nil
}

// ─── Payload types ────────────────────────────────────────────────────────────

// PersonaStartedPayload is the payload for EventPersonaStarted.
type PersonaStartedPayload struct {
	Persona      state.PersonaKind `json:"persona"`
	ProviderName string            `json:"provider_name"`
	ModelName    string            `json:"model_name"`
}

// PersonaCompletedPayload is the payload for EventPersonaCompleted.
type PersonaCompletedPayload struct {
	Persona        state.PersonaKind `json:"persona"`
	DurationMs     int64             `json:"duration_ms"`
	Summary        string            `json:"summary"`
	BlockingIssues []string          `json:"blocking_issues,omitempty"`
}

// PersonaFailedPayload is the payload for EventPersonaFailed.
type PersonaFailedPayload struct {
	Persona state.PersonaKind `json:"persona"`
	Error   string            `json:"error"`
}

// ProviderCallPayload is the payload for EventProviderCall.
type ProviderCallPayload struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

// StateTransitionPayload is the payload for EventStateTransition.
type StateTransitionPayload struct {
	From state.WorkflowStatus `json:"from"`
	To   state.WorkflowStatus `json:"to"`
}

// RefinerSuggestionPayload is the payload for EventRefinerSuggestion.
type RefinerSuggestionPayload struct {
	Component   string `json:"component"` // agent | skill | prompt | persona
	Name        string `json:"name"`
	Suggestion  string `json:"suggestion"`
	AppliedPath string `json:"applied_path,omitempty"`
}

// ─── Journal note ─────────────────────────────────────────────────────────────

// The event journal is persisted via the storage.EventStore interface defined
// in the storage package.  That interface provides context-aware ListEvents,
// ListEventsByType, and EventsSince methods and is satisfied by both the
// Postgres and SQLite concrete stores.
//
// The Journal interface that previously existed here has been removed to avoid
// a divergent, context-free shadow of storage.EventStore.  Callers should
// depend on storage.EventStore (or storage.Store) directly.
