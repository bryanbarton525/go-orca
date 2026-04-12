package engine

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

const defaultModelDiscoveryTimeout = 10 * time.Second

func canonicalProviderName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func canonicalModelID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func (e *Engine) defaultModelForProvider(provider string) string {
	provider = canonicalProviderName(provider)
	if provider != "" && e.opts.ProviderDefaults != nil {
		if model := strings.TrimSpace(e.opts.ProviderDefaults[provider]); model != "" {
			return model
		}
	}
	return strings.TrimSpace(e.opts.DefaultModel)
}

func (e *Engine) isModelExcluded(provider, model string) bool {
	provider = canonicalProviderName(provider)
	model = canonicalModelID(model)
	if provider == "" || model == "" || e.opts.ExcludedModels == nil {
		return false
	}
	blocked, ok := e.opts.ExcludedModels[provider]
	if !ok {
		return false
	}
	_, found := blocked[model]
	return found
}

func (e *Engine) ensureProviderCatalogs(ctx context.Context, ws *state.WorkflowState) error {
	if len(ws.ProviderCatalogs) > 0 {
		return nil
	}
	ws.ProviderCatalogs = e.discoverProviderCatalogs(ctx)
	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) discoverProviderCatalogs(ctx context.Context) map[string]state.ProviderModelCatalog {
	providers := common.All()
	sort.Slice(providers, func(i, j int) bool {
		return canonicalProviderName(providers[i].Name()) < canonicalProviderName(providers[j].Name())
	})

	var mu sync.Mutex
	catalogs := make(map[string]state.ProviderModelCatalog, len(providers))

	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(provider common.Provider) {
			defer wg.Done()
			catalog := e.discoverProviderCatalog(ctx, provider)
			mu.Lock()
			catalogs[catalog.ProviderName] = catalog
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return catalogs
}

func (e *Engine) discoverProviderCatalog(ctx context.Context, provider common.Provider) state.ProviderModelCatalog {
	providerName := canonicalProviderName(provider.Name())
	defaultModel := e.defaultModelForProvider(providerName)

	if !provider.HasCapability(common.CapabilityModelList) {
		return e.fallbackCatalog(providerName, defaultModel, "provider does not expose model listing")
	}

	timeout := e.opts.ModelDiscoveryTimeout
	if timeout <= 0 {
		timeout = defaultModelDiscoveryTimeout
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	models, err := provider.Models(lookupCtx)
	if err != nil {
		return e.fallbackCatalog(providerName, defaultModel, err.Error())
	}

	allowed := make([]state.ProviderModelInfo, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" || e.isModelExcluded(providerName, id) {
			continue
		}
		allowed = append(allowed, state.ProviderModelInfo{
			ID:          id,
			Name:        strings.TrimSpace(model.Name),
			Description: strings.TrimSpace(model.Description),
			Metadata:    model.Metadata,
		})
	}
	sort.Slice(allowed, func(i, j int) bool {
		return canonicalModelID(allowed[i].ID) < canonicalModelID(allowed[j].ID)
	})

	catalog := state.ProviderModelCatalog{
		ProviderName: providerName,
		Models:       allowed,
	}

	switch {
	case modelInCatalog(catalog, defaultModel):
		catalog.DefaultModel = strings.TrimSpace(defaultModel)
	case len(allowed) > 0:
		catalog.DefaultModel = allowed[0].ID
	default:
		fallback := e.syntheticDefaultModel(providerName, defaultModel)
		catalog.Degraded = true
		if fallback != "" {
			catalog.DefaultModel = fallback
			catalog.Models = []state.ProviderModelInfo{{ID: fallback, Name: fallback}}
			catalog.DiscoveryError = "provider returned no allowed models; fell back to configured default"
		} else {
			catalog.DiscoveryError = "no allowed models available"
		}
	}

	return catalog
}

func (e *Engine) fallbackCatalog(providerName, defaultModel, discoveryError string) state.ProviderModelCatalog {
	catalog := state.ProviderModelCatalog{
		ProviderName:   providerName,
		Degraded:       true,
		DiscoveryError: discoveryError,
	}
	if fallback := e.syntheticDefaultModel(providerName, defaultModel); fallback != "" {
		catalog.DefaultModel = fallback
		catalog.Models = []state.ProviderModelInfo{{ID: fallback, Name: fallback}}
	}
	return catalog
}

func (e *Engine) syntheticDefaultModel(providerName, defaultModel string) string {
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel == "" || e.isModelExcluded(providerName, defaultModel) {
		return ""
	}
	return defaultModel
}

func modelInCatalog(catalog state.ProviderModelCatalog, model string) bool {
	model = canonicalModelID(model)
	if model == "" {
		return false
	}
	if canonicalModelID(catalog.DefaultModel) == model {
		return true
	}
	for _, item := range catalog.Models {
		if canonicalModelID(item.ID) == model {
			return true
		}
	}
	return false
}

func (e *Engine) providerHasUsableCatalog(provider string, catalogs map[string]state.ProviderModelCatalog) bool {
	provider = canonicalProviderName(provider)
	if provider == "" {
		return false
	}
	if catalog, ok := catalogs[provider]; ok {
		return catalog.DefaultModel != "" || len(catalog.Models) > 0
	}
	defaultModel := e.defaultModelForProvider(provider)
	return defaultModel != "" && !e.isModelExcluded(provider, defaultModel)
}

func (e *Engine) normalizeProviderSelection(requested, fallback string, catalogs map[string]state.ProviderModelCatalog) string {
	fallback = canonicalProviderName(fallback)
	requested = canonicalProviderName(requested)
	if requested == "" {
		return fallback
	}
	if _, ok := common.Get(requested); !ok {
		return fallback
	}
	if e.providerHasUsableCatalog(requested, catalogs) {
		return requested
	}
	return fallback
}

func (e *Engine) modelAllowed(provider, model string, catalogs map[string]state.ProviderModelCatalog) bool {
	provider = canonicalProviderName(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" || e.isModelExcluded(provider, model) {
		return false
	}
	if catalog, ok := catalogs[provider]; ok {
		return modelInCatalog(catalog, model)
	}
	return true
}

func (e *Engine) providerFallbackModel(provider string, catalogs map[string]state.ProviderModelCatalog) string {
	provider = canonicalProviderName(provider)
	if catalog, ok := catalogs[provider]; ok && strings.TrimSpace(catalog.DefaultModel) != "" {
		return strings.TrimSpace(catalog.DefaultModel)
	}
	defaultModel := e.defaultModelForProvider(provider)
	if defaultModel != "" && !e.isModelExcluded(provider, defaultModel) {
		return defaultModel
	}
	return ""
}

func (e *Engine) normalizeModelSelection(provider, requested, fallback string, catalogs map[string]state.ProviderModelCatalog) string {
	provider = canonicalProviderName(provider)
	requested = strings.TrimSpace(requested)
	if e.modelAllowed(provider, requested, catalogs) {
		return requested
	}
	fallback = strings.TrimSpace(fallback)
	if e.modelAllowed(provider, fallback, catalogs) {
		return fallback
	}
	return e.providerFallbackModel(provider, catalogs)
}

func (e *Engine) resolveProviderName(ws *state.WorkflowState) string {
	provider := canonicalProviderName(ws.ProviderName)
	if provider != "" {
		return provider
	}
	return canonicalProviderName(e.opts.DefaultProvider)
}

func (e *Engine) resolveWorkflowModel(ws *state.WorkflowState, provider string) string {
	provider = canonicalProviderName(provider)
	requested := strings.TrimSpace(ws.ModelName)
	if e.modelAllowed(provider, requested, ws.ProviderCatalogs) {
		return requested
	}
	return e.providerFallbackModel(provider, ws.ProviderCatalogs)
}

func (e *Engine) resolvePersonaModel(ws *state.WorkflowState, kind state.PersonaKind, provider string) string {
	provider = canonicalProviderName(provider)
	if kind != state.PersonaDirector && ws.PersonaModels != nil {
		if model := strings.TrimSpace(ws.PersonaModels[kind]); e.modelAllowed(provider, model, ws.ProviderCatalogs) {
			return model
		}
	}
	return e.resolveWorkflowModel(ws, provider)
}

func (e *Engine) normalizePersonaModels(provider string, requested state.PersonaModelAssignments, fallback string, catalogs map[string]state.ProviderModelCatalog) state.PersonaModelAssignments {
	provider = canonicalProviderName(provider)
	resolvedFallback := e.normalizeModelSelection(provider, "", fallback, catalogs)
	if requested == nil {
		requested = make(state.PersonaModelAssignments)
	}

	out := make(state.PersonaModelAssignments, len(state.DownstreamPersonaKinds()))
	for _, kind := range state.DownstreamPersonaKinds() {
		model := strings.TrimSpace(requested[kind])
		// Strip a "provider/" namespace prefix that LLMs occasionally emit
		// (e.g. "ollama/qwen3.5:9b" → "qwen3.5:9b").
		if idx := strings.Index(model, "/"); idx != -1 {
			stripped := model[idx+1:]
			if e.modelAllowed(provider, stripped, catalogs) {
				out[kind] = stripped
				continue
			}
		}
		// Strip a "provider/" namespace prefix that LLMs occasionally emit
		// (e.g. "ollama/qwen3.5:9b" → "qwen3.5:9b").
		if idx := strings.Index(model, "/"); idx != -1 {
			stripped := model[idx+1:]
			if e.modelAllowed(provider, stripped, catalogs) {
				out[kind] = stripped
				continue
			}
		}
		if e.modelAllowed(provider, model, catalogs) {
			out[kind] = model
			continue
		}
		if resolvedFallback != "" {
			out[kind] = resolvedFallback
		}
	}
	return out
}
