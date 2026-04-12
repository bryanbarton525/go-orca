package implementer

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestNormalizeArtifactKind_ContentPlainTextBecomesBlogPost(t *testing.T) {
	got := normalizeArtifactKind(state.WorkflowModeContent, "plain_text")
	if got != state.ArtifactKindBlogPost {
		t.Fatalf("normalizeArtifactKind(content, plain_text) = %q, want %q", got, state.ArtifactKindBlogPost)
	}
}

func TestNormalizeArtifactKind_UnknownSoftwareDefaultsToDocument(t *testing.T) {
	got := normalizeArtifactKind(state.WorkflowModeSoftware, "unknown_kind")
	if got != state.ArtifactKindDocument {
		t.Fatalf("normalizeArtifactKind(software, unknown_kind) = %q, want %q", got, state.ArtifactKindDocument)
	}
}

func TestLatestSynthesisArtifact_FallsBackToTextualArtifact(t *testing.T) {
	artifacts := []state.Artifact{
		{Kind: state.ArtifactKindCode, Name: "main.go"},
		{Kind: state.ArtifactKindMarkdown, Name: "draft.md", Content: "draft"},
	}

	got := latestSynthesisArtifact(artifacts)
	if got == nil {
		t.Fatal("expected textual synthesis artifact, got nil")
	}
	if got.Name != "draft.md" {
		t.Fatalf("latestSynthesisArtifact().Name = %q, want %q", got.Name, "draft.md")
	}
}
