package actions

import (
	"context"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func makeWorkflow() *state.WorkflowState {
	return &state.WorkflowState{ID: "test-wf", Title: "Test Workflow"}
}

func makeArtifact(kind state.ArtifactKind, name, content string) state.Artifact {
	return state.Artifact{Kind: kind, Name: name, Content: content}
}

// ─── BlogDraftAction ──────────────────────────────────────────────────────────

func TestBlogDraftAction_BlogPostArtifact(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindBlogPost, "post.md", "# Hello World\nContent here."),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success, got error: %s", out.Error)
	}
	if out.Metadata["draft"] != "# Hello World\nContent here." {
		t.Errorf("unexpected draft content: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] == "true" {
		t.Error("fallback flag should not be set when a blog_post artifact is present")
	}
}

func TestBlogDraftAction_MarkdownFallback(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindMarkdown, "article.md", "# Article\nBody text."),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success via markdown fallback, got error: %s", out.Error)
	}
	if out.Metadata["draft"] != "# Article\nBody text." {
		t.Errorf("unexpected draft content: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] != "true" {
		t.Error("fallback flag should be set when markdown artifact is used")
	}
}

func TestBlogDraftAction_BlogPostPreferredOverMarkdown(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindMarkdown, "other.md", "generic markdown"),
			makeArtifact(state.ArtifactKindBlogPost, "post.md", "the real post"),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success: %s", out.Error)
	}
	if out.Metadata["draft"] != "the real post" {
		t.Errorf("blog_post artifact should be preferred; got draft: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] == "true" {
		t.Error("fallback flag should not be set when blog_post artifact is present")
	}
}

func TestBlogDraftAction_NoArtifacts(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{Workflow: makeWorkflow(), Artifacts: nil}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Error("expected failure when no artifacts present")
	}
	if out.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBlogDraftAction_NoMatchingArtifact(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindCode, "main.go", "package main"),
			makeArtifact(state.ArtifactKindConfig, "config.yaml", "key: val"),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Success {
		t.Error("expected failure when no blog_post or markdown artifact present")
	}
	if out.Error == "" {
		t.Error("expected non-empty error message")
	}
}
