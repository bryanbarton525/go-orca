package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStreamingBearerUserinfoSetsMeshUserID(t *testing.T) {
	t.Parallel()

	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer opaque-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"sub":"user-123"}`))
	}))
	defer userinfo.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/events", StreamingBearerUserinfo(userinfo.URL, "X-User-Id"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user": c.GetString("mesh_user_id")})
	})

	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	req.Header.Set("Authorization", "Bearer opaque-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "user-123") {
		t.Fatalf("expected mesh user in body, got %s", w.Body.String())
	}
}
