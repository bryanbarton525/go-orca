package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// StreamingBearerUserinfo resolves X-User-Id from a Bearer token via OIDC userinfo
// when Istio did not map a JWT (for example ZITADEL opaque access tokens or PATs).
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
			c.Next()
			return
		}
		if userinfoURL == "" {
			c.Next()
			return
		}

		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.Next()
			return
		}

		userID, err := fetchUserinfoSubject(c.Request.Context(), client, userinfoURL, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
			return
		}

		c.Set("mesh_user_id", userID)
		c.Request.Header.Set(hdr, userID)
		c.Next()
	}
}

func bearerToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(raw[len(prefix):])
}

func fetchUserinfoSubject(ctx context.Context, client *http.Client, userinfoURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned %d", resp.StatusCode)
	}

	var payload struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	sub := strings.TrimSpace(payload.Sub)
	if sub == "" {
		return "", fmt.Errorf("userinfo missing sub")
	}
	return sub, nil
}
