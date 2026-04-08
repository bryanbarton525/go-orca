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

// TestBlogDraftAction_LatestBlogPostWins verifies that when multiple blog_post
// artifacts are present the newest one (highest index, last appended) wins.
func TestBlogDraftAction_LatestBlogPostWins(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindBlogPost, "draft-v1.md", "first draft"),
			makeArtifact(state.ArtifactKindBlogPost, "draft-v2.md", "second draft"),
			makeArtifact(state.ArtifactKindBlogPost, "draft-final.md", "final draft"),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success: %s", out.Error)
	}
	if out.Metadata["draft"] != "final draft" {
		t.Errorf("expected latest blog_post to win; got draft: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] == "true" {
		t.Error("fallback flag must not be set when blog_post is found")
	}
}

// TestBlogDraftAction_LatestMarkdownFallbackWins verifies that when multiple
// markdown artifacts are present (no blog_post) the newest one wins.
func TestBlogDraftAction_LatestMarkdownFallbackWins(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindMarkdown, "section-a.md", "section a content"),
			makeArtifact(state.ArtifactKindMarkdown, "section-b.md", "section b content"),
			makeArtifact(state.ArtifactKindMarkdown, "final-synthesis.md", "complete synthesis"),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success via markdown fallback: %s", out.Error)
	}
	if out.Metadata["draft"] != "complete synthesis" {
		t.Errorf("expected latest markdown to win; got draft: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] != "true" {
		t.Error("fallback flag must be set when markdown is used")
	}
}

// TestBlogDraftAction_BlogPostWinsOverNewerMarkdown verifies that blog_post
// kind always wins over markdown regardless of order, because blog_post is
// the primary scan pass (newest-to-oldest for blog_post first).
func TestBlogDraftAction_BlogPostWinsOverNewerMarkdown(t *testing.T) {
	a := &BlogDraftAction{}
	in := Input{
		Workflow: makeWorkflow(),
		Artifacts: []state.Artifact{
			makeArtifact(state.ArtifactKindBlogPost, "draft.md", "blog post content"),
			// A later markdown should NOT override the blog_post.
			makeArtifact(state.ArtifactKindMarkdown, "synthesis.md", "markdown synthesis appended later"),
		},
	}
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success: %s", out.Error)
	}
	if out.Metadata["draft"] != "blog post content" {
		t.Errorf("blog_post must win even when a newer markdown is appended; got: %q", out.Metadata["draft"])
	}
	if out.Metadata["fallback"] == "true" {
		t.Error("fallback must not be set when blog_post is found")
	}
}
