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

// OIDCAuthConfig configures Bearer token validation via OIDC userinfo.
type OIDCAuthConfig struct {
	// UserinfoURL is the OIDC userinfo endpoint (Zitadel, Authentik, etc.).
	UserinfoURL string
	// Required rejects requests without a valid Bearer token when UserinfoURL is set.
	Required bool
	// UserIDHeader is set to the token subject after validation (default X-User-Id).
	UserIDHeader string
	// Timeout bounds the userinfo HTTP call.
	Timeout time.Duration
}

// OIDCBearerAuth validates Authorization: Bearer tokens against OIDC userinfo.
// When Required is true, missing or invalid tokens receive 401.
// When Required is false, requests without Bearer pass through unchanged.
func OIDCBearerAuth(cfg OIDCAuthConfig) gin.HandlerFunc {
	userinfoURL := strings.TrimSpace(cfg.UserinfoURL)
	hdr := strings.TrimSpace(cfg.UserIDHeader)
	if hdr == "" {
		hdr = "X-User-Id"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	return func(c *gin.Context) {
		if userinfoURL == "" {
			c.Next()
			return
		}

		token := BearerToken(c.GetHeader("Authorization"))
		if token == "" {
			if cfg.Required {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
				return
			}
			c.Next()
			return
		}

		userID, err := FetchUserinfoSubject(c.Request.Context(), client, userinfoURL, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
			return
		}

		c.Set("mesh_user_id", userID)
		c.Set("oidc_subject", userID)
		c.Request.Header.Set(hdr, userID)
		c.Next()
	}
}

// BearerToken extracts the token from an Authorization: Bearer header.
func BearerToken(raw string) string {
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

// FetchUserinfoSubject calls OIDC userinfo and returns the sub claim.
func FetchUserinfoSubject(ctx context.Context, client *http.Client, userinfoURL, token string) (string, error) {
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
