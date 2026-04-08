package qa

import (
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

// ---------------------------------------------------------------------------
// classifyIssues
// ---------------------------------------------------------------------------

func TestClassifyIssues_TrulyBlocking(t *testing.T) {
	issues := []qaIssue{
		{
			Severity:       "blocking",
			Component:      "final_article",
			Description:    "Code blocks are missing from the performance section",
			Recommendation: "Add benchmark table and inline example from supporting code artifact",
		},
	}
	blocking, downgraded := classifyIssues(issues)
	if len(blocking) != 1 {
		t.Fatalf("expected 1 blocking, got %d", len(blocking))
	}
	if len(downgraded) != 0 {
		t.Fatalf("expected 0 downgraded, got %d", len(downgraded))
	}
	if !strings.Contains(blocking[0], "final_article") {
		t.Errorf("blocking message missing component: %q", blocking[0])
	}
}

func TestClassifyIssues_InvalidSeverityDowngraded(t *testing.T) {
	issues := []qaIssue{
		{
			Severity:       "warning",
			Component:      "outline",
			Description:    "Minor structural note",
			Recommendation: "Reorder the introduction",
		},
		{
			Severity:       "info",
			Component:      "code_helper",
			Description:    "Optional helper note",
			Recommendation: "Add a comment explaining the generics constraint",
		},
	}
	blocking, downgraded := classifyIssues(issues)
	if len(blocking) != 0 {
		t.Fatalf("expected 0 blocking, got %d: %v", len(blocking), blocking)
	}
	if len(downgraded) != 2 {
		t.Fatalf("expected 2 downgraded, got %d", len(downgraded))
	}
}

func TestClassifyIssues_NoneRecommendationDowngraded(t *testing.T) {
	cases := []string{"None", "none", "N/A", "n/a", "no recommendation", ""}
	for _, rec := range cases {
		issues := []qaIssue{
			{
				Severity:       "blocking",
				Component:      "final_article",
				Description:    "Technically excellent — passes the technical bar",
				Recommendation: rec,
			},
		}
		blocking, downgraded := classifyIssues(issues)
		if len(blocking) != 0 {
			t.Errorf("rec=%q: expected 0 blocking, got %d: %v", rec, len(blocking), blocking)
		}
		if len(downgraded) != 1 {
			t.Errorf("rec=%q: expected 1 downgraded, got %d", rec, len(downgraded))
		}
	}
}

func TestClassifyIssues_PhantomEmptyDropped(t *testing.T) {
	issues := []qaIssue{
		{Severity: "blocking", Component: "", Description: "", Recommendation: ""},
		{Severity: "blocking", Component: "", Description: "", Recommendation: "something"},
	}
	blocking, downgraded := classifyIssues(issues)
	// Both should be dropped (empty component AND description)
	if len(blocking) != 0 {
		t.Errorf("expected 0 blocking, got %d", len(blocking))
	}
	if len(downgraded) != 0 {
		t.Errorf("expected 0 downgraded, got %d", len(downgraded))
	}
}

// ---------------------------------------------------------------------------
// isEditorialOnly — regression patterns from workflow a2ffa163
// ---------------------------------------------------------------------------

func TestIsEditorialOnly_TitleTone(t *testing.T) {
	iss := qaIssue{
		Severity:       "blocking",
		Component:      "final_article",
		Description:    "Title is slightly promotional — recommend a more neutral title",
		Recommendation: "Change title to a more academic, more neutral title",
	}
	if !isEditorialOnly(iss) {
		t.Error("title-tone issue should be editorial only")
	}
}

func TestIsEditorialOnly_NextStep(t *testing.T) {
	iss := qaIssue{
		Severity:       "blocking",
		Component:      "final_article",
		Description:    "The concluding paragraph could suggest an advanced topic as a next step",
		Recommendation: "Add a next step recommendation in the conclusion",
	}
	if !isEditorialOnly(iss) {
		t.Error("next-step/concluding-challenge issue should be editorial only")
	}
}

func TestIsEditorialOnly_PassesTechnicalBar(t *testing.T) {
	iss := qaIssue{
		Severity:       "blocking",
		Component:      "final_article",
		Description:    "Technically excellent — passes the technical bar",
		Recommendation: "None",
	}
	if !isEditorialOnly(iss) {
		t.Error("'passes the technical bar' item should be editorial only")
	}
}

func TestIsEditorialOnly_StructuralDefect(t *testing.T) {
	iss := qaIssue{
		Severity:       "blocking",
		Component:      "final_article",
		Description:    "Performance section is missing benchmark data referenced in the outline",
		Recommendation: "Add the benchmark table with concrete numbers",
	}
	if isEditorialOnly(iss) {
		t.Error("structural defect should NOT be classified as editorial only")
	}
}

func TestIsEditorialOnly_PlaceholderInContent(t *testing.T) {
	iss := qaIssue{
		Severity:       "blocking",
		Component:      "final_article",
		Description:    "Article contains [CODE REFERENCE: ...] placeholder text — not self-contained",
		Recommendation: "Inline the referenced code directly into the article",
	}
	if isEditorialOnly(iss) {
		t.Error("placeholder-in-content defect should NOT be editorial only")
	}
}

// ---------------------------------------------------------------------------
// buildContentArtifactSummary
// ---------------------------------------------------------------------------

func makeArtifact(name string, kind state.ArtifactKind, content string) state.Artifact {
	return state.Artifact{Name: name, Kind: kind, Description: "desc:" + name, Content: content}
}

func TestBuildContentArtifactSummary_Empty(t *testing.T) {
	got := buildContentArtifactSummary(nil)
	if got != "(no artifacts produced)" {
		t.Errorf("unexpected: %q", got)
	}
}

func TestBuildContentArtifactSummary_BlogPostIsCandidate(t *testing.T) {
	artifacts := []state.Artifact{
		makeArtifact("outline", state.ArtifactKindMarkdown, "# Outline"),
		makeArtifact("code-helper", state.ArtifactKindCode, "package main"),
		makeArtifact("final-post", state.ArtifactKindBlogPost, "# Go Generics\n\nThe body."),
	}
	got := buildContentArtifactSummary(artifacts)

	if !strings.Contains(got, "DELIVERY CANDIDATE") {
		t.Error("expected DELIVERY CANDIDATE header")
	}
	if !strings.Contains(got, "final-post") {
		t.Error("expected delivery candidate name in output")
	}
	if !strings.Contains(got, "Supporting artifacts") {
		t.Error("expected supporting artifacts section")
	}
	// outline and code-helper should be in supporting, not as delivery candidate
	if strings.Contains(strings.Split(got, "Supporting")[0], "outline") {
		t.Error("outline should be in supporting section, not in candidate section")
	}
}

func TestBuildContentArtifactSummary_LatestBlogPostWins(t *testing.T) {
	artifacts := []state.Artifact{
		makeArtifact("first-draft", state.ArtifactKindBlogPost, "first draft content"),
		makeArtifact("remediation-draft", state.ArtifactKindMarkdown, "remediation content"),
		makeArtifact("final-draft", state.ArtifactKindBlogPost, "final draft content"),
	}
	got := buildContentArtifactSummary(artifacts)

	if !strings.Contains(got, "final-draft") {
		t.Error("expected latest blog_post to be candidate")
	}
	// first-draft must be in supporting, not the candidate
	candidatePart := strings.Split(got, "Supporting")[0]
	if strings.Contains(candidatePart, "first-draft") {
		t.Error("first-draft should be in supporting artifacts, not candidate section")
	}
}

func TestBuildContentArtifactSummary_FallbackToLatestMarkdown(t *testing.T) {
	artifacts := []state.Artifact{
		makeArtifact("outline", state.ArtifactKindMarkdown, "# Outline\ncontent"),
		makeArtifact("synthesis", state.ArtifactKindMarkdown, "# Final\nfinal content"),
		makeArtifact("code", state.ArtifactKindCode, "package main"),
	}
	got := buildContentArtifactSummary(artifacts)

	if !strings.Contains(got, "DELIVERY CANDIDATE") {
		t.Error("expected DELIVERY CANDIDATE header for markdown fallback")
	}
	if !strings.Contains(got, "synthesis") {
		t.Error("expected latest markdown (synthesis) to be candidate")
	}
	candidatePart := strings.Split(got, "Supporting")[0]
	if strings.Contains(candidatePart, "outline") {
		t.Error("outline should be in supporting artifacts, not candidate section")
	}
}

func TestBuildContentArtifactSummary_NoBlogPostNorMarkdown_FallsBack(t *testing.T) {
	artifacts := []state.Artifact{
		makeArtifact("code", state.ArtifactKindCode, "package main"),
		makeArtifact("config", state.ArtifactKindConfig, "key: value"),
	}
	// Should fall back to buildArtifactSummary (all artifacts listed)
	got := buildContentArtifactSummary(artifacts)
	if strings.Contains(got, "DELIVERY CANDIDATE") {
		t.Error("no blog_post/markdown: should NOT have DELIVERY CANDIDATE section")
	}
	if !strings.Contains(got, "code") {
		t.Error("fallback should include all artifacts")
	}
}

func TestBuildContentArtifactSummary_SupportingArtifactsNotUnderEvaluation(t *testing.T) {
	artifacts := []state.Artifact{
		makeArtifact("outline", state.ArtifactKindMarkdown, "outline body"),
		makeArtifact("blog-post", state.ArtifactKindBlogPost, "blog body"),
	}
	got := buildContentArtifactSummary(artifacts)

	if !strings.Contains(got, "NOT under evaluation") {
		t.Error("supporting artifacts must be labelled 'NOT under evaluation'")
	}
}
