// Package templates loads predefined workflow templates with persona/model defaults.
package templates

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/go-orca/go-orca/internal/state"
	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var templateFS embed.FS

// Template is a predefined workflow profile.
type Template struct {
	ID               string                           `yaml:"id"`
	Mode             state.WorkflowMode               `yaml:"mode"`
	RequiredPersonas []state.PersonaKind              `yaml:"required_personas"`
	PersonaModels    state.PersonaModelAssignments    `yaml:"persona_models"`
	PersonaProviders state.PersonaProviderAssignments `yaml:"persona_providers"`
	ToolchainID      string                           `yaml:"toolchain_id"`
	PodSpecialties   []string                         `yaml:"pod_specialties"`
	MCPAgents        bool                             `yaml:"mcp_agents"`
	TaskTiers        map[string]string                `yaml:"task_tiers"`
	DefaultProvider  string                           `yaml:"default_provider"`
	DefaultModel     string                           `yaml:"default_model"`
	WorkflowHints    []string                         `yaml:"workflow_hints"`
}

var (
	mu        sync.RWMutex
	byID      map[string]Template
	modeIndex map[state.WorkflowMode]string
)

func init() {
	_ = reload()
}

func reload() error {
	entries, err := templateFS.ReadDir(".")
	if err != nil {
		return err
	}
	next := make(map[string]Template)
	modeIdx := make(map[state.WorkflowMode]string)
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") {
			continue
		}
		raw, err := templateFS.ReadFile(ent.Name())
		if err != nil {
			return err
		}
		var t Template
		if err := yaml.Unmarshal(raw, &t); err != nil {
			return fmt.Errorf("template %s: %w", ent.Name(), err)
		}
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		normalizeTemplate(&t)
		next[t.ID] = t
		if t.Mode != "" && modeIdx[t.Mode] == "" {
			modeIdx[t.Mode] = t.ID
		}
	}
	mu.Lock()
	byID = next
	modeIndex = modeIdx
	mu.Unlock()
	return nil
}

func normalizeTemplate(t *Template) {
	for i, k := range t.RequiredPersonas {
		t.RequiredPersonas[i] = state.PersonaKind(strings.ToLower(strings.TrimSpace(string(k))))
	}
	if t.PersonaModels == nil {
		t.PersonaModels = make(state.PersonaModelAssignments)
	}
	if t.PersonaProviders == nil {
		t.PersonaProviders = make(state.PersonaProviderAssignments)
	}
}

// Get returns a template by id.
func Get(id string) (Template, bool) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := byID[strings.TrimSpace(id)]
	return t, ok
}

// List returns all templates.
func List() []Template {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Template, 0, len(byID))
	for _, t := range byID {
		out = append(out, t)
	}
	return out
}

// DefaultForMode returns the default template for a workflow mode.
func DefaultForMode(mode state.WorkflowMode) (Template, bool) {
	mu.RLock()
	id := modeIndex[mode]
	mu.RUnlock()
	if id == "" {
		return Template{}, false
	}
	return Get(id)
}

// Apply merges template defaults into workflow state.
func Apply(ws *state.WorkflowState, t Template) {
	if ws == nil {
		return
	}
	if t.Mode != "" && ws.Mode == "" {
		ws.Mode = t.Mode
	}
	if len(t.RequiredPersonas) > 0 && len(ws.RequiredPersonas) == 0 {
		ws.RequiredPersonas = append([]state.PersonaKind(nil), t.RequiredPersonas...)
	}
	if len(t.PersonaModels) > 0 {
		if ws.PersonaModels == nil {
			ws.PersonaModels = make(state.PersonaModelAssignments)
		}
		for k, v := range t.PersonaModels {
			if strings.TrimSpace(ws.PersonaModels[k]) == "" {
				ws.PersonaModels[k] = v
			}
		}
	}
	if len(t.PersonaProviders) > 0 {
		if ws.Execution.PersonaProviderAssignments == nil {
			ws.Execution.PersonaProviderAssignments = make(state.PersonaProviderAssignments)
		}
		for k, v := range t.PersonaProviders {
			if strings.TrimSpace(ws.Execution.PersonaProviderAssignments[k]) == "" {
				ws.Execution.PersonaProviderAssignments[k] = v
			}
		}
	}
	if t.DefaultProvider != "" && strings.TrimSpace(ws.ProviderName) == "" {
		ws.ProviderName = t.DefaultProvider
	}
	if t.DefaultModel != "" && strings.TrimSpace(ws.ModelName) == "" {
		ws.ModelName = t.DefaultModel
	}
	if t.ToolchainID != "" && ws.Execution.Toolchain == nil {
		ws.Execution.Toolchain = &state.ToolchainSelection{ID: t.ToolchainID}
	}
	if t.MCPAgents {
		ws.Execution.PreferMCPAgents = true
	}
	ws.Execution.TemplateID = t.ID
	if ws.Execution.AutoMode == nil {
		ws.Execution.AutoMode = &state.AutoModeState{}
	}
	if len(t.PodSpecialties) > 0 || len(t.WorkflowHints) > 0 {
		def := ws.Execution.AutoMode.ActiveDefinition
		if def == nil {
			def = &state.AutoDefinition{ID: t.ID, Source: "template"}
		}
		if len(t.PodSpecialties) > 0 {
			def.PodSpecialties = append([]string(nil), t.PodSpecialties...)
		}
		if len(t.WorkflowHints) > 0 {
			def.WorkflowHints = append([]string(nil), t.WorkflowHints...)
		}
		ws.Execution.AutoMode.ActiveDefinition = def
	}
}
