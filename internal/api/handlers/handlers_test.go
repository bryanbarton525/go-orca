// Package handlers provides HTTP request handling for go-orca.
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetWorkflows(t *testing.T) {
	h := http.HandlerFunc(GetWorkflows)
	req, err := http.NewRequest("GET", "/workflows", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Tenant-ID", "test-tenant")
	req.Header.Set("X-Scope-ID", "test-scope")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status code 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "pending") || !strings.Contains(rec.Body.String(), "running") || !strings.Contains(rec.Body.String(), "paused") {
		t.Error("response does not contain in-flight statuses")
	}
}

func TestPostWorkflows(t *testing.T) {
	h := http.HandlerFunc(PostWorkflows)
	body := `{
		"name": "test-workflow",
		"status": "pending"
	}`
	req, err := http.NewRequest("POST", "/workflows", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "test-tenant")
	req.Header.Set("X-Scope-ID", "test-scope")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status code 201, got %d", rec.Code)
	}
}