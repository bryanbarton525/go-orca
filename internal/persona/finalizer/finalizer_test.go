package finalizer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

type scriptedProvider struct {
	common.BaseProvider
	name      string
	responses []string
	mu        sync.Mutex
	next      int
}

func newScriptedProvider(name string, responses ...string) *scriptedProvider {
	return &scriptedProvider{
		BaseProvider: common.NewBaseProvider(common.CapabilityChat),
		name:         name,
		responses:    responses,
	}
}

func (p *scriptedProvider) Name() string { return p.name }

func (p *scriptedProvider) Chat(_ context.Context, req common.ChatRequest) (*common.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= len(p.responses) {
		return nil, fmt.Errorf("scripted provider %q exhausted responses", p.name)
	}
	content := p.responses[p.next]
	p.next++
	return &common.ChatResponse{
		ID:    fmt.Sprintf("resp-%d", p.next),
		Model: req.Model,
		Message: common.Message{
			Role:    common.RoleAssistant,
			Content: content,
		},
		Latency: 5 * time.Millisecond,
	}, nil
}

func (p *scriptedProvider) Stream(context.Context, common.ChatRequest) (<-chan common.StreamChunk, error) {
	return nil, fmt.Errorf("stream not implemented in scripted provider")
}

func (p *scriptedProvider) Models(context.Context) ([]common.ModelInfo, error) { return nil, nil }

func (p *scriptedProvider) HealthCheck(context.Context) error { return nil }

var scriptedProviderSeq uint64

func registerScriptedProvider(responses ...string) string {
	name := fmt.Sprintf("scripted-finalizer-%d", atomic.AddUint64(&scriptedProviderSeq, 1))
	common.Register(newScriptedProvider(name, responses...))
	return name
}

func TestFinalizerRunRefiner_EmitsNestedPersonaProgress(t *testing.T) {
	providerName := registerScriptedProvider(
		`{"delivery_action":"blog-draft","summary":"finalized","links":[],"metadata":{},"suggestions":[],"delivery_notes":"ok"}`,
		`{"improvements":[],"overall_assessment":"healthy","health_score":95,"summary":"retrospective complete"}`,
	)

	f := New()
	packet := state.HandoffPacket{
		WorkflowID:     "wf-1",
		TenantID:       "tenant-1",
		ScopeID:        "scope-1",
		Mode:           state.WorkflowModeContent,
		Request:        "Define Go in one sentence",
		CurrentPersona: state.PersonaFinalizer,
		ProviderName:   providerName,
		ModelName:      "mock-model",
		Artifacts: []state.Artifact{{
			ID:          "art-1",
			WorkflowID:  "wf-1",
			Kind:        state.ArtifactKindBlogPost,
			Name:        "go-definition.md",
			Description: "single sentence definition",
			Content:     "Go is a programming language.",
			CreatedBy:   state.PersonaImplementer,
			CreatedAt:   time.Now().UTC(),
		}},
		PersonaPromptSnapshot: map[string]string{
			prompts.KeyFinalizer:        "finalizer system prompt",
			prompts.KeyFinalizerRefiner: "refiner system prompt",
		},
	}

	type startedCall struct {
		persona  state.PersonaKind
		provider string
		model    string
	}
	type completedCall struct {
		persona    state.PersonaKind
		durationMs int64
		summary    string
		issueCount int
	}

	var started []startedCall
	var completed []completedCall
	var failed []string

	packet.EmitPersonaStarted = func(_ context.Context, persona state.PersonaKind, providerName, modelName string) {
		started = append(started, startedCall{persona: persona, provider: providerName, model: modelName})
	}
	packet.EmitPersonaCompleted = func(_ context.Context, persona state.PersonaKind, durationMs int64, summary string, blockingIssues []string) {
		completed = append(completed, completedCall{persona: persona, durationMs: durationMs, summary: summary, issueCount: len(blockingIssues)})
	}
	packet.EmitPersonaFailed = func(_ context.Context, persona state.PersonaKind, err string) {
		failed = append(failed, string(persona)+": "+err)
	}

	out, err := f.Execute(context.Background(), packet)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Finalization == nil {
		t.Fatal("expected finalization result")
	}
	if len(started) != 1 {
		t.Fatalf("expected 1 nested persona start callback, got %d", len(started))
	}
	if started[0].persona != state.PersonaRefiner {
		t.Fatalf("start persona = %q, want %q", started[0].persona, state.PersonaRefiner)
	}
	if started[0].provider != providerName || started[0].model != "mock-model" {
		t.Fatalf("unexpected nested persona routing: %+v", started[0])
	}
	if len(completed) != 1 {
		t.Fatalf("expected 1 nested persona completed callback, got %d", len(completed))
	}
	if completed[0].persona != state.PersonaRefiner {
		t.Fatalf("completed persona = %q, want %q", completed[0].persona, state.PersonaRefiner)
	}
	if completed[0].durationMs < 0 {
		t.Fatalf("duration must be non-negative, got %d", completed[0].durationMs)
	}
	if completed[0].summary != "retrospective complete" {
		t.Fatalf("summary = %q, want %q", completed[0].summary, "retrospective complete")
	}
	if len(failed) != 0 {
		t.Fatalf("unexpected nested persona failures: %v", failed)
	}
}

// ─── normalizeImprovements ────────────────────────────────────────────────────

func TestNormalizeImprovements_DropsBlankComponentName(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "implementer" {
		t.Errorf("wrong improvement retained: %+v", result[0])
	}
}

func TestNormalizeImprovements_DropsBlankComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "medium"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d: %+v", len(result), result)
	}
}

func TestNormalizeImprovements_DropsBlankProblem(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "", ProposedFix: "f", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsBlankProposedFix(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "p", ProposedFix: "", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsInvalidPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "urgent"},
		{ComponentType: "persona", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "medium"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "director" {
		t.Errorf("wrong improvement retained: %+v", result[0])
	}
}

func TestNormalizeImprovements_DropsBlankPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "qa", Problem: "p", ProposedFix: "f", Priority: ""},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements with blank priority, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsValidImprovement(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "prob", ProposedFix: "fix", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].ComponentName != "my-skill" {
		t.Errorf("unexpected component name: %q", result[0].ComponentName)
	}
}

func TestNormalizeImprovements_EmptyInput(t *testing.T) {
	result := normalizeImprovements(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements, got %d", len(result))
	}
}

func TestNormalizeImprovements_TrimsWhitespace(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "  persona  ", ComponentName: "  implementer  ", Problem: "  p  ", ProposedFix: "  f  ", Priority: "  high  "},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after trimming, got %d", len(result))
	}
	if result[0].ComponentType != "persona" {
		t.Errorf("ComponentType not trimmed: %q", result[0].ComponentType)
	}
	if result[0].ComponentName != "implementer" {
		t.Errorf("ComponentName not trimmed: %q", result[0].ComponentName)
	}
	if result[0].Priority != "high" {
		t.Errorf("Priority not trimmed/lowercased: %q", result[0].Priority)
	}
}

func TestNormalizeImprovements_NormalizesPriorityCase(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "HIGH"},
		{ComponentType: "persona", ComponentName: "director", Problem: "p", ProposedFix: "f", Priority: "Medium"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "LOW"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 3 {
		t.Fatalf("expected 3 improvements after case normalization, got %d", len(result))
	}
	if result[0].Priority != "high" {
		t.Errorf("expected 'high', got %q", result[0].Priority)
	}
	if result[1].Priority != "medium" {
		t.Errorf("expected 'medium', got %q", result[1].Priority)
	}
	if result[2].Priority != "low" {
		t.Errorf("expected 'low', got %q", result[2].Priority)
	}
}

func TestNormalizeImprovements_DropsPlaceholderComponentNames(t *testing.T) {
	placeholders := []string{"N/A", "n/a", "NA", "unknown", "placeholder", "TBD", "tbd"}
	for _, name := range placeholders {
		imps := []state.RefinerImprovement{
			{ComponentType: "persona", ComponentName: name, Problem: "p", ProposedFix: "f", Priority: "low"},
		}
		result := normalizeImprovements(imps)
		if len(result) != 0 {
			t.Errorf("component_name %q should be rejected as placeholder, got %d results", name, len(result))
		}
	}
}

// ─── deduplication ────────────────────────────────────────────────────────────

func TestNormalizeImprovements_DeduplicatesByComponentTypeAndName(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "p1", ProposedFix: "f1", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p2", ProposedFix: "f2", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "p3", ProposedFix: "f3", Priority: "low"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after dedup, got %d", len(result))
	}
	if result[0].Problem != "p1" {
		t.Errorf("expected first-seen entry retained, got problem=%q", result[0].Problem)
	}
}

func TestNormalizeImprovements_DeduplicateKeepsHigherPriority(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "low-p", ProposedFix: "f1", Priority: "low"},
		{ComponentType: "persona", ComponentName: "architect", Problem: "high-p", ProposedFix: "f2", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement after dedup, got %d", len(result))
	}
	if result[0].Priority != "high" {
		t.Errorf("expected higher-priority entry, got priority=%q problem=%q", result[0].Priority, result[0].Problem)
	}
}

func TestNormalizeImprovements_DifferentComponentsNotDeduped(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high"},
		{ComponentType: "skill", ComponentName: "architect", Problem: "p", ProposedFix: "f", Priority: "high"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 3 {
		t.Fatalf("expected 3 improvements (different keys), got %d", len(result))
	}
}

func TestNormalizeImprovements_PreservesContent(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "p", ProposedFix: "f", Priority: "high", Content: "# My Skill\n"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement, got %d", len(result))
	}
	if result[0].Content != "# My Skill" {
		// Content is trimmed.
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

// ─── surface policy filtering inside normalizeImprovements ───────────────────

func TestNormalizeImprovements_DropsAgentComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "agent", ComponentName: "my-agent", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "agents/my-agent.agent.md", Content: "# agent"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for agent type, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsWorkflowComponentType(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "workflow", ComponentName: "engine", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "advisory"},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for workflow type, got %d", len(result))
	}
}

func TestNormalizeImprovements_DropsGoSourceFilePath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "engine", Problem: "p", ProposedFix: "f", Priority: "high",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "internal/workflow/engine/engine.go", Content: "package engine"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 0 {
		t.Errorf("expected 0 improvements for Go source path, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsPersonaWithAllowedPath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "persona", ComponentName: "implementer", Problem: "p", ProposedFix: "f", Priority: "high",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "prompts/personas/implementer.md", Content: "# impl"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement for valid persona path, got %d", len(result))
	}
}

func TestNormalizeImprovements_KeepsSkillWithAllowedPath(t *testing.T) {
	imps := []state.RefinerImprovement{
		{ComponentType: "skill", ComponentName: "my-skill", Problem: "p", ProposedFix: "f", Priority: "low",
			ChangeType: "update",
			Files:      []state.ImprovementFile{{Path: "skills/my-skill/SKILL.md", Content: "---\nname: my-skill\ndescription: d\n---\n"}}},
	}
	result := normalizeImprovements(imps)
	if len(result) != 1 {
		t.Fatalf("expected 1 improvement for valid skill path, got %d", len(result))
	}
}
