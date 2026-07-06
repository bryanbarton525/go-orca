// Package sglang implements the go-orca Provider interface for SGLang's
// OpenAI-compatible API server.
package sglang

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/provider/openai"
)

const ProviderName = "sglang"

// Provider wraps the OpenAI-compatible provider with an SGLang registry name.
type Provider struct {
	*openai.Provider
	apiKey       string
	healthURL    string
	modelsURL    string
	modelInfoURL string
	httpClient   *http.Client
}

// New constructs and returns an SGLang provider. It does NOT register itself;
// call Register() after construction.
func New(cfg config.SGLangConfig) (*Provider, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("sglang: base_url is required")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "sglang"
	}

	provider, err := openai.New(config.OpenAIConfig{
		Enabled:        cfg.Enabled,
		APIKey:         cfg.APIKey,
		BaseURL:        cfg.BaseURL,
		DefaultModel:   cfg.DefaultModel,
		ExcludedModels: cfg.ExcludedModels,
		Timeout:        cfg.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("sglang: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &Provider{
		Provider:     provider,
		apiKey:       cfg.APIKey,
		healthURL:    healthEndpoint(cfg.BaseURL),
		modelsURL:    strings.TrimRight(cfg.BaseURL, "/") + "/models",
		modelInfoURL: nativeEndpoint(cfg.BaseURL, "/get_model_info"),
		httpClient:   &http.Client{Timeout: timeout},
	}, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }

// Models implements Provider. SGLang's /v1/models may return either the
// OpenAI-compatible {"data":[...]} shape or the native {"models":[...]} shape.
// If that endpoint is unavailable or not JSON, fall back to /get_model_info.
func (p *Provider) Models(ctx context.Context) ([]common.ModelInfo, error) {
	models, err := p.modelsFromListEndpoint(ctx)
	if err == nil && len(models) > 0 {
		return models, nil
	}

	infoModels, infoErr := p.modelsFromInfoEndpoint(ctx)
	if infoErr == nil && len(infoModels) > 0 {
		return infoModels, nil
	}

	if err != nil {
		return nil, fmt.Errorf("sglang: list models error: %w", err)
	}
	return nil, fmt.Errorf("sglang: get model info error: %w", infoErr)
}

// HealthCheck implements Provider. SGLang exposes a native /health endpoint
// that returns a plain-text 200, so probe it first instead of the OpenAI-style
// model list (which requires a JSON response some SGLang gateways do not
// provide). Falls back to the embedded OpenAI check if /health is unavailable.
func (p *Provider) HealthCheck(ctx context.Context) error {
	req, err := p.newRequest(ctx, http.MethodGet, p.healthURL)
	if err != nil {
		return fmt.Errorf("sglang: health check request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
	}

	// /health unreachable or non-2xx: fall back to the OpenAI-style check so
	// gateways that only expose /v1 routes can still pass.
	if fallbackErr := p.Provider.HealthCheck(ctx); fallbackErr != nil {
		return fmt.Errorf("sglang: health check failed: %w", fallbackErr)
	}
	return nil
}

func (p *Provider) modelsFromListEndpoint(ctx context.Context) ([]common.ModelInfo, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	if err := p.getJSON(ctx, p.modelsURL, &payload); err != nil {
		return nil, err
	}

	models := make([]common.ModelInfo, 0, len(payload.Data)+len(payload.Models))
	for _, model := range payload.Data {
		models = appendSGLangModel(models, model.ID)
	}
	for _, model := range payload.Models {
		models = appendSGLangModel(models, model)
	}
	return models, nil
}

func (p *Provider) modelsFromInfoEndpoint(ctx context.Context) ([]common.ModelInfo, error) {
	var payload struct {
		ModelPath     string   `json:"model_path"`
		TokenizerPath string   `json:"tokenizer_path"`
		ModelType     string   `json:"model_type"`
		Architectures []string `json:"architectures"`
	}
	if err := p.getJSON(ctx, p.modelInfoURL, &payload); err != nil {
		return nil, err
	}

	model := payload.ModelPath
	if model == "" {
		model = payload.TokenizerPath
	}
	models := appendSGLangModel(nil, model)
	if len(models) == 0 {
		return nil, fmt.Errorf("model info did not include model_path or tokenizer_path")
	}
	if payload.ModelType != "" || len(payload.Architectures) > 0 {
		models[0].Metadata = map[string]string{}
		if payload.ModelType != "" {
			models[0].Metadata["model_type"] = payload.ModelType
		}
		if len(payload.Architectures) > 0 {
			models[0].Metadata["architectures"] = strings.Join(payload.Architectures, ",")
		}
	}
	return models, nil
}

func (p *Provider) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := p.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned status %d", endpoint, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return err
	}
	return nil
}

func (p *Provider) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return req, nil
}

func appendSGLangModel(models []common.ModelInfo, id string) []common.ModelInfo {
	id = strings.TrimSpace(id)
	if id == "" {
		return models
	}
	return append(models, common.ModelInfo{
		ID:   id,
		Name: id,
		Capabilities: []common.Capability{
			common.CapabilityChat,
			common.CapabilityStreaming,
			common.CapabilityToolCalling,
		},
	})
}

// healthEndpoint derives SGLang's native /health URL from an
// OpenAI-compatible base URL (e.g. http://host:8080/v1 -> http://host:8080/health).
func healthEndpoint(baseURL string) string {
	return nativeEndpoint(baseURL, "/health")
}

func nativeEndpoint(baseURL, path string) string {
	root := strings.TrimRight(baseURL, "/")
	root = strings.TrimSuffix(root, "/v1")
	return root + path
}
