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

func TestValidateOutput_SoftwareSummaryOnlyFails(t *testing.T) {
	out := &implOutput{Summary: "wrote main.go and go.mod"}

	err := validateOutput(state.WorkflowModeSoftware, "write source files", out)
	if err == nil {
		t.Fatal("expected software summary-only output to fail")
	}
	if got := err.Error(); got != "pod: model produced summary-only output for software task \"write source files\" — write the files or return artifact content" {
		t.Fatalf("validateOutput() error = %q", got)
	}
	if out.Content != "" {
		t.Fatalf("validateOutput() mutated content = %q, want empty string", out.Content)
	}
}

func TestValidateOutput_ContentWins(t *testing.T) {
	out := &implOutput{Content: "package main", Summary: "wrote main.go"}

	if err := validateOutput(state.WorkflowModeSoftware, "write source files", out); err != nil {
		t.Fatalf("validateOutput() error = %v, want nil", err)
	}
}
