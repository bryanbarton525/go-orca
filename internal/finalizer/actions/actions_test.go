package actions_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-orca/go-orca/internal/finalizer/actions"
	"github.com/go-orca/go-orca/internal/state"
)

func makeInput(ws *state.WorkflowState, arts []state.Artifact, cfg interface{}) actions.Input {
	var rawCfg json.RawMessage
	if cfg != nil {
		rawCfg, _ = json.Marshal(cfg)
	}
	return actions.Input{Workflow: ws, Artifacts: arts, Config: rawCfg}
}

func testWorkflow() *state.WorkflowState {
	ws := state.NewWorkflowState("t1", "s1", "test workflow")
	ws.Title = "Test Workflow"
	return ws
}

// ─── MarkdownExportAction ─────────────────────────────────────────────────────

func TestMarkdownExportAction(t *testing.T) {
	a := &actions.MarkdownExportAction{}
	if a.Kind() != actions.ActionMarkdownExport {
		t.Errorf("Kind: got %q", a.Kind())
	}

	ws := testWorkflow()
	ws.Constitution = &state.Constitution{Vision: "Build great things"}
	arts := []state.Artifact{
		{Name: "spec.md", Content: "# Spec", Kind: state.ArtifactKindMarkdown},
	}

	out, err := a.Execute(context.Background(), makeInput(ws, arts, nil))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Success {
		t.Errorf("expected Success=true, got error: %s", out.Error)
	}
	content := out.Metadata["content"]
	if content == "" {
		t.Error("expected non-empty content in metadata")
	}
}

// ─── ArtifactBundleAction ─────────────────────────────────────────────────────

func TestArtifactBundleAction(t *testing.T) {
	a := &actions.ArtifactBundleAction{}
	arts := []state.Artifact{
		{Name: "a.md", Kind: state.ArtifactKindMarkdown},
		{Name: "b.md", Kind: state.ArtifactKindDocument},
	}
	out, err := a.Execute(context.Background(), makeInput(testWorkflow(), arts, nil))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Success {
		t.Errorf("expected Success=true")
	}
	if len(out.Metadata) != 2 {
		t.Errorf("metadata count: got %d, want 2", len(out.Metadata))
	}
}

// ─── BlogDraftAction ──────────────────────────────────────────────────────────

func TestBlogDraftAction_Found(t *testing.T) {
	a := &actions.BlogDraftAction{}
	arts := []state.Artifact{
		{Name: "post.md", Kind: state.ArtifactKindBlogPost, Content: "# My Post"},
	}
	out, err := a.Execute(context.Background(), makeInput(testWorkflow(), arts, nil))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Success {
		t.Errorf("expected Success=true; error: %s", out.Error)
	}
	if out.Metadata["draft"] != "# My Post" {
		t.Errorf("draft content mismatch: %q", out.Metadata["draft"])
	}
}

func TestBlogDraftAction_NotFound(t *testing.T) {
	a := &actions.BlogDraftAction{}
	out, err := a.Execute(context.Background(), makeInput(testWorkflow(), nil, nil))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Success {
		t.Error("expected Success=false when no blog_post artifact")
	}
}

// ─── WebhookAction ────────────────────────────────────────────────────────────

func TestWebhookAction_Success(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &actions.WebhookAction{}
	in := makeInput(testWorkflow(), []state.Artifact{
		{Name: "art.md", Kind: state.ArtifactKindMarkdown, Content: "hello"},
	}, map[string]string{"url": srv.URL + "/hook"})

	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Success {
		t.Errorf("expected Success=true; error: %s", out.Error)
	}
	if len(received) == 0 {
		t.Error("expected webhook to receive a request body")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(received, &payload); err != nil {
		t.Fatalf("webhook body not valid JSON: %v", err)
	}
	if _, ok := payload["workflow_id"]; !ok {
		t.Error("payload missing workflow_id")
	}
}

func TestWebhookAction_MissingURL(t *testing.T) {
	a := &actions.WebhookAction{}
	_, err := a.Execute(context.Background(), makeInput(testWorkflow(), nil, nil))
	if err == nil {
		t.Error("expected error for missing URL, got nil")
	}
}

func TestWebhookAction_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &actions.WebhookAction{}
	in := makeInput(testWorkflow(), nil, map[string]string{"url": srv.URL})
	out, err := a.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Success {
		t.Error("expected Success=false for 500 response")
	}
}

// ─── Global registry ──────────────────────────────────────────────────────────

func TestGlobalRegistryContainsAll(t *testing.T) {
	kinds := []actions.ActionKind{
		actions.ActionMarkdownExport,
		actions.ActionArtifactBundle,
		actions.ActionBlogDraft,
		actions.ActionWebhook,
		actions.ActionGitHubPR,
		actions.ActionRepoCommit,
	}
	for _, k := range kinds {
		if _, ok := actions.Global.Get(k); !ok {
			t.Errorf("Global registry missing action %q", k)
		}
	}
}
