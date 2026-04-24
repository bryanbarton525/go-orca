// Package copilot implements the gorca Provider interface using the official
// GitHub Copilot Go SDK (github.com/github/copilot-sdk/go).
//
// The Copilot SDK communicates with the Copilot CLI server over JSON-RPC.
// This provider manages the client lifecycle and maps Copilot sessions to
// gorca ChatRequest / ChatResponse structures.
package copilot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

const ProviderName = "copilot"

// Provider wraps the GitHub Copilot SDK client.
type Provider struct {
	common.BaseProvider
	cfg    config.CopilotConfig
	mu     sync.Mutex
	client *copilot.Client
	ready  bool
}

// New constructs a Copilot provider. The underlying CLI client is started lazily
// on the first call to Chat or Stream so that application startup is not
// blocked by the Copilot CLI process.
func New(cfg config.CopilotConfig) (*Provider, error) {
	return &Provider{
		BaseProvider: common.NewBaseProvider(
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityAgentRuntime,
			common.CapabilitySkills,
			common.CapabilityHandoffs,
			common.CapabilityModelList,
		),
		cfg: cfg,
	}, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

// ensureClient starts the Copilot CLI client if it is not yet started.
func (p *Provider) ensureClient(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ready {
		return nil
	}

	opts := &copilot.ClientOptions{}
	if p.cfg.CLIPath != "" {
		opts.CLIPath = p.cfg.CLIPath
	}
	if p.cfg.GitHubToken != "" {
		opts.GitHubToken = p.cfg.GitHubToken
	}

	p.client = copilot.NewClient(opts)
	if err := p.client.Start(ctx); err != nil {
		return fmt.Errorf("copilot: failed to start CLI client: %w", err)
	}

	p.ready = true
	return nil
}

// Chat implements Provider.
//
// Each call creates a short-lived Copilot session, sends the last user message,
// collects the full assistant response via events, then tears down the session.
func (p *Provider) Chat(ctx context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	if err := p.ensureClient(ctx); err != nil {
		return nil, err
	}

	start := time.Now()

	model := req.Model
	if model == "" {
		model = "gpt-4o"
	}

	session, err := p.client.CreateSession(ctx, &copilot.SessionConfig{
		Model:               model,
		ClientName:          "gorca",
		SystemMessage:       p.buildSystemMessage(req.Messages),
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
		// Disable all built-in agent tools so the model responds with text only
		// and does not try to invoke filesystem / shell tools during generation.
		AvailableTools: []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("copilot: create session error: %w", err)
	}
	defer func() { _ = p.client.DeleteSession(ctx, session.SessionID) }()

	// Collect the full response through events.
	var (
		responseBuilder strings.Builder
		done            = make(chan struct{})
		mu              sync.Mutex
		finishReason    string
		once            sync.Once
	)

	session.On(func(evt copilot.SessionEvent) {
		mu.Lock()
		defer mu.Unlock()

		switch evt.Type {
		case copilot.SessionEventTypeAssistantMessage:
			if evt.Data.Content != nil {
				responseBuilder.WriteString(*evt.Data.Content)
			}
		case copilot.SessionEventTypeAssistantTurnEnd, copilot.SessionEventTypeSessionIdle:
			finishReason = string(evt.Type)
			once.Do(func() { close(done) })
		}
	})

	// Send the last user message.
	userPrompt := lastUserMessage(req.Messages)
	if _, err := session.Send(ctx, copilot.MessageOptions{Prompt: userPrompt}); err != nil {
		return nil, fmt.Errorf("copilot: send message error: %w", err)
	}

	// Wait for idle/turn-end or context cancellation.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
	}

	mu.Lock()
	content := responseBuilder.String()
	reason := finishReason
	mu.Unlock()

	// The Copilot SDK does not expose a finish_reason equivalent for token-limit
	// truncation. Treat an empty response as truncated so the executor can
	// apply its recovery path (slim retry) instead of propagating a confusing
	// "mid-JSON" parse error.
	truncated := content == ""

	return &common.ChatResponse{
		ID:           fmt.Sprintf("copilot-%d", time.Now().UnixMilli()),
		Model:        model,
		Message:      common.Message{Role: common.RoleAssistant, Content: content},
		FinishReason: reason,
		Truncated:    truncated,
		Latency:      time.Since(start),
	}, nil
}

// Stream implements Provider.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	if err := p.ensureClient(ctx); err != nil {
		return nil, err
	}

	ch := make(chan common.StreamChunk, 64)

	model := req.Model
	if model == "" {
		model = "gpt-4o"
	}

	go func() {
		defer close(ch)

		session, err := p.client.CreateSession(ctx, &copilot.SessionConfig{
			Model:               model,
			ClientName:          "gorca",
			SystemMessage:       p.buildSystemMessage(req.Messages),
			OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
			AvailableTools:      []string{},
		})
		if err != nil {
			ch <- common.StreamChunk{Done: true}
			return
		}
		defer func() { _ = p.client.DeleteSession(ctx, session.SessionID) }()

		done := make(chan struct{})
		var once sync.Once

		session.On(func(evt copilot.SessionEvent) {
			switch evt.Type {
			case copilot.SessionEventTypeAssistantMessageDelta:
				if evt.Data.DeltaContent != nil {
					ch <- common.StreamChunk{Delta: *evt.Data.DeltaContent}
				}
			case copilot.SessionEventTypeAssistantMessage:
				if evt.Data.Content != nil {
					ch <- common.StreamChunk{Delta: *evt.Data.Content}
				}
			case copilot.SessionEventTypeAssistantTurnEnd, copilot.SessionEventTypeSessionIdle:
				ch <- common.StreamChunk{Done: true}
				once.Do(func() { close(done) })
			}
		})

		userPrompt := lastUserMessage(req.Messages)
		if _, err := session.Send(ctx, copilot.MessageOptions{Prompt: userPrompt}); err != nil {
			ch <- common.StreamChunk{Done: true}
			return
		}

		select {
		case <-ctx.Done():
		case <-done:
		}
	}()

	return ch, nil
}

// Models implements Provider.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	if err := p.ensureClient(ctx); err != nil {
		return nil, err
	}

	models, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("copilot: list models error: %w", err)
	}

	out := make([]common.ModelInfo, 0, len(models))
	for _, m := range models {
		out = append(out, common.ModelInfo{
			ID:   m.ID,
			Name: m.Name,
			Capabilities: []common.Capability{
				common.CapabilityChat,
				common.CapabilityAgentRuntime,
			},
		})
	}
	return out, nil
}

// HealthCheck implements Provider.
func (p *Provider) HealthCheck(ctx context.Context) error {
	if err := p.ensureClient(ctx); err != nil {
		return err
	}
	_, err := p.client.Ping(ctx, "gorca-health")
	if err != nil {
		return fmt.Errorf("copilot: health check failed: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the Copilot CLI client. Should be called on
// application shutdown.
func (p *Provider) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready && p.client != nil {
		p.ready = false
		return p.client.Stop()
	}
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildSystemMessage extracts system-role messages to populate SessionConfig.
func (p *Provider) buildSystemMessage(msgs []common.Message) *copilot.SystemMessageConfig {
	var parts []string
	for _, m := range msgs {
		if m.Role == common.RoleSystem {
			parts = append(parts, m.Content)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return &copilot.SystemMessageConfig{
		Mode:    "prepend",
		Content: strings.Join(parts, "\n\n"),
	}
}

// lastUserMessage returns the content of the most recent user message.
func lastUserMessage(msgs []common.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == common.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
