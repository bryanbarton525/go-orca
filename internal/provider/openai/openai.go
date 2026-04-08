// Package openai implements the gorca Provider interface using the official
// OpenAI Go SDK (github.com/openai/openai-go).
package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

const ProviderName = "openai"

// Provider wraps the OpenAI SDK client.
type Provider struct {
	common.BaseProvider
	client *oai.Client
	cfg    config.OpenAIConfig
}

// New constructs and returns an OpenAI provider.  It does NOT register itself;
// call Register() after construction.
func New(cfg config.OpenAIConfig) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHTTPClient(&http.Client{Timeout: cfg.Timeout}),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := oai.NewClient(opts...)

	return &Provider{
		BaseProvider: common.NewBaseProvider(
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityToolCalling,
			common.CapabilityEmbeddings,
			common.CapabilityModelList,
		),
		client: &client,
		cfg:    cfg,
	}, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

// Chat implements Provider.
func (p *Provider) Chat(ctx context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	start := time.Now()

	msgs := make([]oai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case common.RoleSystem:
			msgs = append(msgs, oai.SystemMessage(m.Content))
		case common.RoleUser:
			msgs = append(msgs, oai.UserMessage(m.Content))
		case common.RoleAssistant:
			msgs = append(msgs, oai.AssistantMessage(m.Content))
		}
	}

	params := oai.ChatCompletionNewParams{
		Model:    oai.ChatModel(req.Model),
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = oai.Int(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = oai.Float(req.Temperature)
	}

	// Structured output: prefer schema-constrained mode over plain json_object.
	if req.OutputSchema != nil {
		name := req.SchemaName
		if name == "" {
			name = "response"
		}
		params.ResponseFormat = oai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &oai.ResponseFormatJSONSchemaParam{
				JSONSchema: oai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   name,
					Schema: req.OutputSchema,
				},
			},
		}
	} else if req.JSONMode {
		params.ResponseFormat = oai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &oai.ResponseFormatJSONObjectParam{},
		}
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai: chat error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices returned")
	}

	choice := resp.Choices[0]
	return &common.ChatResponse{
		ID:           resp.ID,
		Model:        string(resp.Model),
		Message:      common.Message{Role: common.RoleAssistant, Content: choice.Message.Content},
		FinishReason: string(choice.FinishReason),
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
		Latency:      time.Since(start),
	}, nil
}

// Stream implements Provider.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	ch := make(chan common.StreamChunk, 64)

	msgs := make([]oai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case common.RoleSystem:
			msgs = append(msgs, oai.SystemMessage(m.Content))
		case common.RoleUser:
			msgs = append(msgs, oai.UserMessage(m.Content))
		case common.RoleAssistant:
			msgs = append(msgs, oai.AssistantMessage(m.Content))
		}
	}

	params := oai.ChatCompletionNewParams{
		Model:    oai.ChatModel(req.Model),
		Messages: msgs,
	}

	go func() {
		defer close(ch)
		stream := p.client.Chat.Completions.NewStreaming(ctx, params)
		defer stream.Close()

		for stream.Next() {
			evt := stream.Current()
			if len(evt.Choices) == 0 {
				continue
			}
			delta := evt.Choices[0].Delta.Content
			done := string(evt.Choices[0].FinishReason) != ""
			ch <- common.StreamChunk{ID: evt.ID, Delta: delta, Done: done}
		}
		if err := stream.Err(); err != nil {
			ch <- common.StreamChunk{Done: true}
		}
	}()

	return ch, nil
}

// Models implements Provider.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	page, err := p.client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai: list models error: %w", err)
	}

	out := make([]common.ModelInfo, 0, len(page.Data))
	for _, m := range page.Data {
		out = append(out, common.ModelInfo{
			ID:   m.ID,
			Name: m.ID,
			Capabilities: []common.Capability{
				common.CapabilityChat,
			},
		})
	}
	return out, nil
}

// HealthCheck implements Provider.
func (p *Provider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Models.List(ctx)
	if err != nil {
		return fmt.Errorf("openai: health check failed: %w", err)
	}
	return nil
}
