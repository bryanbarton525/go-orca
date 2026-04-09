package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WorkflowParams defines the expected input structure for a new workflow submission.
type WorkflowParams struct {
	TemplateID string `json:"template_id"`
	Parameters map[string]string `json:"parameters"`
}

// SubmissionResponse defines the standardized output for submission results.
type SubmissionResponse struct {
	Success    bool   `json:"success"`
	WorkflowID string `json:"workflow_id,omitempty"`
	Message    string `json:"message,omitempty"`
}

// SubmitWorkflow handles the POST request to submit a new workflow execution.
// It simulates interaction with the core orchestration service.
func SubmitWorkflow(w http.ResponseWriter, r *http.Request) {
	var params WorkflowParams

	// 1. Decode and Validate Input
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	if params.TemplateID == "" {
		RespondWithError(w, http.StatusBadRequest, "Missing required field: template_id")
		return
	}

	// In a real system, we would validate parameters map keys/values here.

	// 2. Simulate Core Orchestration Submission
	// Simulate the call to the underlying workflow engine service.
	workflowID := generateMockWorkflowID()

	// Simulate potential failure during submission
	if params.TemplateID == "fail_template" {
		RespondWithError(w, http.StatusInternalServerError, "Failed to submit workflow template: Template not found or service unavailable.")
		return
	}

	// 3. Respond on Success
	resp := SubmissionResponse{
		Success:    true,
		WorkflowID: workflowID,
		Message:    fmt.Sprintf("Workflow submission initiated successfully for template %s.", params.TemplateID),
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202 Accepted is standard for async job submission
	json.NewEncoder(w).Encode(resp)
}

// generateMockWorkflowID creates a deterministic mock ID for testing.
func generateMockWorkflowID() string {
	return fmt.Sprintf("wf-%d-%s", time.Now().Unix(), "mock")
}

// RespondWithError sends a structured JSON error response.
func RespondWithError(w http.ResponseWriter, code int, message string) {
	resp := map[string]string{
		"success": "false",
		"message": message,
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}
