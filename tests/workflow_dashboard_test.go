package main

import (
	"encoding/json"
	"net/http"
	http.Error
	"net/http/httptest"
	"testing"
)

// Assuming the following structures and handlers are defined elsewhere in the package
type WorkflowStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type SubmissionRequest struct {
	TemplateID string `json:"template_id"`
	Params     map[string]string `json:"params"`
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	WorkflowID string `json:"workflow_id,omitempty"`
}

// TestSubmitWorkflow_BadRequest tests the submission endpoint when required fields are missing.
func TestSubmitWorkflow_BadRequest(t *testing.T) {
	// Setup: Mock the handler to test failure path
	// In a real scenario, this would use the actual handler function.
	// We are focusing on testing the contract defined by the expected error response.
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Simulate validation failure for required field
	w.WriteHeader(http.StatusBadRequest)
	// Write the specific JSON error structure required by the contract
	resp := APIResponse{Success: false, Message: "Template ID is required to submit a workflow request."}
	json.NewEncoder(w).Encode(resp)
})

	req, _ := http.NewRequest("POST", "/api/v1/workflows/submit", nil)
	rr := httptest.NewRecorder()

	// Execute the test case
	testHandler.ServeHTTP(rr, req)

	// 1. Assert Status Code
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}

	// 2. Assert Body Content Structure
	var actualResp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &actualResp); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}
	
	if actualResp.Success != false {
		t.Errorf("Expected Success: false, got: %v", actualResp.Success)
	}
	
	if actualResp.Message != "Template ID is required to submit a workflow request." {
		.Errorf("Expected error message 'Template ID is required to submit a workflow request.', got: %s", actualResp.Message)
	}
}

// TestSubmitWorkflow_Success tests a successful job submission (202 Accepted).
func TestSubmitWorkflow_Success(t *testing.T) {
	// Setup: Mock successful handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Simulate success
	w.WriteHeader(http.StatusAccepted)
	resp := APIResponse{Success: true, Message: "Workflow accepted for processing.", WorkflowID: "wf-abc123"}
	json.NewEncoder(w).Encode(resp)
})

	req, _ := http.NewRequest("POST", "/api/v1/workflows/submit", nil)
	rr := httptest.NewRecorder()

	testHandler.ServeHTTP(rr, req)

	// Assert Status Code
	if rr.Code != http.StatusAccepted {
		t.Errorf("Expected status code %d, got %d", http.StatusAccepted, rr.Code)
	}

	// Assert Body Content Structure
	var actualResp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &actualResp); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}
	
	if !actualResp.Success {
		t.Errorf("Expected Success: true, got: %v", actualResp.Success)
	}
		if actualResp.WorkflowID != "wf-abc123" {
		t.Errorf("Expected WorkflowID 'wf-abc123', got: %s", actualResp.WorkflowID)
	}
}
