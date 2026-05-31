package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/go-orca/go-orca/internal/api/middleware"
)

func TestOIDCBearerAuthRequiredRejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.OIDCBearerAuth(middleware.OIDCAuthConfig{
		UserinfoURL: "http://example.invalid/userinfo",
		Required:    true,
	}))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestOIDCBearerAuthRequiredAcceptsValidToken(t *testing.T) {
	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer good-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"user-abc"}`))
	}))
	defer userinfo.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.OIDCBearerAuth(middleware.OIDCAuthConfig{
		UserinfoURL:  userinfo.URL,
		Required:     true,
		UserIDHeader: "X-User-Id",
	}))
	var got string
	r.GET("/ok", func(c *gin.Context) {
		got = c.GetHeader("X-User-Id")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got != "user-abc" {
		t.Fatalf("X-User-Id = %q", got)
	}
}

func TestOIDCBearerAuthOptionalAllowsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.OIDCBearerAuth(middleware.OIDCAuthConfig{
		UserinfoURL: "http://example.invalid/userinfo",
		Required:    false,
	}))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
