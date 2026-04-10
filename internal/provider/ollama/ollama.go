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
	"time"

	ollamaapi "github.com/ollama/ollama/api"

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
		msgs = append(msgs, ollamaapi.Message{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	model := req.Model
	if model == "" {
		model = p.cfg.DefaultModel
	}

	stream := false
	ollamaReq := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   &stream,
		// num_predict: -1 means unlimited output tokens.
		// Without this, Ollama uses the model's Modelfile default (often 2048 or
		// less), causing done_reason=length on long persona responses.
		Options: map[string]any{"num_predict": -1},
	}

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

	return &common.ChatResponse{
		ID:           fmt.Sprintf("ollama-%d", finalResp.CreatedAt.UnixMilli()),
		Model:        finalResp.Model,
		Message:      common.Message{Role: common.RoleAssistant, Content: finalResp.Message.Content},
		FinishReason: finalResp.DoneReason,
		InputTokens:  finalResp.PromptEvalCount,
		OutputTokens: finalResp.EvalCount,
		Latency:      time.Since(start),
		Truncated:    truncated,
	}, nil
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
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	list, err := p.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("ollama: list models error: %w", err)
	}

	out := make([]common.ModelInfo, 0, len(list.Models))
	for _, m := range list.Models {
		out = append(out, common.ModelInfo{
			ID:   m.Name,
			Name: m.Name,
			Capabilities: []common.Capability{
				common.CapabilityChat,
				common.CapabilityStreaming,
			},
			Metadata: map[string]string{
				"size":   fmt.Sprintf("%d", m.Size),
				"family": strings.ToLower(m.Details.Family),
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
