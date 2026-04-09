package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// WorkflowStatus represents the core data structure returned by the status endpoint.
type WorkflowStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"` // e.g., "Pending", "Running", "Paused"
}

// MockStateStore simulates access to the actual orchestration state store.
type MockStateStore struct {
	mu sync.RWMutex
	workflows map[string]string // ID -> Status
	isAvailable bool
}

func NewMockStateStore(available bool) *MockStateStore {
	return &MockStateStore{
		workflows: map[string]string{
			"wf-abc-123": "Running",
			"wf-def-456": "Pending",
			"wf-ghi-789": "Paused",
		},
		isAvailable: available,
	}
}

// GetActiveWorkflows simulates querying the state store for all in-flight workflows.
// It returns an error if the store is conceptually unavailable.
func (m *MockStateStore) GetActiveWorkflows() ([]WorkflowStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.isAvailable {
		return nil, fmt.Errorf("state store is currently unreachable or inaccessible")
	}

	statuses := make([]WorkflowStatus, 0, len(m.workflows))
	for id, status := range m.workflows {
		statuses = append(statuses, WorkflowStatus{ID: id, Status: status})
	}
	return statuses, nil
}

// StatusHandler implements the handler for GET /api/v1/workflows/status.
// It reads the status from the mocked state store and returns it as JSON.
func StatusHandler(store *MockStateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// In a real application, this would likely use middleware or dependency injection 
	// to access the actual state store connection object.
	
	statuses, err := store.GetActiveWorkflows()
	if err != nil {
		// F4: Robust Error Handling for backend failure
		http.Error(w, fmt.Sprintf("Failed to retrieve workflow status: %v", err), http.StatusServiceUnavailable)
		return
	}

	// Success case
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Using json.NewEncoder for minimal boilerplate, writes directly to response writer
	json.NewEncoder(w).Encode(statuses)
}

// --- Usage Example / Initialization --- 
// In the main application router setup:
/*
func main() {
    // Setup the simulated state store
    store := NewMockStateStore(true) 
    
    // Setup handlers
    mux := http.NewServeMux()
    // Register the handler, assuming the 'api' package context
    mux.Handle("/api/v1/workflows/status", &StatusHandler{store})

    // Start server
    http.ListenAndServe(":8080", mux)
}
*/
