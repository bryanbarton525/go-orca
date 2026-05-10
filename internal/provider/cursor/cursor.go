// Package cursor implements the gorca Provider interface using Cursor's
// Cloud Agents HTTP API (https://api.cursor.com/v1).
//
// It does not advertise CapabilityToolCalling: the Cloud Agent runs tools in
// Cursor's environment; go-orca's Phase-A MCP tool loop is skipped for this
// provider. Structured JSON relies on prompt instructions plus the executor's
// JSON extraction.
package cursor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

const (
	// ProviderName is the registry key for this provider.
	ProviderName = "cursor"

	metaWorkflowID    = "workflow_id"
	metaCursorAgentID = "cursor_agent_id"
	metaRepoURL       = "repo_url"
	metaStartingRef   = "starting_ref"

	// SessionHintCursorAgentID is the ChatResponse.SessionHints key persisted
	// by the engine / executor.
	SessionHintCursorAgentID = "cursor_agent_id"

	defaultBaseURL = "https://api.cursor.com"
)

// Provider calls Cursor Cloud Agents API.
type Provider struct {
	common.BaseProvider
	httpClient *http.Client
	baseURL    string
	cfg        config.CursorConfig
	timeout    time.Duration
}

// New constructs a Cursor Cloud provider. Caller must common.Register(p).
func New(cfg config.CursorConfig) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("cursor: api_key is required")
	}
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &Provider{
		BaseProvider: common.NewBaseProvider(
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityModelList,
		),
		httpClient: &http.Client{Timeout: 0}, // per-request timeouts via context
		baseURL:    base,
		cfg:        cfg,
		timeout:    timeout,
	}, nil
}

func (p *Provider) withChatTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, p.timeout)
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

func (p *Provider) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(p.cfg.APIKey+":"))
}

func (p *Provider) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", p.authHeader())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cursor: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("cursor: decode %s: %w", path, err)
		}
	}
	return nil
}

// Models implements Provider.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := p.doJSON(ctx, http.MethodGet, "/v1/models", nil, &envelope); err != nil {
		return nil, err
	}
	caps := []common.Capability{common.CapabilityChat, common.CapabilityStreaming, common.CapabilityModelList}
	infos := make([]common.ModelInfo, 0, len(envelope.Items))
	for _, raw := range envelope.Items {
		id := decodeModelsItemID(raw)
		if id == "" {
			continue
		}
		infos = append(infos, common.ModelInfo{
			ID:           id,
			Name:         id,
			Description:  "Cursor Cloud Agents model",
			Capabilities: caps,
		})
	}
	return infos, nil
}

func decodeModelsItemID(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return strings.TrimSpace(obj.ID)
	}
	return ""
}

// HealthCheck implements Provider.
func (p *Provider) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.doJSON(ctx, http.MethodGet, "/v1/me", nil, &map[string]any{})
}

type modelRef struct {
	ID     string           `json:"id"`
	Params []modelParamItem `json:"params,omitempty"`
}

type modelParamItem struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type repoConfig struct {
	URL         string `json:"url,omitempty"`
	StartingRef string `json:"startingRef,omitempty"`
}

type promptObj struct {
	Text string `json:"text"`
}

type createAgentBody struct {
	Prompt       promptObj    `json:"prompt"`
	Model        *modelRef    `json:"model,omitempty"`
	Repos        []repoConfig `json:"repos"`
	AutoCreatePR *bool        `json:"autoCreatePR,omitempty"`
}

type createAgentResponse struct {
	Agent struct {
		ID string `json:"id"`
	} `json:"agent"`
	Run runRef `json:"run"`
}

type runRef struct {
	ID string `json:"id"`
}

type createRunBody struct {
	Prompt promptObj `json:"prompt"`
	Model  *modelRef `json:"model,omitempty"`
}

type createRunResponse struct {
	Run runRef `json:"run"`
}

func (p *Provider) resolveRepo(req common.ChatRequest) (url, ref string, err error) {
	if req.Metadata != nil {
		if u := strings.TrimSpace(req.Metadata[metaRepoURL]); u != "" {
			url = u
			ref = strings.TrimSpace(req.Metadata[metaStartingRef])
			if ref == "" {
				ref = strings.TrimSpace(p.cfg.StartingRef)
			}
			return url, ref, nil
		}
	}
	if u := strings.TrimSpace(p.cfg.RepoURL); u != "" {
		return u, strings.TrimSpace(p.cfg.StartingRef), nil
	}
	return "", "", fmt.Errorf("cursor: repo URL is required (set providers.cursor.repo_url or pass metadata %q, or ensure workspace.repo_url is set)", metaRepoURL)
}

func (p *Provider) resolveModel(req common.ChatRequest) *modelRef {
	m := strings.TrimSpace(req.Model)
	if m == "" {
		m = strings.TrimSpace(p.cfg.DefaultModel)
	}
	if m == "" {
		return nil
	}
	return &modelRef{ID: m}
}

func serializeMessages(msgs []common.Message, jsonMode bool, outputSchema map[string]any, schemaName string) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case common.RoleSystem:
			fmt.Fprintf(&b, "### system\n%s\n\n", m.Content)
		case common.RoleUser:
			fmt.Fprintf(&b, "### user\n%s\n\n", m.Content)
		case common.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				raw, _ := json.Marshal(m.ToolCalls)
				fmt.Fprintf(&b, "### assistant (tool_calls)\n%s\n\n", string(raw))
			}
			if strings.TrimSpace(m.Content) != "" {
				fmt.Fprintf(&b, "### assistant\n%s\n\n", m.Content)
			}
		case common.RoleTool:
			fmt.Fprintf(&b, "### tool result (id=%s)\n%s\n\n", m.ToolCallID, m.Content)
		default:
			fmt.Fprintf(&b, "### %s\n%s\n\n", m.Role, m.Content)
		}
	}
	if jsonMode || outputSchema != nil {
		b.WriteString("\n---\n## Output instructions\nYou must follow the JSON-only constraints given in the system/user messages above.\n")
		if len(outputSchema) > 0 {
			name := strings.TrimSpace(schemaName)
			if name == "" {
				name = "response"
			}
			schemaJSON, _ := json.Marshal(outputSchema)
			fmt.Fprintf(&b, "Target JSON schema name: %s\nSchema:\n%s\n", name, string(schemaJSON))
		}
	}
	return strings.TrimSpace(b.String())
}

func (p *Provider) createAgent(ctx context.Context, req common.ChatRequest, promptText string) (agentID, runID string, err error) {
	repoURL, startingRef, err := p.resolveRepo(req)
	if err != nil {
		return "", "", err
	}
	f := false
	body := createAgentBody{
		Prompt: promptObj{Text: promptText},
		Model:  p.resolveModel(req),
		Repos: []repoConfig{{
			URL:         repoURL,
			StartingRef: startingRef,
		}},
		AutoCreatePR: &f,
	}
	if p.cfg.AutoCreatePR {
		t := true
		body.AutoCreatePR = &t
	}
	var resp createAgentResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v1/agents", body, &resp); err != nil {
		return "", "", err
	}
	agentID = strings.TrimSpace(resp.Agent.ID)
	runID = strings.TrimSpace(resp.Run.ID)
	if agentID == "" || runID == "" {
		return "", "", fmt.Errorf("cursor: create agent: missing agent or run id")
	}
	return agentID, runID, nil
}

func (p *Provider) createRun(ctx context.Context, agentID string, req common.ChatRequest, promptText string) (runID string, err error) {
	body := createRunBody{
		Prompt: promptObj{Text: promptText},
		Model:  p.resolveModel(req),
	}
	var resp createRunResponse
	path := "/v1/agents/" + agentID + "/runs"
	const maxBusy = 6
	var lastErr error
	for attempt := 0; attempt < maxBusy; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		lastErr = p.doJSON(ctx, http.MethodPost, path, body, &resp)
		if lastErr == nil {
			runID = strings.TrimSpace(resp.Run.ID)
			if runID == "" {
				return "", fmt.Errorf("cursor: create run: missing run id")
			}
			return runID, nil
		}
		if !strings.Contains(strings.ToLower(lastErr.Error()), "409") &&
			!strings.Contains(strings.ToLower(lastErr.Error()), "agent_busy") {
			break
		}
	}
	return "", lastErr
}

// assistantPayload matches SSE data for event type "assistant".
type assistantPayload struct {
	Text string `json:"text"`
}

func (p *Provider) streamRun(ctx context.Context, agentID, runID string) (string, error) {
	path := fmt.Sprintf("/v1/agents/%s/runs/%s/stream", agentID, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", p.authHeader())
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("cursor: stream: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out strings.Builder
	sc := bufio.NewScanner(resp.Body)
	// Allow long lines in SSE data.
	const maxBuf = 8 << 20
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxBuf)

	var curEvent string
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			curEvent = ""
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		switch curEvent {
		case "assistant":
			var ap assistantPayload
			if err := json.Unmarshal([]byte(payload), &ap); err == nil && ap.Text != "" {
				out.WriteString(ap.Text)
			}
		case "thinking":
			if !p.cfg.IncludeThinkingSSE {
				break
			}
			var ap assistantPayload
			if err := json.Unmarshal([]byte(payload), &ap); err == nil && ap.Text != "" {
				out.WriteString(ap.Text)
			}
		case "error":
			var ep struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}
			_ = json.Unmarshal([]byte(payload), &ep)
			if ep.Message != "" {
				return fmt.Errorf("cursor: stream error: %s (%s)", ep.Message, ep.Code)
			}
		}
		curEvent = ""
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return "", err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			if err := flush(); err != nil {
				return "", err
			}
			curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			dataLines = append(dataLines, data)
			continue
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("cursor: stream read: %w", err)
	}
	_ = flush()
	return out.String(), nil
}

// Chat implements Provider.
func (p *Provider) Chat(ctx context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	start := time.Now()
	ctx, cancel := p.withChatTimeout(ctx)
	defer cancel()

	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	promptText := serializeMessages(req.Messages, req.JSONMode, req.OutputSchema, req.SchemaName)

	agentID := strings.TrimSpace(req.Metadata[metaCursorAgentID])
	var runID string
	var err error
	var sessionHints map[string]string

	if agentID == "" {
		agentID, runID, err = p.createAgent(ctx, req, promptText)
		if err != nil {
			return nil, err
		}
		sessionHints = map[string]string{SessionHintCursorAgentID: agentID}
	} else {
		runID, err = p.createRun(ctx, agentID, req, promptText)
		if err != nil {
			return nil, err
		}
	}

	text, err := p.streamRun(ctx, agentID, runID)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.cfg.DefaultModel)
	}
	if model == "" {
		model = "default"
	}

	return &common.ChatResponse{
		ID:           runID,
		Model:        model,
		Message:      common.Message{Role: common.RoleAssistant, Content: text},
		FinishReason: "stop",
		Latency:      time.Since(start),
		SessionHints: sessionHints,
	}, nil
}

// Stream implements Provider.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	// Run the same path as Chat but emit incremental assistant deltas.
	ch := make(chan common.StreamChunk, 16)
	go func() {
		defer close(ch)
		ctx, cancel := p.withChatTimeout(ctx)
		defer cancel()

		promptText := serializeMessages(req.Messages, req.JSONMode, req.OutputSchema, req.SchemaName)
		agentID := strings.TrimSpace(req.Metadata[metaCursorAgentID])
		var runID string
		var err error
		if agentID == "" {
			agentID, runID, err = p.createAgent(ctx, req, promptText)
		} else {
			runID, err = p.createRun(ctx, agentID, req, promptText)
		}
		if err != nil {
			ch <- common.StreamChunk{Done: true}
			return
		}
		path := fmt.Sprintf("/v1/agents/%s/runs/%s/stream", agentID, runID)
		hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
		if err != nil {
			ch <- common.StreamChunk{Done: true}
			return
		}
		hreq.Header.Set("Authorization", p.authHeader())
		hreq.Header.Set("Accept", "text/event-stream")
		resp, err := p.httpClient.Do(hreq)
		if err != nil {
			ch <- common.StreamChunk{Done: true}
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			ch <- common.StreamChunk{Done: true}
			return
		}
		sc := bufio.NewScanner(resp.Body)
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 8<<20)
		var curEvent string
		var dataLines []string
		id := runID
		flush := func() {
			if len(dataLines) == 0 {
				curEvent = ""
				return
			}
			payload := strings.Join(dataLines, "\n")
			dataLines = dataLines[:0]
			if curEvent == "assistant" {
				var ap assistantPayload
				if err := json.Unmarshal([]byte(payload), &ap); err == nil && ap.Text != "" {
					ch <- common.StreamChunk{ID: id, Delta: ap.Text}
				}
			}
			if curEvent == "done" || curEvent == "result" {
				ch <- common.StreamChunk{ID: id, Done: true}
			}
			curEvent = ""
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				flush()
				curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(line, "data:")
				if len(data) > 0 && data[0] == ' ' {
					data = data[1:]
				}
				dataLines = append(dataLines, data)
			}
		}
		flush()
		ch <- common.StreamChunk{Done: true}
	}()
	return ch, nil
}
