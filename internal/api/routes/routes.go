// Package routes defines HTTP routes for the go-orca API server.
package routes

import (
	"github.com/gorilla/mux"
	"net/http"
	"github.com/example/go-orca/internal/api/handlers"
)

// SetupRoutes initializes and returns a configured router with all necessary routes defined.
func SetupRoutes() *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/workflows", handlers.GetWorkflows).Methods(http.MethodGet)
	r.HandleFunc("/workflows", handlers.PostWorkflows).Methods(http.MethodPost)

	// Add new routes for the dashboard here if necessary

	return r
}