// Package events defines the append-only workflow event journal used for
// auditing, replay, and the Refiner's retrospective analysis.
package events

import (
	"encoding/json"
	"time"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/google/uuid"
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
	EventPersonaRetrying  EventType = "persona.retrying"

	EventTaskCreated   EventType = "task.created"
	EventTaskStarted   EventType = "task.started"
	EventTaskCompleted EventType = "task.completed"
	EventTaskFailed    EventType = "task.failed"
	EventTaskSkipped   EventType = "task.skipped"

	EventArtifactProduced  EventType = "artifact.produced"
	EventStateTransition   EventType = "state.transition"
	EventProviderCall      EventType = "provider.call"
	EventRefinerSuggestion EventType = "refiner.suggestion"

	// EventQAExhausted is emitted when the QA remediation loop hits
	// MaxQARetries with blocking issues still present.  The workflow
	// continues to the Finalizer rather than failing.
	EventQAExhausted EventType = "qa.exhausted"
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

// PersonaRetryingPayload is the payload for EventPersonaRetrying.
// Emitted before each retry attempt so that clients can observe recovery in
// progress without needing to poll GET /workflows/:id.
type PersonaRetryingPayload struct {
	Persona      state.PersonaKind `json:"persona"`
	Attempt      int               `json:"attempt"`      // 1-based retry number
	MaxAttempts  int               `json:"max_attempts"` // total attempts including original
	Error        string            `json:"error"`        // error that triggered this retry (kept for compatibility)
	Reason       string            `json:"reason"`       // human-readable reason, mirrors Error
	RetryAfterMs int64             `json:"retry_after_ms"`
}

// TaskStartedPayload is the payload for EventTaskStarted.
type TaskStartedPayload struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
}

// TaskCompletedPayload is the payload for EventTaskCompleted.
type TaskCompletedPayload struct {
	TaskID     string `json:"task_id"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	DurationMs int64  `json:"duration_ms"`
}

// TaskFailedPayload is the payload for EventTaskFailed.
type TaskFailedPayload struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	Error  string `json:"error"`
}

// ArtifactProducedPayload is the payload for EventArtifactProduced.
type ArtifactProducedPayload struct {
	TaskID        string `json:"task_id"`
	ArtifactName  string `json:"artifact_name"`
	Kind          string `json:"kind"`
	ContentLength int    `json:"content_length"`
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
	// Status is the outcome from the ImprovementDispatcher:
	// "applied" | "dispatched" | "skipped" | "error" | "" (when no dispatcher)
	Status string `json:"status,omitempty"`
	// ChildWorkflowID is set when Status == "dispatched" and a child
	// improvement workflow was launched to open a GitHub PR.
	ChildWorkflowID string `json:"child_workflow_id,omitempty"`
}

// QAExhaustedPayload is the payload for EventQAExhausted.
// It records the unresolved blocking issues so the Refiner can include them
// in its retrospective analysis.
type QAExhaustedPayload struct {
	RetriesAllowed int      `json:"retries_allowed"`
	BlockingIssues []string `json:"blocking_issues"`
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
