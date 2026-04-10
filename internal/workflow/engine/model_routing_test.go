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
		state.PersonaImplementer: "code-model",
		state.PersonaQA:          "review-model",
	}

	if got := eng.buildPacket(ws, state.PersonaDirector, nil).ModelName; got != "bootstrap-model" {
		t.Fatalf("director model: got %q", got)
	}
	if got := eng.buildPacket(ws, state.PersonaImplementer, nil).ModelName; got != "code-model" {
		t.Fatalf("implementer model: got %q", got)
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
		RawContent: `{"mode":"software","title":"Route models","provider":"catalog-mock","model":"plan-model","persona_models":{"project_manager":"plan-model","implementer":"code-model","qa":"blocked-model"},"finalizer_action":"artifact-bundle","required_personas":["project_manager","architect","implementer","qa","finalizer"],"rationale":"Route coding separately.","summary":"Use the coding model for implementer only."}`,
	})

	if ws.ProviderName != "catalog-mock" {
		t.Fatalf("ProviderName: got %q", ws.ProviderName)
	}
	if ws.ModelName != "plan-model" {
		t.Fatalf("ModelName: got %q", ws.ModelName)
	}
	if got := ws.PersonaModels[state.PersonaImplementer]; got != "code-model" {
		t.Fatalf("implementer persona model: got %q", got)
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
