package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// StreamingBearerUserinfo resolves X-User-Id from a Bearer token via OIDC userinfo
// when Istio did not map a JWT (for example ZITADEL opaque access tokens or PATs).
// Prefer applying OIDCBearerAuth on the parent /api/v1 group when server.oidc is configured.
func StreamingBearerUserinfo(userinfoURL, userIDHeader string) gin.HandlerFunc {
	userinfoURL = strings.TrimSpace(userinfoURL)
	hdr := strings.TrimSpace(userIDHeader)
	if hdr == "" {
		hdr = "X-User-Id"
	}
	client := &http.Client{Timeout: 5 * time.Second}

	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader(hdr)) != "" {
			c.Next()
			return
		}
		if existing := strings.TrimSpace(c.GetString("mesh_user_id")); existing != "" {
			c.Request.Header.Set(hdr, existing)
			c.Next()
			return
		}
		if userinfoURL == "" {
			c.Next()
			return
		}

		token := BearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.Next()
			return
		}

		userID, err := FetchUserinfoSubject(c.Request.Context(), client, userinfoURL, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
			return
		}

		c.Set("mesh_user_id", userID)
		c.Request.Header.Set(hdr, userID)
		c.Next()
	}
}
