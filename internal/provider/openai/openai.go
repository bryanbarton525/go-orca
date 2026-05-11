// Package openai implements the gorca Provider interface using the official
// OpenAI Go SDK (github.com/openai/openai-go).
package openai

import (
	"context"
	"encoding/json"
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

	params, err := buildChatParams(req, p.cfg.DefaultModel)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai: chat error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices returned")
	}

	choice := resp.Choices[0]
	toolCalls := convertFromOpenAIToolCalls(choice.Message.ToolCalls)
	return &common.ChatResponse{
		ID:           resp.ID,
		Model:        string(resp.Model),
		Message:      common.Message{Role: common.RoleAssistant, Content: choice.Message.Content, ToolCalls: toolCalls},
		FinishReason: string(choice.FinishReason),
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
		Latency:      time.Since(start),
		Truncated:    string(choice.FinishReason) == "length",
	}, nil
}

// Stream implements Provider.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	ch := make(chan common.StreamChunk, 64)

	params, err := buildChatParams(req, p.cfg.DefaultModel)
	if err != nil {
		return nil, err
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
			chunk := common.StreamChunk{ID: evt.ID, Delta: delta, Done: done}
			if len(evt.Choices[0].Delta.ToolCalls) > 0 {
				tc := evt.Choices[0].Delta.ToolCalls[0]
				args := decodeToolArguments(tc.Function.Arguments)
				chunk.ToolCall = &common.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				}
			}
			ch <- chunk
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

func buildChatParams(req common.ChatRequest, defaultModel string) (oai.ChatCompletionNewParams, error) {
	msgs, err := convertToOpenAIMessages(req.Messages)
	if err != nil {
		return oai.ChatCompletionNewParams{}, err
	}

	model := req.Model
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		return oai.ChatCompletionNewParams{}, fmt.Errorf("openai: model is required")
	}

	params := oai.ChatCompletionNewParams{
		Model:    oai.ChatModel(model),
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = oai.Int(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = oai.Float(req.Temperature)
	}

	if len(req.Tools) > 0 {
		params.Tools = convertToOpenAITools(req.Tools)
		return params, nil
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

	return params, nil
}

func convertToOpenAIMessages(messages []common.Message) ([]oai.ChatCompletionMessageParamUnion, error) {
	out := make([]oai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case common.RoleSystem:
			out = append(out, oai.SystemMessage(m.Content))
		case common.RoleUser:
			out = append(out, oai.UserMessage(m.Content))
		case common.RoleAssistant:
			assistant := oai.AssistantMessage(m.Content)
			if len(m.ToolCalls) > 0 {
				assistant.OfAssistant.ToolCalls = convertToOpenAIToolCallParams(m.ToolCalls)
			}
			out = append(out, assistant)
		case common.RoleTool:
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("openai: tool message is missing tool_call_id")
			}
			out = append(out, oai.ToolMessage(m.Content, m.ToolCallID))
		default:
			return nil, fmt.Errorf("openai: unsupported message role %q", m.Role)
		}
	}
	return out, nil
}

func convertToOpenAITools(defs []common.ToolDefinition) []oai.ChatCompletionToolParam {
	out := make([]oai.ChatCompletionToolParam, 0, len(defs))
	for _, d := range defs {
		tool := oai.ChatCompletionToolParam{
			Function: oai.FunctionDefinitionParam{
				Name:        d.Name,
				Description: oai.String(d.Description),
				Parameters:  oai.FunctionParameters(d.Parameters),
			},
		}
		out = append(out, tool)
	}
	return out
}

func convertToOpenAIToolCallParams(calls []common.ToolCall) []oai.ChatCompletionMessageToolCallParam {
	out := make([]oai.ChatCompletionMessageToolCallParam, 0, len(calls))
	for _, call := range calls {
		args, err := json.Marshal(call.Arguments)
		if err != nil {
			args = []byte(`{}`)
		}
		out = append(out, oai.ChatCompletionMessageToolCallParam{
			ID: call.ID,
			Function: oai.ChatCompletionMessageToolCallFunctionParam{
				Name:      call.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}

func convertFromOpenAIToolCalls(calls []oai.ChatCompletionMessageToolCall) []common.ToolCall {
	out := make([]common.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, common.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: decodeToolArguments(call.Function.Arguments),
		})
	}
	return out
}

func decodeToolArguments(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return map[string]interface{}{"_raw": raw}
	}
	return args
}
