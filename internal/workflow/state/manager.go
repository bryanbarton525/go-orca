package workflowstate

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-orca/go-orca/internal/state"
)

// Manager governs dynamic logical pod/workflow definitions for auto mode.
type Manager interface {
	ListDefinitions(ctx context.Context, ws *state.WorkflowState) ([]state.AutoDefinition, error)
	GenerateCandidates(ctx context.Context, ws *state.WorkflowState, attempt int) ([]state.AutoDefinition, error)
	RecordAttempt(ctx context.Context, ws *state.WorkflowState, attempt state.AutoDefinitionAttempt) error
	PromoteDefinition(ctx context.Context, ws *state.WorkflowState, definition state.AutoDefinition) error
}

// MemoryManager is a lightweight default manager that derives definitions from
// the workflow request and keeps promoted definitions in-process.
type MemoryManager struct {
	mu       sync.Mutex
	defs     map[string][]state.AutoDefinition
	attempts map[string][]state.AutoDefinitionAttempt
}

func NewMemoryManager() *MemoryManager {
	return &MemoryManager{
		defs:     make(map[string][]state.AutoDefinition),
		attempts: make(map[string][]state.AutoDefinitionAttempt),
	}
}

func (m *MemoryManager) ListDefinitions(_ context.Context, ws *state.WorkflowState) ([]state.AutoDefinition, error) {
	if ws == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.defs[ws.ScopeID]
	out := make([]state.AutoDefinition, len(current))
	copy(out, current)
	return out, nil
}

func (m *MemoryManager) GenerateCandidates(_ context.Context, ws *state.WorkflowState, attempt int) ([]state.AutoDefinition, error) {
	if ws == nil {
		return nil, nil
	}
	seed := strings.TrimSpace(ws.Title)
	if seed == "" {
		seed = strings.TrimSpace(ws.Request)
	}
	if seed == "" {
		seed = "auto-mode-workflow"
	}
	baseID := slug(seed)
	if baseID == "" {
		baseID = "auto-workflow"
	}
	if attempt <= 0 {
		attempt = 1
	}
	primary := state.AutoDefinition{
		ID:             fmt.Sprintf("%s-v%d", baseID, attempt),
		Name:           fmt.Sprintf("Auto Definition v%d", attempt),
		Summary:        "Generated logical pod workflow for auto mode remediation and convergence.",
		PodSpecialties: []string{"backend", "ops"},
		WorkflowHints:  []string{"qa-remediation", "validation-first", "small-iteration"},
		Prompt:         fmt.Sprintf("Auto mode definition attempt %d for request: %s", attempt, seed),
		Source:         "generated",
	}
	alternate := primary
	alternate.ID = fmt.Sprintf("%s-v%d-alt", baseID, attempt)
	alternate.Name = fmt.Sprintf("Auto Definition v%d Alternate", attempt)
	alternate.PodSpecialties = []string{"backend", "data"}
	alternate.WorkflowHints = []string{"qa-remediation", "parallel-pod", "checkpoint-heavy"}
	return []state.AutoDefinition{primary, alternate}, nil
}

func (m *MemoryManager) RecordAttempt(_ context.Context, ws *state.WorkflowState, attempt state.AutoDefinitionAttempt) error {
	if ws == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if attempt.OccurredAt.IsZero() {
		attempt.OccurredAt = time.Now().UTC()
	}
	m.attempts[ws.ID] = append(m.attempts[ws.ID], attempt)
	return nil
}

func (m *MemoryManager) PromoteDefinition(_ context.Context, ws *state.WorkflowState, definition state.AutoDefinition) error {
	if ws == nil || strings.TrimSpace(definition.ID) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	definition.Source = "catalog"
	existing := m.defs[ws.ScopeID]
	updated := make([]state.AutoDefinition, 0, len(existing)+1)
	updated = append(updated, definition)
	for _, item := range existing {
		if item.ID != definition.ID {
			updated = append(updated, item)
		}
	}
	m.defs[ws.ScopeID] = updated
	return nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, value)
	value = strings.Trim(value, "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return value
}
