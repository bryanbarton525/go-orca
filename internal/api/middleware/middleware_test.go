package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/go-orca/go-orca/internal/api/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequireMeshUserIDAcceptsCanonicalizedHeader(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequireMeshUserID("X-User-Id"))
	r.POST("/ingest", func(c *gin.Context) { c.Status(http.StatusAccepted) })

	req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	// Simulate Envoy/Istio lowercase output; net/http canonicalizes internally.
	req.Header.Set("x-user-id", "user-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestRequireMeshUserIDRejectsMissingHeader(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequireMeshUserID("X-User-Id"))
	r.POST("/ingest", func(c *gin.Context) { c.Status(http.StatusAccepted) })

	req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
