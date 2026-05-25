package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/tools"
)

func TestConnectOptions_withDefaults(t *testing.T) {
	opts := ConnectOptions{}.withDefaults()
	if opts.MaxAttempts != defaultConnectMaxAttempts {
		t.Fatalf("MaxAttempts=%d want %d", opts.MaxAttempts, defaultConnectMaxAttempts)
	}
	if opts.Backoff != defaultConnectBackoff {
		t.Fatalf("Backoff=%v want %v", opts.Backoff, defaultConnectBackoff)
	}

	custom := ConnectOptions{MaxAttempts: 3, Backoff: 500 * time.Millisecond}.withDefaults()
	if custom.MaxAttempts != 3 || custom.Backoff != 500*time.Millisecond {
		t.Fatalf("custom defaults not preserved: %+v", custom)
	}
}

func TestHealthCheckURL(t *testing.T) {
	u, err := healthCheckURL(config.MCPServerConfig{
		Endpoint: "http://mcp-git:3000/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u != "http://mcp-git:3000/healthz" {
		t.Fatalf("got %q", u)
	}

	u, err = healthCheckURL(config.MCPServerConfig{
		Endpoint:   "http://mcp-git:3000/mcp",
		HealthPath: "/ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u != "http://mcp-git:3000/ready" {
		t.Fatalf("got %q", u)
	}
}

func TestServerHTTPEndpointHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := New(tools.NewRegistry(), nil)
	entry := &serverEntry{
		cfg: config.MCPServerConfig{
			Endpoint: srv.URL + "/mcp",
		},
	}
	if !r.serverHTTPEndpointHealthy(context.Background(), entry) {
		t.Fatal("expected healthy endpoint")
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	entry.cfg.Endpoint = down.URL + "/mcp"
	if r.serverHTTPEndpointHealthy(context.Background(), entry) {
		t.Fatal("expected unhealthy when health returns 503")
	}
}
