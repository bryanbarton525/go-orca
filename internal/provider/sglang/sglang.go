// Package sglang implements the go-orca Provider interface for SGLang's
// OpenAI-compatible API server.
package sglang

import (
	"fmt"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/openai"
)

const ProviderName = "sglang"

// Provider wraps the OpenAI-compatible provider with an SGLang registry name.
type Provider struct {
	*openai.Provider
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

	return &Provider{Provider: provider}, nil
}

// Name implements Provider.
func (p *Provider) Name() string { return ProviderName }
