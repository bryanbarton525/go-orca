// Package ollama implements the gorca Provider interface using the official
// Ollama Go SDK (github.com/ollama/ollama/api).
package ollama

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	ollamaapi "github.com/ollama/ollama/api"
	ollamamodel "github.com/ollama/ollama/types/model"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

const ProviderName = "ollama"

// Provider wraps the Ollama SDK client.
type Provider struct {
	common.BaseProvider
	client *ollamaapi.Client
	cfg    config.OllamaConfig
}

// New constructs and returns an Ollama provider.
func New(cfg config.OllamaConfig) (*Provider, error) {
	base, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("ollama: invalid host %q: %w", cfg.Host, err)
	}

	transport, err := buildTransport(cfg.TLSSkipVerify)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: cfg.Timeout, Transport: transport}
	client := ollamaapi.NewClient(base, httpClient)

	return &Provider{
		BaseProvider: common.NewBaseProvider(
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityToolCalling,
			common.CapabilityEmbeddings,
			common.CapabilityModelList,
		),
		client: client,
		cfg:    cfg,
	}, nil
}

func buildTransport(skipVerify bool) (*http.Transport, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("ollama: unexpected default transport type %T", http.DefaultTransport)
	}
	transport := baseTransport.Clone()
	if skipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return transport, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

// Chat implements Provider.
func (p *Provider) Chat(ctx context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	start := time.Now()

	msgs := make([]ollamaapi.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := ollamaapi.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			var args ollamaapi.ToolCallFunctionArguments
			_ = json.Unmarshal(argsJSON, &args)
			om.ToolCalls = append(om.ToolCalls, ollamaapi.ToolCall{
				ID: tc.ID,
				Function: ollamaapi.ToolCallFunction{
					Name:      tc.Name,
					Arguments: args,
				},
			})
		}
		msgs = append(msgs, om)
	}

	model := req.Model
	if model == "" {
		model = p.cfg.DefaultModel
	}

	stream := false
	opts := map[string]any{
		// num_predict: -1 means unlimited output tokens.
		// Without this, Ollama uses the model's Modelfile default (often 2048 or
		// less), causing done_reason=length on long persona responses.
		"num_predict": -1,
	}
	if p.cfg.NumCtx > 0 {
		// num_ctx controls the context window size. Ollama defaults to the
		// model's built-in value (often 2048–4096), which is too small for
		// long synthesis tasks. Setting this explicitly prevents the model
		// from truncating content inside schema-constrained JSON fields
		// (where done_reason stays "stop" even though content was cut short).
		opts["num_ctx"] = p.cfg.NumCtx
	}
	ollamaReq := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   &stream,
		Options:  opts,
	}

	// Attach tool definitions when the caller has provided them.
	// When tools are present we skip format constraints: the model must be free
	// to return a tool_calls message rather than a JSON-formatted text reply.
	if len(req.Tools) > 0 {
		ollamaReq.Tools = convertToOllamaTools(req.Tools)
	} else {
		// Use the strongest structured-output mode available.
		// Schema-constrained format is strictly better than plain "json" — the model
		// is constrained to the exact field names and types we expect.
		if req.OutputSchema != nil {
			schemaBytes, err := json.Marshal(req.OutputSchema)
			if err == nil {
				ollamaReq.Format = json.RawMessage(schemaBytes)
			}
		} else if req.JSONMode {
			ollamaReq.Format = json.RawMessage(`"json"`)
		}
	}

	var finalResp *ollamaapi.ChatResponse
	err := p.client.Chat(ctx, ollamaReq, func(r ollamaapi.ChatResponse) error {
		finalResp = &r
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: chat error: %w", err)
	}
	if finalResp == nil {
		return nil, fmt.Errorf("ollama: no response received")
	}

	truncated := finalResp.DoneReason == "length"

	// Map Ollama tool calls back to the canonical type.
	var toolCalls []common.ToolCall
	for _, tc := range finalResp.Message.ToolCalls {
		toolCalls = append(toolCalls, common.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments.ToMap(),
		})
	}

	return &common.ChatResponse{
		ID:    fmt.Sprintf("ollama-%d", finalResp.CreatedAt.UnixMilli()),
		Model: finalResp.Model,
		Message: common.Message{
			Role:      common.RoleAssistant,
			Content:   finalResp.Message.Content,
			ToolCalls: toolCalls,
		},
		FinishReason: finalResp.DoneReason,
		InputTokens:  finalResp.PromptEvalCount,
		OutputTokens: finalResp.EvalCount,
		Latency:      time.Since(start),
		Truncated:    truncated,
	}, nil
}

// convertToOllamaTools converts canonical ToolDefinitions to the Ollama SDK
// tool type, marshaling the parameters JSON-Schema map through the SDK type.
func convertToOllamaTools(defs []common.ToolDefinition) ollamaapi.Tools {
	out := make(ollamaapi.Tools, 0, len(defs))
	for _, d := range defs {
		paramsJSON, _ := json.Marshal(d.Parameters)
		var params ollamaapi.ToolFunctionParameters
		_ = json.Unmarshal(paramsJSON, &params)
		out = append(out, ollamaapi.Tool{
			Type: "function",
			Function: ollamaapi.ToolFunction{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

// Stream implements Provider.
func (p *Provider) Stream(ctx context.Context, req common.ChatRequest) (<-chan common.StreamChunk, error) {
	ch := make(chan common.StreamChunk, 64)

	msgs := make([]ollamaapi.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaapi.Message{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	model := req.Model
	if model == "" {
		model = p.cfg.DefaultModel
	}

	streamOn := true
	ollamaReq := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   &streamOn,
	}

	go func() {
		defer close(ch)
		_ = p.client.Chat(ctx, ollamaReq, func(r ollamaapi.ChatResponse) error {
			ch <- common.StreamChunk{
				Delta: r.Message.Content,
				Done:  r.Done,
			}
			return nil
		})
	}()

	return ch, nil
}

// Models implements Provider.
// It calls Show for each model (in parallel) to discover whether the model
// supports Ollama's tool-calling API. The result is stored in
// ModelInfo.Metadata["tools"] ("yes" or "no") and reflected in Capabilities.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	list, err := p.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama: list models error: %w", err)
	}

	// Check tool-calling support for each model via Show API (parallel).
	toolSupport := make([]bool, len(list.Models))
	var wg sync.WaitGroup
	for i, m := range list.Models {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			showResp, showErr := p.client.Show(ctx, &ollamaapi.ShowRequest{Model: name})
			if showErr != nil {
				return // default: tools=no (safe fallback)
			}
			for _, cap := range showResp.Capabilities {
				if cap == ollamamodel.CapabilityTools {
					toolSupport[i] = true
					return
				}
			}
		}(i, m.Name)
	}
	wg.Wait()

	out := make([]common.ModelInfo, 0, len(list.Models))
	for i, m := range list.Models {
		capabilities := []common.Capability{
			common.CapabilityChat,
			common.CapabilityStreaming,
		}
		toolsVal := "no"
		if toolSupport[i] {
			capabilities = append(capabilities, common.CapabilityToolCalling)
			toolsVal = "yes"
		}
		out = append(out, common.ModelInfo{
			ID:           m.Name,
			Name:         m.Name,
			Capabilities: capabilities,
			Metadata: map[string]string{
				"size":           fmt.Sprintf("%d", m.Size),
				"family":         strings.ToLower(m.Details.Family),
				"parameter_size": m.Details.ParameterSize,
				"tools":          toolsVal,
			},
		})
	}
	return out, nil
}

// HealthCheck implements Provider.
func (p *Provider) HealthCheck(ctx context.Context) error {
	if err := p.client.Heartbeat(ctx); err != nil {
		return fmt.Errorf("ollama: health check failed: %w", err)
	}
	return nil
}
