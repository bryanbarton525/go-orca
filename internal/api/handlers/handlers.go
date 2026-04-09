package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

func fetchWorkflows(w http.ResponseWriter, r *http.Request) {
	enctype := r.Header.Get("Content-Type")
	if enctype != "application/json" {
		http.Error(w, "Unsupported Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	tenantID := r.Header.Get("X-Tenant-ID")
	scopeID := r.Header.Get("X-Scope-ID")

	workflows := []Workflow{
		{ID: 1, Status: "pending"},
		{ID: 2, Status: "running"},
		{ID: 3, Status: "paused"},
	}

	var filteredWorkflows []Workflow
	for _, workflow := range workflows {
		if (tenantID == "" || strings.EqualFold(workflow.TenantID, tenantID)) && (scopeID == "" || strings.EqualFold(workflow.ScopeID, scopeID)) && isInFlightStatus(workflow.Status) {
			filteredWorkflows = append(filteredWorkflows, workflow)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filteredWorkflows)
}

func isInFlightStatus(status string) bool {
	return status == "pending" || status == "running" || status == "paused"
}

type Workflow struct {
	ID       int    `json:"id"`
	Status   string `json:"status"`
	TenantID string `json:"tenant_id,omitempty"`
	ScopeID  string `json:"scope_id,omitempty"`
}