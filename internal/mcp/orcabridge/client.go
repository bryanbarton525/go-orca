// Package orcabridge implements the go-orca MCP bridge for external agent offload.
package orcabridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds bridge → API client settings.
type Config struct {
	BaseURL         string
	APIKey          string
	DefaultTenantID string
	DefaultScopeID  string
	HTTPTimeout     time.Duration
}

// Client calls go-orca-api REST endpoints.
type Client struct {
	cfg    Config
	client *http.Client
}

// NewClient constructs an API client.
func NewClient(cfg Config) *Client {
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 120 * time.Second
	}
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, tenantID, scopeID string, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	if tenantID == "" {
		tenantID = c.cfg.DefaultTenantID
	}
	if scopeID == "" {
		scopeID = c.cfg.DefaultScopeID
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if scopeID != "" {
		req.Header.Set("X-Scope-ID", scopeID)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("api %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
