package handler

import (
	"encoding/json"
	"net/http"
	"go-orca/internal/service"
	"go-orca/pkg/models"
)

// WorkflowHandler manages the API endpoints for workflows.
type WorkflowHandler struct {
	Service *service.WorkflowService
}

// NewWorkflowHandler creates a new instance of WorkflowHandler.
func NewWorkflowHandler(s *service.WorkflowService) *WorkflowHandler {
	return &WorkflowHandler{Service: s}
}

// submitWorkflow handles the POST request to submit a new workflow.
// It enforces input validation (F3) and ensures stable JSON responses (F4).
func (h *WorkflowHandler) SubmitWorkflow(w http.ResponseWriter, r *http.Request) {
	var req models.SubmitRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON format: invalid body", http.StatusBadRequest)
		return
	}

	// F3: Input Validation
	if req.WorkflowName == "" || req.Parameters == nil {
		http.Error(w, `{"error": "WorkflowName and Parameters are required."}`, http.StatusBadRequest)
		return
	}

	// F3: Deeper validation check on parameters structure (minimal example)
	if len(req.Parameters) == 0 {
		http.Error(w, `{"error": "Parameters list cannot be empty."}`,
		http.StatusBadRequest)
		return
	}

	// Call service layer
	workflowID, err := h.Service.SubmitWorkflow(r.Context(), req.WorkflowName, req.Parameters)

	if err != nil {
		// Return stable JSON error structure
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(models.APIError{ErrorMessage: err.Error()})
		return
	}

	// F4: Return stable JSON success structure
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.APIResponse{Success: true, Message: "Workflow submitted successfully", WorkflowID: workflowID})
}
