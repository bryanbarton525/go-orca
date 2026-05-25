// Package registry is the first-class MCP registry consumed by the workflow
// engine.  It is the only path through which the engine reaches MCP servers:
// no persona, executor, or engine code dials an MCP endpoint directly.
//
// The registry has two layers:
//
//   - Server layer: what MCP servers exist, their endpoint, transport, image,
//     advertised tools, and current health state.
//   - Toolchain layer: which MCP server backs which language stack and which
//     governed (capability → tool name) bindings form a validation profile.
//
// Together they expose a single resolution API:
//
//	res := reg.CallCapability(ctx, "go", "run_tests", args)
//
// The registry probes each server on connect, caches the advertised tool list,
// and refuses to invoke a capability whose target tool is not advertised.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/tools"
)

// ServerStatus describes one entry in the server layer of the registry.
type ServerStatus struct {
	Name            string    `json:"name"`
	Endpoint        string    `json:"endpoint,omitempty"`
	Transport       string    `json:"transport,omitempty"`
	Image           string    `json:"image,omitempty"`
	HealthPath      string    `json:"health_path,omitempty"`
	Required        bool      `json:"required"`
	Connected       bool      `json:"connected"`
	Healthy         bool      `json:"healthy"`
	AdvertisedTools []string  `json:"advertised_tools,omitempty"`
	LastSeen        time.Time `json:"last_seen,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

// ToolchainStatus describes one toolchain binding in the registry.
type ToolchainStatus struct {
	ID                   string              `json:"id"`
	Languages            []string            `json:"languages,omitempty"`
	MCPServer            string              `json:"mcp_server"`
	ServerReachable      bool                `json:"server_reachable"`
	Capabilities         []string            `json:"capabilities,omitempty"`
	CapabilityTools      map[string]string   `json:"capability_tools,omitempty"`
	MissingCapabilities  []string            `json:"missing_capabilities,omitempty"`
	ValidationProfiles   map[string][]string `json:"validation_profiles,omitempty"`
	CheckpointCapability string              `json:"checkpoint_capability,omitempty"`
	PushCheckpoints      bool                `json:"push_checkpoints,omitempty"`
}

// Snapshot is the response shape for GET /api/v1/mcp/registry.
type Snapshot struct {
	Servers    []ServerStatus    `json:"servers"`
	Toolchains []ToolchainStatus `json:"toolchains"`
}

// CallResult is the engine-facing outcome of a capability invocation.
// It mirrors the wire-level CapabilityResult but is parsed and sanitised so
// the engine doesn't re-marshal/re-parse JSON.
type CallResult struct {
	Passed   bool
	Output   string
	Stdout   string
	Stderr   string
	Error    string
	ToolName string
	Raw      json.RawMessage
}

// Toolchain is the registry-side representation of a toolchain config.
type Toolchain struct {
	ID                   string
	Languages            []string
	MCPServer            string
	Capabilities         []string
	CapabilityTools      map[string]string
	ValidationProfiles   map[string][]string
	CheckpointCapability string
	PushCheckpoints      bool
}

// ResolveError is returned when capability resolution fails.
type ResolveError struct {
	ToolchainID string
	Capability  string
	Reason      string
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("registry: resolve %s/%s: %s", e.ToolchainID, e.Capability, e.Reason)
}

// Registry is the live, in-memory view of MCP servers + toolchains.
type Registry struct {
	logger     *zap.Logger
	httpClient *http.Client
	tools      *tools.Registry
	connect    ConnectOptions

	mu         sync.RWMutex
	servers    map[string]*serverEntry
	toolchains map[string]Toolchain
	sessions   []*sdkmcp.ClientSession
}

type serverEntry struct {
	cfg          config.MCPServerConfig
	advertised   map[string]struct{} // tool names
	session      *sdkmcp.ClientSession
	connected    bool
	healthy      bool
	lastSeen     time.Time
	lastErr      string
	guidanceText string // cached prompts/resources for persona handoffs
}

// ServerNames returns connected MCP server names in stable sorted order.
func (r *Registry) ServerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.servers))
	for name, entry := range r.servers {
		if entry.connected {
			out = append(out, name)
		}
	}
	sortStrings(out)
	return out
}

// AdvertisedTools returns tool names advertised by the named MCP server.
func (r *Registry) AdvertisedTools(server string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.servers[server]
	if !ok {
		return nil, fmt.Errorf("mcp server %q not configured", server)
	}
	if !entry.connected {
		return nil, fmt.Errorf("mcp server %q is not connected", server)
	}
	out := make([]string, 0, len(entry.advertised))
	for name := range entry.advertised {
		out = append(out, name)
	}
	sortStrings(out)
	return out, nil
}

func sortStrings(ss []string) {
	for i := 0; i < len(ss); i++ {
		for j := i + 1; j < len(ss); j++ {
			if ss[j] < ss[i] {
				ss[i], ss[j] = ss[j], ss[i]
			}
		}
	}
}

// New constructs an empty Registry.  Call [Registry.LoadServers] and
// [Registry.LoadToolchains] from config, then [Registry.Probe] before serving
// requests.
func New(toolReg *tools.Registry, logger *zap.Logger) *Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Registry{
		logger:     logger,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		tools:      toolReg,
		servers:    make(map[string]*serverEntry),
		toolchains: make(map[string]Toolchain),
	}
}

// LoadServers connects to each server in cfg, registers its advertised tools
// into the underlying tool registry, and stores connection state.  Connection
// failures are logged and do not prevent startup; the affected servers are
// marked unhealthy and any toolchain that depends on them will fail to
// resolve at workflow start.  Each server is retried with backoff so a cold
// MCP pod at API startup does not stay disconnected permanently.
func (r *Registry) LoadServers(ctx context.Context, servers []config.MCPServerConfig) {
	for _, srv := range servers {
		entry := &serverEntry{cfg: srv, advertised: make(map[string]struct{})}
		r.mu.Lock()
		r.servers[srv.Name] = entry
		r.mu.Unlock()

		session, advertised, err := r.connectServerWithRetry(ctx, srv, r.connect)
		if err != nil {
			r.logger.Warn("mcp server load failed",
				zap.String("name", srv.Name),
				zap.Error(err),
			)
			r.markServerDisconnected(entry, err)
			r.mu.Lock()
			entry.healthy = false
			r.mu.Unlock()
			continue
		}

		r.applyConnectedSession(entry, session, advertised)
		r.cacheServerGuidance(ctx, srv.Name, session)
		r.logger.Info("mcp server connected",
			zap.String("name", srv.Name),
			zap.String("transport", srv.Transport),
			zap.Int("tools", len(advertised)),
		)
	}
}

// LoadToolchains stores toolchain bindings.  Capability tools are validated
// against advertised server tools; missing bindings are surfaced as warnings
// at startup and as ServerReachable=false / MissingCapabilities in the
// snapshot.
func (r *Registry) LoadToolchains(toolchains []Toolchain) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tc := range toolchains {
		r.toolchains[tc.ID] = tc
		entry, ok := r.servers[tc.MCPServer]
		if !ok {
			r.logger.Warn("toolchain references unknown mcp server",
				zap.String("toolchain", tc.ID),
				zap.String("mcp_server", tc.MCPServer),
			)
			continue
		}
		for _, cap := range tc.Capabilities {
			tool := tc.CapabilityTools[cap]
			if tool == "" {
				tool = cap
			}
			if _, ok := entry.advertised[tool]; !ok && entry.connected {
				r.logger.Warn("capability tool not advertised by mcp server",
					zap.String("toolchain", tc.ID),
					zap.String("capability", cap),
					zap.String("tool", tool),
					zap.String("mcp_server", tc.MCPServer),
				)
			}
		}
	}
}

// Probe pings each server's health_path (defaulting to /healthz) and updates
// per-server health.  When the HTTP endpoint is healthy but the MCP session
// is disconnected (common after a rolling deploy), Probe attempts reconnect
// with the same backoff policy as [Registry.LoadServers].
func (r *Registry) Probe(ctx context.Context) {
	r.mu.RLock()
	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sortStrings(names)

	for _, name := range names {
		r.mu.RLock()
		e := r.servers[name]
		r.mu.RUnlock()
		if e == nil {
			continue
		}

		healthy := e.connected
		errStr := ""

		if e.cfg.Endpoint != "" {
			if r.serverHTTPEndpointHealthy(ctx, e) {
				healthy = true
			} else {
				healthy = false
				errStr = "health probe failed"
			}
		}

		r.mu.Lock()
		e.healthy = healthy
		if healthy {
			e.lastSeen = time.Now().UTC()
		}
		if errStr != "" {
			e.lastErr = errStr
		}
		needsReconnect := !e.connected
		r.mu.Unlock()

		if needsReconnect {
			r.maybeReconnectServer(ctx, name, r.connect)
		}
	}
}

// Sessions returns all open MCP client sessions.  Callers must close each
// session at shutdown.
func (r *Registry) Sessions() []*sdkmcp.ClientSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*sdkmcp.ClientSession, len(r.sessions))
	copy(out, r.sessions)
	return out
}

// Toolchain returns the toolchain config for id.
func (r *Registry) Toolchain(id string) (Toolchain, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tc, ok := r.toolchains[id]
	return tc, ok
}

// Resolve maps (toolchainID, capability) to the underlying MCP tool name.
// Returns a [*ResolveError] when the toolchain is unknown, the capability
// is not bound, the target server is unreachable, or the tool is not
// advertised.
func (r *Registry) Resolve(toolchainID, capability string) (string, error) {
	return r.ResolveContext(context.Background(), toolchainID, capability)
}

// ResolveContext resolves a capability, attempting MCP reconnect when the
// backing server is down but its HTTP health endpoint responds.
func (r *Registry) ResolveContext(ctx context.Context, toolchainID, capability string) (string, error) {
	capability = strings.TrimSpace(capability)
	r.mu.RLock()
	tc, ok := r.toolchains[toolchainID]
	r.mu.RUnlock()
	if ok {
		r.maybeReconnectServer(ctx, tc.MCPServer, r.connect)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	tc, ok = r.toolchains[toolchainID]
	if !ok {
		return "", &ResolveError{ToolchainID: toolchainID, Capability: capability, Reason: "toolchain not configured"}
	}
	tool := tc.CapabilityTools[capability]
	if tool == "" {
		tool = capability
	}
	if tool == "" {
		return "", &ResolveError{ToolchainID: toolchainID, Capability: capability, Reason: "capability not bound to a tool"}
	}
	srv, ok := r.servers[tc.MCPServer]
	if !ok {
		return "", &ResolveError{ToolchainID: toolchainID, Capability: capability, Reason: "mcp server " + tc.MCPServer + " not registered"}
	}
	if !srv.connected {
		return "", &ResolveError{ToolchainID: toolchainID, Capability: capability, Reason: "mcp server " + tc.MCPServer + " unreachable"}
	}
	if _, ok := srv.advertised[tool]; !ok {
		return "", &ResolveError{ToolchainID: toolchainID, Capability: capability, Reason: fmt.Sprintf("tool %q not advertised by %s", tool, tc.MCPServer)}
	}
	return tool, nil
}

// CallCapability resolves and invokes a capability, returning a parsed
// [CallResult].  args is the JSON-encoded argument payload as produced by the
// engine's toolchainArgs helper.
func (r *Registry) CallCapability(ctx context.Context, toolchainID, capability string, args json.RawMessage) (CallResult, error) {
	toolName, err := r.ResolveContext(ctx, toolchainID, capability)
	if err != nil {
		return CallResult{}, err
	}
	res := r.tools.Call(ctx, toolName, args)
	if res.Error != "" {
		return CallResult{ToolName: toolName, Error: res.Error, Passed: false, Raw: res.Output}, nil
	}
	cr := CallResult{ToolName: toolName, Raw: res.Output, Passed: true}
	if len(res.Output) > 0 {
		var parsed struct {
			Success *bool  `json:"success"`
			Passed  *bool  `json:"passed"`
			Stdout  string `json:"stdout"`
			Stderr  string `json:"stderr"`
			Output  string `json:"output"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(res.Output, &parsed); err == nil {
			if parsed.Passed != nil {
				cr.Passed = *parsed.Passed
			} else if parsed.Success != nil {
				cr.Passed = *parsed.Success
			}
			cr.Stdout = parsed.Stdout
			cr.Stderr = parsed.Stderr
			cr.Output = parsed.Output
			cr.Error = parsed.Error
			if parsed.Error != "" {
				cr.Passed = false
			}
		}
	}
	return cr, nil
}

// ToolchainReachable reports an error when the MCP server backing a specific
// toolchain is unreachable.  Used at workflow start to fail fast rather than
// spin a workflow whose validation steps will all error.  Toolchains whose
// MCP server is not marked `required` are treated as soft-failures: the
// workflow is allowed to proceed (validation will still error per-step, but
// the engine doesn't refuse to start).
func (r *Registry) ToolchainReachable(toolchainID string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tc, ok := r.toolchains[toolchainID]
	if !ok {
		return fmt.Errorf("registry: toolchain %q not configured", toolchainID)
	}
	srv, ok := r.servers[tc.MCPServer]
	if !ok {
		return fmt.Errorf("registry: toolchain %q references unknown mcp server %q", toolchainID, tc.MCPServer)
	}
	if srv.connected {
		return nil
	}
	if !srv.cfg.Required {
		// Server is down but not required — let the workflow proceed; the
		// per-capability resolver will surface the failure on each call.
		return nil
	}
	reason := srv.lastErr
	if reason == "" {
		reason = "not connected"
	}
	return fmt.Errorf("registry: required mcp server %q for toolchain %q is unreachable: %s", tc.MCPServer, toolchainID, reason)
}

// RequiredServersReachable reports an error if any required MCP server is
// unreachable. Used at workflow start to fail fast rather than spin a
// workflow whose validation steps will all error.
func (r *Registry) RequiredServersReachable() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var missing []string
	for name, e := range r.servers {
		if e.cfg.Required && !e.connected {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return errors.New("required MCP servers unreachable: " + strings.Join(missing, ", "))
	}
	return nil
}

// SnapshotJSON returns the registry snapshot for the API endpoint.
func (r *Registry) SnapshotJSON() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	servers := make([]ServerStatus, 0, len(r.servers))
	for _, e := range r.servers {
		ad := make([]string, 0, len(e.advertised))
		for n := range e.advertised {
			ad = append(ad, n)
		}
		servers = append(servers, ServerStatus{
			Name:            e.cfg.Name,
			Endpoint:        e.cfg.Endpoint,
			Transport:       e.cfg.Transport,
			Image:           e.cfg.Image,
			HealthPath:      e.cfg.HealthPath,
			Required:        e.cfg.Required,
			Connected:       e.connected,
			Healthy:         e.healthy,
			AdvertisedTools: ad,
			LastSeen:        e.lastSeen,
			LastError:       e.lastErr,
		})
	}
	tcs := make([]ToolchainStatus, 0, len(r.toolchains))
	for _, tc := range r.toolchains {
		srv, ok := r.servers[tc.MCPServer]
		reachable := ok && srv.connected
		var missing []string
		if ok {
			for _, cap := range tc.Capabilities {
				tool := tc.CapabilityTools[cap]
				if tool == "" {
					tool = cap
				}
				if _, has := srv.advertised[tool]; !has {
					missing = append(missing, cap)
				}
			}
		}
		tcs = append(tcs, ToolchainStatus{
			ID:                   tc.ID,
			Languages:            tc.Languages,
			MCPServer:            tc.MCPServer,
			ServerReachable:      reachable,
			Capabilities:         tc.Capabilities,
			CapabilityTools:      tc.CapabilityTools,
			MissingCapabilities:  missing,
			ValidationProfiles:   tc.ValidationProfiles,
			CheckpointCapability: tc.CheckpointCapability,
			PushCheckpoints:      tc.PushCheckpoints,
		})
	}
	return Snapshot{Servers: servers, Toolchains: tcs}
}
