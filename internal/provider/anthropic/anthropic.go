// Package anthropic implements the gorca Provider interface using the official
// Anthropic Go SDK (github.com/anthropics/anthropic-sdk-go).
//
// Structured outputs are sent via the Beta Messages API using the json_schema
// output format, which constrains the model's response to the exact JSON shape
// defined by the persona's OutputSchema.
package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

const ProviderName = "anthropic"

// Provider wraps the Anthropic SDK client.
type Provider struct {
	common.BaseProvider
	client *anthropicapi.Client
	cfg    config.AnthropicConfig
}

// New constructs and returns an Anthropic provider.
func New(cfg config.AnthropicConfig) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(&http.Client{Timeout: cfg.Timeout}),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := anthropicapi.NewClient(opts...)

	return &Provider{
		BaseProvider: common.NewBaseProvider(
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityToolCalling,
			common.CapabilityModelList,
		),
		client: &client,
		cfg:    cfg,
	}, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

// Chat implements Provider.
//
// When req.OutputSchema is set, the request is sent via the Beta Messages API
// with output_format=json_schema so the model is constrained to the schema.
// When only req.JSONMode is set, the model is asked for JSON via plain text.
func (p *Provider) Chat(ctx context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	start := time.Now()

	model := req.Model
	if model == "" {
		model = p.cfg.DefaultModel
	}

	maxTokens := int64(p.cfg.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = 16384
	}

	// Separate system messages from conversation messages.
	var systemBlocks []anthropicapi.BetaTextBlockParam
	var betaMsgs []anthropicapi.BetaMessageParam

	for _, m := range req.Messages {
		switch m.Role {
		case common.RoleSystem:
			systemBlocks = append(systemBlocks, anthropicapi.BetaTextBlockParam{Text: m.Content})
		case common.RoleUser:
			betaMsgs = append(betaMsgs, anthropicapi.NewBetaUserMessage(
				anthropicapi.NewBetaTextBlock(m.Content),
			))
		case common.RoleAssistant:
			betaMsgs = append(betaMsgs, anthropicapi.BetaMessageParam{
				Role: anthropicapi.BetaMessageParamRoleAssistant,
				Content: []anthropicapi.BetaContentBlockParamUnion{
					anthropicapi.NewBetaTextBlock(m.Content),
				},
			})
		}
	}

	if len(betaMsgs) == 0 {
		return nil, fmt.Errorf("anthropic: no user or assistant messages in request")
	}

	params := anthropicapi.BetaMessageNewParams{
		Model:     anthropicapi.Model(model),
		MaxTokens: maxTokens,
		Messages:  betaMsgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if req.OutputSchema != nil {
		params.OutputConfig = anthropicapi.BetaOutputConfigParam{
			Format: anthropicapi.BetaJSONOutputFormatParam{
				Schema: req.OutputSchema,
			},
		}
	}

	msg, err := p.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic: chat error: %w", err)
	}

	// Collect text blocks from the response.
	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}

	truncated := string(msg.StopReason) == "max_tokens"

	return &common.ChatResponse{
		ID:           msg.ID,
		Model:        string(msg.Model),
		Message:      common.Message{Role: common.RoleAssistant, Content: sb.String()},
		FinishReason: string(msg.StopReason),
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
		Latency:      time.Since(start),
		Truncated:    truncated,
	}, nil
}

// Stream implements Provider.
// Anthropic streaming is not yet implemented; returns an error.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	return nil, fmt.Errorf("anthropic: streaming not yet implemented")
}

// Models implements Provider — returns hardcoded known Claude models.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	return []common.ModelInfo{
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5", Capabilities: []common.Capability{common.CapabilityChat, common.CapabilityToolCalling}},
		{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Capabilities: []common.Capability{common.CapabilityChat, common.CapabilityToolCalling}},
		{ID: "claude-haiku-3-5", Name: "Claude Haiku 3.5", Capabilities: []common.Capability{common.CapabilityChat, common.CapabilityToolCalling}},
	}, nil
}

// HealthCheck verifies connectivity to the Anthropic API with a minimal request.
func (p *Provider) HealthCheck(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	model := p.cfg.DefaultModel
	if model == "" {
		model = "claude-haiku-3-5"
	}

	_, err := p.client.Beta.Messages.New(hctx, anthropicapi.BetaMessageNewParams{
		Model:     anthropicapi.Model(model),
		MaxTokens: 1,
		Messages: []anthropicapi.BetaMessageParam{
			anthropicapi.NewBetaUserMessage(anthropicapi.NewBetaTextBlock("hi")),
		},
	})
	if err != nil {
		return fmt.Errorf("anthropic: health check failed: %w", err)
	}
	return nil
}
