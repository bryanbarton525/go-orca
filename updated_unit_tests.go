package dashboard_test

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

type mockServer struct {
    handler http.Handler
}

func (ms *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ms.handler.ServeHTTP(w, r)
}

func TestFetchWorkflows(t *testing.T) {
    testCases := []struct {
        name          string
        expectedCount int
        status        string
    }{
        {"All Workflows", 3, ""},
        {"Pending Workflows", 1, "pending"},
        {"Running Workflows", 1, "running"},
        {"Paused Workflows", 1, "paused"},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                // Mock response based on tc.status
                workflows := []string{"pending", "running", "paused"}
                var filteredWorkflows []string
                if tc.status == "" {
                    filteredWorkflows = workflows
                } else {
                    for _, workflow := range workflows {
                        if workflow == tc.status {
                            filteredWorkflows = append(filteredWorkflows, workflow)
                        }
                    }
                }
                w.Write([]byte(`[{