package engine

import (
	"context"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

type noopStore struct{}

func (noopStore) GetWorkflow(context.Context, string) (*state.WorkflowState, error) { return nil, nil }
func (noopStore) SaveWorkflow(context.Context, *state.WorkflowState) error          { return nil }
func (noopStore) AppendEvents(context.Context, ...*events.Event) error              { return nil }

func TestLatestArtifactsByLogicalFile_PrefersLatestByPathThenNameKind(t *testing.T) {
	now := time.Now().UTC()
	artifacts := []state.Artifact{
		{
			ID:        "a1",
			Name:      "sum_even.go",
			Path:      "pkg/sum_even.go",
			Kind:      state.ArtifactKindCode,
			Content:   "version one",
			CreatedAt: now,
		},
		{
			ID:        "a2",
			Name:      "notes.md",
			Kind:      state.ArtifactKindMarkdown,
			Content:   "draft one",
			CreatedAt: now,
		},
		{
			ID:        "a3",
			Name:      "sum_even.go",
			Path:      "pkg/sum_even.go",
			Kind:      state.ArtifactKindCode,
			Content:   "version two",
			CreatedAt: now,
		},
		{
			ID:        "a4",
			Name:      "notes.md",
			Kind:      state.ArtifactKindMarkdown,
			Content:   "draft two",
			CreatedAt: now,
		},
		{
			ID:        "a5",
			Name:      "notes.md",
			Kind:      state.ArtifactKindReport,
			Content:   "report copy",
			CreatedAt: now,
		},
	}

	got := latestArtifactsByLogicalFile(artifacts)
	if len(got) != 3 {
		t.Fatalf("expected 3 artifacts after dedupe, got %d", len(got))
	}
	if got[0].ID != "a3" || got[0].Content != "version two" {
		t.Fatalf("expected latest code artifact first, got ID=%q content=%q", got[0].ID, got[0].Content)
	}
	if got[1].ID != "a4" || got[1].Content != "draft two" {
		t.Fatalf("expected latest markdown artifact second, got ID=%q content=%q", got[1].ID, got[1].Content)
	}
	if got[2].ID != "a5" {
		t.Fatalf("expected report artifact to remain distinct, got ID=%q", got[2].ID)
	}
}

func TestBuildPacket_DeduplicatesArtifactsForQA(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock-model"})
	ws := state.NewWorkflowState("tenant-1", "scope-1", "sum even")
	ws.ProviderName = "mock"
	ws.ModelName = "mock-model"
	ws.Artifacts = []state.Artifact{
		{Name: "sum_even.go", Path: "pkg/sum_even.go", Kind: state.ArtifactKindCode, Content: "old"},
		{Name: "sum_even.go", Path: "pkg/sum_even.go", Kind: state.ArtifactKindCode, Content: "new"},
		{Name: "sum_even_test.go", Kind: state.ArtifactKindCode, Content: "tests"},
	}

	packet := eng.buildPacket(ws, state.PersonaQA, nil)
	if len(packet.Artifacts) != 2 {
		t.Fatalf("expected 2 QA artifacts after dedupe, got %d", len(packet.Artifacts))
	}
	if packet.Artifacts[0].Content != "new" {
		t.Fatalf("expected latest code artifact in QA packet, got %q", packet.Artifacts[0].Content)
	}
	if packet.Artifacts[1].Name != "sum_even_test.go" {
		t.Fatalf("expected test artifact to remain in QA packet, got %q", packet.Artifacts[1].Name)
	}
}

func TestBuildPacket_PreservesArtifactHistoryOutsideQA(t *testing.T) {
	eng := New(noopStore{}, Options{DefaultProvider: "mock", DefaultModel: "mock-model"})
	ws := state.NewWorkflowState("tenant-1", "scope-1", "sum even")
	ws.ProviderName = "mock"
	ws.ModelName = "mock-model"
	ws.Artifacts = []state.Artifact{
		{Name: "sum_even.go", Path: "pkg/sum_even.go", Kind: state.ArtifactKindCode, Content: "old"},
		{Name: "sum_even.go", Path: "pkg/sum_even.go", Kind: state.ArtifactKindCode, Content: "new"},
		{Name: "sum_even_test.go", Kind: state.ArtifactKindCode, Content: "tests"},
	}

	packet := eng.buildPacket(ws, state.PersonaArchitect, nil)
	if len(packet.Artifacts) != len(ws.Artifacts) {
		t.Fatalf("expected non-QA packet to preserve all %d artifacts, got %d", len(ws.Artifacts), len(packet.Artifacts))
	}
	if packet.Artifacts[0].Content != "old" || packet.Artifacts[1].Content != "new" {
		t.Fatalf("expected non-QA packet to preserve artifact history, got %#v", packet.Artifacts)
	}
}
