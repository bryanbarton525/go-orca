package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/config"
	mcpclient "github.com/go-orca/go-orca/internal/tools/mcp"
)

const (
	defaultConnectMaxAttempts = 8
	defaultConnectBackoff     = 2 * time.Second
	defaultProbeInterval      = 15 * time.Second
)

// ConnectOptions tunes MCP session establishment retries.
type ConnectOptions struct {
	MaxAttempts int
	Backoff     time.Duration
}

func (o ConnectOptions) withDefaults() ConnectOptions {
	out := o
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaultConnectMaxAttempts
	}
	if out.Backoff <= 0 {
		out.Backoff = defaultConnectBackoff
	}
	return out
}

// StartProbeLoop periodically runs [Registry.Probe], which health-checks each
// server and reconnects when the HTTP endpoint is up but the MCP session is down.
func (r *Registry) StartProbeLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultProbeInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Probe(ctx)
			}
		}
	}()
}

func (r *Registry) connectServer(ctx context.Context, srv config.MCPServerConfig) (*sdkmcp.ClientSession, map[string]struct{}, error) {
	opts := mcpclient.LoaderOptions{
		HTTPTimeout:   srv.HTTPTimeout,
		TLSSkipVerify: srv.TLSSkipVerify,
	}
	var (
		session *sdkmcp.ClientSession
		err     error
	)
	switch srv.Transport {
	case "sse":
		opts.HTTPTransport = mcpclient.TransportSSE
		session, err = mcpclient.Load(ctx, r.tools, srv.Endpoint, opts)
	case "command", "stdio":
		session, err = mcpclient.LoadCommand(ctx, r.tools, srv.Command, srv.Args, opts)
	default:
		session, err = mcpclient.Load(ctx, r.tools, srv.Endpoint, opts)
	}
	if err != nil {
		return nil, nil, err
	}

	advertised := make(map[string]struct{})
	listed, listErr := session.ListTools(ctx, nil)
	if listErr != nil {
		_ = session.Close()
		return nil, nil, listErr
	}
	for _, t := range listed.Tools {
		advertised[t.Name] = struct{}{}
	}
	return session, advertised, nil
}

func (r *Registry) connectServerWithRetry(ctx context.Context, srv config.MCPServerConfig, opts ConnectOptions) (*sdkmcp.ClientSession, map[string]struct{}, error) {
	opts = opts.withDefaults()
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		session, advertised, err := r.connectServer(ctx, srv)
		if err == nil {
			if attempt > 1 {
				r.logger.Info("mcp server connected after retry",
					zap.String("name", srv.Name),
					zap.Int("attempt", attempt),
				)
			}
			return session, advertised, nil
		}
		lastErr = err
		if attempt < opts.MaxAttempts {
			r.logger.Warn("mcp server connect attempt failed, retrying",
				zap.String("name", srv.Name),
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", opts.MaxAttempts),
				zap.Error(err),
			)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(opts.Backoff):
			}
		}
	}
	return nil, nil, fmt.Errorf("after %d attempts: %w", opts.MaxAttempts, lastErr)
}

func (r *Registry) applyConnectedSession(entry *serverEntry, session *sdkmcp.ClientSession, advertised map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.session != nil && entry.session != session {
		_ = entry.session.Close()
		r.removeSessionLocked(entry.session)
	}
	entry.session = session
	entry.connected = true
	entry.healthy = true
	entry.lastSeen = time.Now().UTC()
	entry.advertised = advertised
	entry.lastErr = ""
	if session != nil {
		r.sessions = append(r.sessions, session)
	}
}

func (r *Registry) removeSessionLocked(session *sdkmcp.ClientSession) {
	for i, s := range r.sessions {
		if s == session {
			r.sessions = append(r.sessions[:i], r.sessions[i+1:]...)
			return
		}
	}
}

func (r *Registry) markServerDisconnected(entry *serverEntry, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.session != nil {
		_ = entry.session.Close()
		r.removeSessionLocked(entry.session)
		entry.session = nil
	}
	entry.connected = false
	if err != nil {
		entry.lastErr = err.Error()
	}
}

func (r *Registry) maybeReconnectServer(ctx context.Context, name string, opts ConnectOptions) {
	r.mu.RLock()
	entry, ok := r.servers[name]
	if !ok || entry.connected {
		r.mu.RUnlock()
		return
	}
	// Command/stdio transports have no HTTP health URL; retry connect directly.
	hasHTTP := entry.cfg.Endpoint != ""
	r.mu.RUnlock()

	if hasHTTP && !r.serverHTTPEndpointHealthy(ctx, entry) {
		return
	}

	opts = opts.withDefaults()
	r.mu.RLock()
	srv := entry.cfg
	r.mu.RUnlock()

	session, advertised, err := r.connectServerWithRetry(ctx, srv, opts)
	if err != nil {
		r.logger.Warn("mcp server reconnect failed",
			zap.String("name", name),
			zap.Error(err),
		)
		r.mu.Lock()
		if e, ok := r.servers[name]; ok {
			e.lastErr = err.Error()
		}
		r.mu.Unlock()
		return
	}

	r.mu.RLock()
	entry, ok = r.servers[name]
	r.mu.RUnlock()
	if !ok {
		_ = session.Close()
		return
	}
	r.applyConnectedSession(entry, session, advertised)
	r.cacheServerGuidance(ctx, name, session)
	r.logger.Info("mcp server reconnected",
		zap.String("name", name),
		zap.Int("tools", len(advertised)),
	)
}

func (r *Registry) serverHTTPEndpointHealthy(ctx context.Context, entry *serverEntry) bool {
	u, err := healthCheckURL(entry.cfg)
	if err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func healthCheckURL(srv config.MCPServerConfig) (string, error) {
	if srv.Endpoint == "" {
		return "", fmt.Errorf("no endpoint")
	}
	u, err := url.Parse(srv.Endpoint)
	if err != nil {
		return "", err
	}
	path := srv.HealthPath
	if path == "" {
		path = "/healthz"
	}
	u.Path = path
	return u.String(), nil
}

// SetConnectOptions configures MCP session retry backoff used by
// [Registry.LoadServers], [Registry.Probe], and [Registry.ResolveContext].
func (r *Registry) SetConnectOptions(opts ConnectOptions) {
	r.connect = opts.withDefaults()
}
