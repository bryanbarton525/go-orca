package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-orca/models"
	"go-orca/services"
)

// MockService implements the services.WorkflowService interface for testing purposes.
type MockService struct {}

// ListWorkflows mocks the service call for listing workflows.
func (m *MockService) ListWorkflows() ([]models.Workflow, error) {
	return nil, nil
}

// TestWorkflowHandler_ListWorkflows fully tests the ListWorkflows handler
testCase := []struct {
	name               string
	setupService       func(*MockService, []models.Workflow, error)
	expectedStatusCode int
	expectedBodyCheck  func(t *testing.T, body []byte) bool
}{
	{
		name: "Success_ReturnsWorkflows",
		setupService: func(m *MockService, workflows []models.Workflow, err error) {
		m.MockService = &MockService{mockWorkflows: workflows, mockError: err}
	},
		expectedStatusCode: http.StatusOK,
	expectedBodyCheck: func(t *testing.T, body []byte) bool {
		var apiResponse models.APIResponse
		if err := json.Unmarshal(body, &apiResponse); err != nil { return false }
		if apiResponse.Data == nil || len(apiResponse.Data) == 0 { return false }
		// Check if the returned workflows match the expected count
		return len(apiResponse.Data) == 2
	}},
	},
	{
		name: "EmptyList_ReturnsSuccessWithEmptyData",
		setupService: func(m *MockService, workflows []models.Workflow, err error) {
		m.MockService = &MockService{mockWorkflows: workflows, mockError: err}
	},
		expectedStatusCode: http.StatusOK,
	expectedBodyCheck: func(t *testing.T, body []byte) bool {
		var apiResponse models.APIResponse
		if err := json.Unmarshal(body, &apiResponse); err != nil { return false }
		// Should return an empty, but structurally valid, list
		return apiResponse.Data != nil && len(apiResponse.Data) == 0
	}},
	{
		name: "ServiceError_ReturnsInternalError",
		setupService: func(m *MockService, workflows []models.Workflow, err error) {
		m.MockService = &MockService{mockWorkflows: workflows, mockError: err}
	},
		expectedStatusCode: http.StatusInternalServerError,
	expectedBodyCheck: func(t *testing.T, body []byte) bool {
		var apiResponse models.APIResponse
		if err := json.Unmarshal(body, &apiResponse); err != nil { return false }
		// Check that the error message is present
		return apiResponse.Error != nil && apiResponse.Error == "Failed to retrieve workflows"
	}},
	},
{}
]

func TestWorkflowHandler_ListWorkflows(t *testing.T) {
	// Setup mocks and handlers
	mockService := &MockService{}
	handler := handler.NewWorkflowHandler(mockService)

	for _, tt := range testCase {
		t.Parallel()
		// Setup test-specific service state
		t.setupService(mockService, nil, nil)

		// Create request and response recorder
		req, _ := http.NewRequest(http.MethodGet, "/api/workflows/list", nil)
		recorder := httptest.NewRecorder()

		// Execute the handler
		http.HandlerFunc(handler.ListWorkflows).ServeHTTP(recorder, req)

		// 1. Check Status Code
	if recorder.Code != tt.expectedStatusCode {
		t.Errorf("Expected status code %d, got %d. Body: %s", tt.expectedStatusCode, recorder.Code, recorder.Body.String())
		continue
	}

		// 2. Check Body Content
	if !tt.expectedBodyCheck(t, recorder.Body.Bytes()) {
		t.Errorf("Body content validation failed. Expected structure/content mismatch. Body: %s", recorder.Body.String())
	}
	}
}
