package models

// Workflow represents the core structure for a running workflow instance.
type Workflow struct {
	WorkflowID string    `json:"workflow_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"` // e.g., PENDING, RUNNING, PAUSED
	SubmittedAt string `json:"submitted_at"`
	Progress   *float64  `json:"progress"` // Optional: numeric progress value
	Details    *string   `json:"details"` // Optional details string
}

// SubmitRequest defines the payload structure for submitting a new workflow.
type SubmitRequest struct {
	WorkflowName string `json:"workflow_name"`
	Parameters   string `json:"parameters"`
}

// APIResponse standardizes the success payload structure for general API responses.
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// APIError standardizes the error payload structure for consistent JSON error handling.
type APIError struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code"`
}
