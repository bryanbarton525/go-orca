// +build integration

package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-orca/go-orca/internal/api/handlers"
	"github.com/go-orca/go-orca/internal/models"
)

func TestGetWorkflows(t *testing.T) {
	// Setup test server
	r := httptest.NewRequest("GET", "/workflows", nil)
	r.Header.Set("X-Tenant-ID", "test-tenant")
	r.Header.Set("X-Scope-ID", "test-scope")

	recorder := httptest.NewRecorder()
	handlers.GetWorkflows(recorder, r)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status code %d; got %d", http.StatusOK, recorder.Code)
	}

	var workflows []models.Workflow
	err := json.Unmarshal(recorder.Body.Bytes(), &workflows)
	if err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	// Additional assertions can be added here based on expected behavior
}

func TestPostWorkflows(t *testing.T) {
	// Setup test server
	h := handlers.Handler{
		Store: &mockStore{},
	}

	workflow := models.Workflow{
		ID:   "test-workflow",
		Name: "Test Workflow",
	}

	body, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("Failed to marshal workflow data: %v", err)
	}

	r := httptest.NewRequest("POST", "/workflows", bytes.NewBuffer(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-ID", "test-tenant")
	r.Header.Set("X-Scope-ID", "test-scope")

	recorder := httptest.NewRecorder()
	h.PostWorkflows(recorder, r)

	if recorder.Code != http.StatusCreated {
		t.Errorf("Expected status code %d; got %d", http.StatusCreated, recorder.Code)
	}

	// Additional assertions can be added here based on expected behavior
}

func TestGetWorkflowsWithInvalidHeaders(t *testing.T) {
	// Setup test server
	r := httptest.NewRequest("GET", "/workflows", nil)
	r.Header.Set("X-Tenant-ID", "test-tenant") // Missing X-Scope-ID header

	recorder := httptest.NewRecorder()
	handlers.GetWorkflows(recorder, r)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d; got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestPostWorkflowsWithInvalidHeaders(t *testing.T) {
	// Setup test server
	h := handlers.Handler{
		Store: &mockStore{},
	}

	workflow := models.Workflow{
		ID:   "test-workflow",
		Name: "Test Workflow",
	}

	body, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("Failed to marshal workflow data: %v", err)
	}

	r := httptest.NewRequest("POST", "/workflows", bytes.NewBuffer(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-ID", "test-tenant") // Missing X-Scope-ID header

	recorder := httptest.NewRecorder()
	h.PostWorkflows(recorder, r)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d; got %d", http.StatusBadRequest, recorder.Code)
	}
}
