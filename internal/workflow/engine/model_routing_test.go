package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

type routingStore struct{}

func (routingStore) GetWorkflow(context.Context, string) (*state.WorkflowState, error) {
	return nil, errors.New("not implemented")
}

func (routingStore) SaveWorkflow(context.Context, *state.WorkflowState) error {
	return nil
}

func (routingStore) AppendEvents(context.Context, ...*events.Event) error {
	return nil
}

type routingProvider struct {
	common.BaseProvider
	name      string
	models    []common.ModelInfo
	modelsErr error
}

func newRoutingProvider(name string, models []common.ModelInfo, modelsErr error) *routingProvider {
	return &routingProvider{
		BaseProvider: common.NewBaseProvider(common.CapabilityChat, common.CapabilityModelList),
		name:         name,
		models:       models,
		modelsErr:    modelsErr,
	}
}

func (p *routingProvider) Name() string { return p.name }

func (p *routingProvider) Chat(context.Context, common.ChatRequest) (*common.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *routingProvider) Stream(context.Context, common.ChatRequest) (<-chan common.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (p *routingProvider) Models(context.Context) ([]common.ModelInfo, error) {
	if p.modelsErr != nil {
		return nil, p.modelsErr
	}
	return p.models, nil
}

func (p *routingProvider) HealthCheck(context.Context) error { return nil }

// trackingSaveStore wraps routingStore and records SaveWorkflow calls.
type trackingSaveStore struct {
	routingStore
	saveCalls int
}

func (s *trackingSaveStore) SaveWorkflow(_ context.Context, _ *state.WorkflowState) error {
	s.saveCalls++
	return nil
}

var registerCatalogMockProvider sync.Once

func ensureCatalogMockProviderRegistered() {
	registerCatalogMockProvider.Do(func() {
		common.Register(newRoutingProvider("catalog-mock", nil, nil))
	})
}

func TestDiscoverProviderCatalogFiltersExcludedModels(t *testing.T) {
	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "blocked-model",
		ProviderDefaults: map[string]string{"catalog-mock": "blocked-model"},
		ExcludedModels: map[string]map[string]struct{}{
			"catalog-mock": {"blocked-model": {}},
		},
	})

	catalog := eng.discoverProviderCatalog(context.Background(), newRoutingProvider("catalog-mock", []common.ModelInfo{
		{ID: "safe-model", Name: "Safe Model"},
		{ID: "blocked-model", Name: "Blocked Model"},
	}, nil))

	if catalog.DefaultModel != "safe-model" {
		t.Fatalf("DefaultModel: got %q, want %q", catalog.DefaultModel, "safe-model")
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "safe-model" {
		t.Fatalf("filtered models: got %#v", catalog.Models)
	}
}

func TestDiscoverProviderCatalogFallsBackToConfiguredDefaultOnError(t *testing.T) {
	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	catalog := eng.discoverProviderCatalog(context.Background(), newRoutingProvider("catalog-mock", nil, errors.New("boom")))
	if !catalog.Degraded {
		t.Fatal("expected degraded catalog when model discovery fails")
	}
	if catalog.DefaultModel != "bootstrap-model" {
		t.Fatalf("DefaultModel: got %q, want %q", catalog.DefaultModel, "bootstrap-model")
	}
	if len(catalog.Models) != 1 || catalog.Models[0].ID != "bootstrap-model" {
		t.Fatalf("fallback models: got %#v", catalog.Models)
	}
}

func TestBuildPacketUsesPersonaAssignmentsForDownstreamPhases(t *testing.T) {
	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	ws := state.NewWorkflowState("t1", "s1", "route different models")
	ws.ProviderCatalogs = map[string]state.ProviderModelCatalog{
		"catalog-mock": {
			ProviderName: "catalog-mock",
			DefaultModel: "bootstrap-model",
			Models: []state.ProviderModelInfo{
				{ID: "bootstrap-model"},
				{ID: "code-model"},
				{ID: "review-model"},
			},
		},
	}
	ws.PersonaModels = state.PersonaModelAssignments{
		state.PersonaPod: "code-model",
		state.PersonaQA:          "review-model",
	}

	if got := eng.buildPacket(ws, state.PersonaDirector, nil).ModelName; got != "bootstrap-model" {
		t.Fatalf("director model: got %q", got)
	}
	if got := eng.buildPacket(ws, state.PersonaPod, nil).ModelName; got != "code-model" {
		t.Fatalf("pod model: got %q", got)
	}
	if got := eng.buildPacket(ws, state.PersonaQA, nil).ModelName; got != "review-model" {
		t.Fatalf("qa model: got %q", got)
	}
	if got := eng.buildPacket(ws, state.PersonaArchitect, nil).ModelName; got != "bootstrap-model" {
		t.Fatalf("architect fallback model: got %q", got)
	}
}

func TestApplyOutputNormalizesDirectorPersonaSelections(t *testing.T) {
	ensureCatalogMockProviderRegistered()

	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
		ExcludedModels: map[string]map[string]struct{}{
			"catalog-mock": {"blocked-model": {}},
		},
	})

	ws := state.NewWorkflowState("t1", "s1", "route different models")
	ws.ProviderCatalogs = map[string]state.ProviderModelCatalog{
		"catalog-mock": {
			ProviderName: "catalog-mock",
			DefaultModel: "bootstrap-model",
			Models: []state.ProviderModelInfo{
				{ID: "bootstrap-model"},
				{ID: "plan-model"},
				{ID: "code-model"},
			},
		},
	}

	eng.applyOutput(ws, &state.PersonaOutput{
		Persona:    state.PersonaDirector,
		Summary:    "use specialized downstream models",
		RawContent: `{"mode":"software","title":"Route models","provider":"catalog-mock","model":"plan-model","persona_models":{"project_manager":"plan-model","pod":"code-model","qa":"blocked-model"},"finalizer_action":"artifact-bundle","required_personas":["project_manager","architect","pod","qa","finalizer"],"rationale":"Route coding separately.","summary":"Use the coding model for pod only."}`,
	})

	if ws.ProviderName != "catalog-mock" {
		t.Fatalf("ProviderName: got %q", ws.ProviderName)
	}
	if ws.ModelName != "plan-model" {
		t.Fatalf("ModelName: got %q", ws.ModelName)
	}
	if got := ws.PersonaModels[state.PersonaPod]; got != "code-model" {
		t.Fatalf("pod persona model: got %q", got)
	}
	if got := ws.PersonaModels[state.PersonaQA]; got != "plan-model" {
		t.Fatalf("qa persona model fallback: got %q", got)
	}
	if got := ws.PersonaModels[state.PersonaArchitect]; got != "plan-model" {
		t.Fatalf("architect persona model fallback: got %q", got)
	}
	if ws.Title != "Route models" {
		t.Fatalf("Title: got %q", ws.Title)
	}
}

func TestEnsureProviderCatalogsIsIdempotentWhenAlreadySet(t *testing.T) {
	store := &trackingSaveStore{}
	eng := New(store, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	ws := state.NewWorkflowState("t1", "s1", "check idempotency")
	ws.ProviderCatalogs = map[string]state.ProviderModelCatalog{
		"catalog-mock": {ProviderName: "catalog-mock", DefaultModel: "bootstrap-model"},
	}

	if err := eng.ensureProviderCatalogs(context.Background(), ws); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("SaveWorkflow called %d times; want 0 (catalogs already snapshotted)", store.saveCalls)
	}
}

func TestBuildPacketForwardsFinalizerAction(t *testing.T) {
	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	ws := state.NewWorkflowState("t1", "s1", "write a blog post")
	ws.ProviderName = "catalog-mock"
	ws.ModelName = "bootstrap-model"
	ws.FinalizerAction = "blog-draft"

	packet := eng.buildPacket(ws, state.PersonaFinalizer, nil)
	if packet.FinalizerAction != "blog-draft" {
		t.Fatalf("FinalizerAction: got %q, want %q", packet.FinalizerAction, "blog-draft")
	}
}

func TestApplyOutputSetsRequiredPersonasAndFinalizerAction(t *testing.T) {
	ensureCatalogMockProviderRegistered()

	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	ws := state.NewWorkflowState("t1", "s1", "write a blog post")
	ws.ProviderCatalogs = map[string]state.ProviderModelCatalog{
		"catalog-mock": {
			ProviderName: "catalog-mock",
			DefaultModel: "bootstrap-model",
			Models:       []state.ProviderModelInfo{{ID: "bootstrap-model"}},
		},
	}

	eng.applyOutput(ws, &state.PersonaOutput{
		Persona:    state.PersonaDirector,
		Summary:    "ops workflow",
		RawContent: `{"mode":"ops","title":"Deploy","provider":"catalog-mock","model":"bootstrap-model","persona_models":{},"finalizer_action":"doc-draft","required_personas":["project_manager","finalizer"],"rationale":"Simple ops task.","summary":"Deploy to prod."}`,
	})

	if ws.FinalizerAction != "doc-draft" {
		t.Fatalf("FinalizerAction: got %q, want %q", ws.FinalizerAction, "doc-draft")
	}

	hasPersona := func(kind state.PersonaKind) bool {
		for _, k := range ws.RequiredPersonas {
			if k == kind {
				return true
			}
		}
		return false
	}

	if !hasPersona(state.PersonaProjectMgr) {
		t.Fatalf("RequiredPersonas missing project_manager: %v", ws.RequiredPersonas)
	}
	if !hasPersona(state.PersonaFinalizer) {
		t.Fatalf("RequiredPersonas missing finalizer: %v", ws.RequiredPersonas)
	}
	// Architect was not requested — must be absent.
	if hasPersona(state.PersonaArchitect) {
		t.Fatalf("RequiredPersonas unexpectedly contains architect: %v", ws.RequiredPersonas)
	}
}

func TestNormalizePersonaModelsSwapsNonToolModel(t *testing.T) {
	ensureCatalogMockProviderRegistered()

	eng := New(routingStore{}, Options{
		DefaultProvider:  "catalog-mock",
		DefaultModel:     "bootstrap-model",
		ProviderDefaults: map[string]string{"catalog-mock": "bootstrap-model"},
	})

	catalogs := map[string]state.ProviderModelCatalog{
		"catalog-mock": {
			ProviderName: "catalog-mock",
			DefaultModel: "bootstrap-model",
			Models: []state.ProviderModelInfo{
				{ID: "bootstrap-model", Metadata: map[string]string{"tools": "no"}},
				{ID: "codegemma:7b", Metadata: map[string]string{"tools": "no"}},
				{ID: "qwen2.5-coder:7b", Metadata: map[string]string{"tools": "yes"}},
				{ID: "qwen3.5:9b", Metadata: map[string]string{"tools": "yes"}},
			},
		},
	}

	requested := state.PersonaModelAssignments{
		state.PersonaPod: "codegemma:7b",    // no tools
		state.PersonaQA:          "codegemma:7b",    // no tools
		state.PersonaArchitect:   "codegemma:7b",    // no tools (architect doesn't need tools)
		state.PersonaFinalizer:   "bootstrap-model", // no tools (finalizer doesn't need tools)
	}

	out := eng.normalizePersonaModels("catalog-mock", requested, "bootstrap-model", catalogs)

	// Pod and QA must be swapped to a tool-capable model.
	if got := out[state.PersonaPod]; got != "qwen2.5-coder:7b" {
		t.Fatalf("pod: want qwen2.5-coder:7b, got %q", got)
	}
	if got := out[state.PersonaQA]; got != "qwen2.5-coder:7b" {
		t.Fatalf("qa: want qwen2.5-coder:7b, got %q", got)
	}
	// Architect does not require tools — should keep the original assignment.
	if got := out[state.PersonaArchitect]; got != "codegemma:7b" {
		t.Fatalf("architect: want codegemma:7b, got %q", got)
	}
	// Finalizer does not require tools — should keep the original.
	if got := out[state.PersonaFinalizer]; got != "bootstrap-model" {
		t.Fatalf("finalizer: want bootstrap-model, got %q", got)
	}
}
