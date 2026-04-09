package handler

import (
	"encoding/json"
	"net/http"
	"go-orca/models"
	"go-orca/service"
)

// writeJSONError writes a standardized JSON error response to the http.ResponseWriter
func writeJSONError(w http.ResponseWriter, statusCode int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(models.APIError{ErrorMessage: err.Error()})
}

// SubmitWorkflow handles the submission of a new workflow request.
// It expects the request body to contain the data needed to construct a Workflow.
func SubmitWorkflow(svc service.WorkflowServiceInterface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req models.SubmitRequest
		// 1. Decode Request Body
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err)
			return
		}

		// 2. Input Validation (F3)
		if req.WorkflowName == "" || req.Parameters == nil {
			writeJSONError(w, http.StatusBadRequest, "WorkflowName and Parameters are required.")
			return
		}

		// 3. Build Canonical Model from validated input
		workflow := models.Workflow{
			WorkflowName: req.WorkflowName,
			Parameters:   req.Parameters,
			Status:       models.StatusPending, // Newly submitted workflows start as Pending
			// Assuming other fields like ID and SubmittedAt are populated by the service layer
		} 

		// 4. Service Call (using mockable service interface)
		newWorkflow, err := svc.ExecuteWorkflow(workflow)
		
		if err != nil {
			// Handle service layer errors using structured JSON error
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		// 5. Success Response (F4)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.APIResponse{Success: true, Data: newWorkflow})
	}
}
