// Package actions defines the pluggable Finalizer delivery action interface
// and a registry of built-in actions.
//
// Supported delivery actions:
//   - github-pr:        open a GitHub pull request
//   - repo-commit-only: commit artifacts without opening a PR
//   - artifact-bundle:  package artifacts into an archive
//   - markdown-export:  render a single markdown document
//   - blog-draft:       produce a blog post draft artifact
//   - webhook-dispatch: POST artifacts and metadata to a webhook URL
package actions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-orca/go-orca/internal/state"
)

// ActionKind is the canonical delivery action identifier.
type ActionKind string

const (
	ActionGitHubPR       ActionKind = "github-pr"
	ActionRepoCommit     ActionKind = "repo-commit-only"
	ActionArtifactBundle ActionKind = "artifact-bundle"
	ActionMarkdownExport ActionKind = "markdown-export"
	ActionBlogDraft      ActionKind = "blog-draft"
	ActionWebhook        ActionKind = "webhook-dispatch"
)

// Input is the data passed to a delivery action.
type Input struct {
	Workflow  *state.WorkflowState
	Artifacts []state.Artifact
	// Config is the action-specific configuration (e.g. target repo, webhook URL).
	Config json.RawMessage
}

// Output is the result from a delivery action.
type Output struct {
	Action   ActionKind        `json:"action"`
	Success  bool              `json:"success"`
	Links    []string          `json:"links,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Message  string            `json:"message,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// Action is the interface all delivery actions must implement.
type Action interface {
	Kind() ActionKind
	Description() string
	Execute(ctx context.Context, in Input) (*Output, error)
}

// Registry holds registered delivery actions.
type Registry struct {
	mu      sync.RWMutex
	actions map[ActionKind]Action
}

// NewRegistry creates an empty action registry.
func NewRegistry() *Registry {
	return &Registry{actions: make(map[ActionKind]Action)}
}

// Register adds an action. Panics on duplicate.
func (r *Registry) Register(a Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.actions[a.Kind()]; exists {
		panic("actions: duplicate registration: " + string(a.Kind()))
	}
	r.actions[a.Kind()] = a
}

// Get returns the named action, or (nil, false).
func (r *Registry) Get(kind ActionKind) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions[kind]
	return a, ok
}

// Execute dispatches to the named action, or returns an error if not found.
func (r *Registry) Execute(ctx context.Context, kind ActionKind, in Input) (*Output, error) {
	a, ok := r.Get(kind)
	if !ok {
		return nil, fmt.Errorf("actions: %q not registered", kind)
	}
	return a.Execute(ctx, in)
}

// ─── Built-in stub actions ────────────────────────────────────────────────────
// These stubs produce informational output and are replaced by real
// implementations as the project matures.

// MarkdownExportAction bundles all artifact content into a single markdown doc.
type MarkdownExportAction struct{}

func (a *MarkdownExportAction) Kind() ActionKind { return ActionMarkdownExport }
func (a *MarkdownExportAction) Description() string {
	return "Export all artifacts as a single markdown document."
}

func (a *MarkdownExportAction) Execute(_ context.Context, in Input) (*Output, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", in.Workflow.Title))
	if in.Workflow.Constitution != nil {
		sb.WriteString(fmt.Sprintf("## Vision\n%s\n\n", in.Workflow.Constitution.Vision))
	}
	sb.WriteString("## Artifacts\n\n")
	for _, a := range in.Artifacts {
		sb.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", a.Name, a.Content))
	}
	return &Output{
		Action:  ActionMarkdownExport,
		Success: true,
		Message: "Markdown document generated (inline).",
		Metadata: map[string]string{
			"content": sb.String(),
		},
	}, nil
}

// ArtifactBundleAction records all artifact metadata (full bundling requires storage).
type ArtifactBundleAction struct{}

func (a *ArtifactBundleAction) Kind() ActionKind { return ActionArtifactBundle }
func (a *ArtifactBundleAction) Description() string {
	return "Package artifacts into a downloadable bundle."
}

func (a *ArtifactBundleAction) Execute(_ context.Context, in Input) (*Output, error) {
	meta := make(map[string]string, len(in.Artifacts))
	for _, art := range in.Artifacts {
		meta[art.Name] = string(art.Kind)
	}
	return &Output{
		Action:   ActionArtifactBundle,
		Success:  true,
		Message:  fmt.Sprintf("Bundle manifest created (%d artifacts).", len(in.Artifacts)),
		Metadata: meta,
	}, nil
}

// BlogDraftAction extracts the first blog_post artifact as the draft.
type BlogDraftAction struct{}

func (a *BlogDraftAction) Kind() ActionKind    { return ActionBlogDraft }
func (a *BlogDraftAction) Description() string { return "Produce a publication-ready blog post draft." }

func (a *BlogDraftAction) Execute(_ context.Context, in Input) (*Output, error) {
	for _, art := range in.Artifacts {
		if art.Kind == state.ArtifactKindBlogPost {
			return &Output{
				Action:  ActionBlogDraft,
				Success: true,
				Message: fmt.Sprintf("Blog draft: %s", art.Name),
				Metadata: map[string]string{
					"draft": art.Content,
				},
			}, nil
		}
	}
	return &Output{
		Action:  ActionBlogDraft,
		Success: false,
		Error:   "no blog_post artifact found",
	}, nil
}

// WebhookAction POSTs workflow metadata and artifacts to a configured URL.
type WebhookAction struct{}

func (a *WebhookAction) Kind() ActionKind { return ActionWebhook }
func (a *WebhookAction) Description() string {
	return "POST artifacts and metadata to a configured webhook URL."
}

func (a *WebhookAction) Execute(ctx context.Context, in Input) (*Output, error) {
	var cfg struct {
		URL string `json:"url"`
	}
	if in.Config != nil {
		_ = json.Unmarshal(in.Config, &cfg)
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("actions: webhook-dispatch requires config.url")
	}

	type artifactSummary struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	type payload struct {
		WorkflowID string            `json:"workflow_id"`
		TenantID   string            `json:"tenant_id"`
		Title      string            `json:"title"`
		Status     string            `json:"status"`
		Artifacts  []artifactSummary `json:"artifacts"`
	}

	arts := make([]artifactSummary, 0, len(in.Artifacts))
	for _, art := range in.Artifacts {
		arts = append(arts, artifactSummary{Name: art.Name, Kind: string(art.Kind), Content: art.Content})
	}
	body := payload{
		WorkflowID: in.Workflow.ID,
		TenantID:   in.Workflow.TenantID,
		Title:      in.Workflow.Title,
		Status:     string(in.Workflow.Status),
		Artifacts:  arts,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("actions: webhook marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("actions: webhook create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &Output{
			Action:  ActionWebhook,
			Success: false,
			Error:   fmt.Sprintf("webhook POST failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return &Output{
			Action:  ActionWebhook,
			Success: false,
			Error:   fmt.Sprintf("webhook responded %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	return &Output{
		Action:  ActionWebhook,
		Success: true,
		Links:   []string{cfg.URL},
		Message: fmt.Sprintf("Webhook dispatched to %s (%d)", cfg.URL, resp.StatusCode),
	}, nil
}

// GitHubPRAction opens a GitHub pull request containing all artifacts.
// Config fields:
//
//	repo         – "owner/repo" (required)
//	base_branch  – branch to merge into (default: "main")
//	head_branch  – new branch to create (required)
//	title        – PR title (defaults to workflow title)
//	body         – PR body text (optional)
//	path         – directory prefix for artifact files (default: "")
//	token        – GitHub PAT (falls back to env GITHUB_TOKEN)
type GitHubPRAction struct{}

func (a *GitHubPRAction) Kind() ActionKind { return ActionGitHubPR }
func (a *GitHubPRAction) Description() string {
	return "Open a GitHub pull request with all artifacts."
}

func (a *GitHubPRAction) Execute(ctx context.Context, in Input) (*Output, error) {
	var cfg struct {
		Repo       string `json:"repo"`
		BaseBranch string `json:"base_branch"`
		HeadBranch string `json:"head_branch"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Path       string `json:"path"`
		Token      string `json:"token"`
	}
	if in.Config != nil {
		_ = json.Unmarshal(in.Config, &cfg)
	}
	if cfg.Repo == "" {
		return nil, fmt.Errorf("actions: github-pr requires config.repo (\"owner/repo\")")
	}
	if cfg.HeadBranch == "" {
		return nil, fmt.Errorf("actions: github-pr requires config.head_branch")
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.Title == "" {
		cfg.Title = in.Workflow.Title
	}
	token := cfg.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("actions: github-pr requires a GitHub token (config.token or GITHUB_TOKEN env)")
	}

	ghDo := func(method, url string, reqBody interface{}) ([]byte, int, error) {
		var bodyReader io.Reader
		if reqBody != nil {
			b, err := json.Marshal(reqBody)
			if err != nil {
				return nil, 0, err
			}
			bodyReader = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode, nil
	}

	apiBase := "https://api.github.com/repos/" + cfg.Repo

	// 1. Resolve base branch SHA.
	refBody, refStatus, err := ghDo(http.MethodGet,
		apiBase+"/git/ref/heads/"+cfg.BaseBranch, nil)
	if err != nil {
		return nil, fmt.Errorf("actions: github-pr get base ref: %w", err)
	}
	if refStatus != http.StatusOK {
		return nil, fmt.Errorf("actions: github-pr get base ref %d: %s", refStatus, string(refBody))
	}
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(refBody, &refResp); err != nil {
		return nil, fmt.Errorf("actions: github-pr parse ref: %w", err)
	}
	baseSHA := refResp.Object.SHA

	// 2. Create head branch.
	createRefBody, createRefStatus, err := ghDo(http.MethodPost, apiBase+"/git/refs", map[string]string{
		"ref": "refs/heads/" + cfg.HeadBranch,
		"sha": baseSHA,
	})
	if err != nil {
		return nil, fmt.Errorf("actions: github-pr create branch: %w", err)
	}
	if createRefStatus != http.StatusCreated && createRefStatus != http.StatusUnprocessableEntity {
		// 422 = already exists, treat as recoverable.
		return nil, fmt.Errorf("actions: github-pr create branch %d: %s", createRefStatus, string(createRefBody))
	}

	// 3. Commit each artifact via Contents API.
	var committed []string
	for _, art := range in.Artifacts {
		filePath := art.Path
		if filePath == "" {
			filePath = art.Name
		}
		if cfg.Path != "" {
			filePath = strings.TrimRight(cfg.Path, "/") + "/" + filePath
		}
		content := base64.StdEncoding.EncodeToString([]byte(art.Content))
		putBody := map[string]string{
			"message": "add " + art.Name,
			"content": content,
			"branch":  cfg.HeadBranch,
		}
		putData, putStatus, err := ghDo(http.MethodPut,
			apiBase+"/contents/"+filePath, putBody)
		if err != nil {
			return nil, fmt.Errorf("actions: github-pr put %s: %w", filePath, err)
		}
		if putStatus != http.StatusCreated && putStatus != http.StatusOK {
			return nil, fmt.Errorf("actions: github-pr put %s (%d): %s", filePath, putStatus, string(putData))
		}
		committed = append(committed, filePath)
	}

	// 4. Open the PR.
	prPayload := map[string]string{
		"title": cfg.Title,
		"body":  cfg.Body,
		"head":  cfg.HeadBranch,
		"base":  cfg.BaseBranch,
	}
	prData, prStatus, err := ghDo(http.MethodPost, apiBase+"/pulls", prPayload)
	if err != nil {
		return nil, fmt.Errorf("actions: github-pr create pr: %w", err)
	}
	if prStatus != http.StatusCreated {
		return nil, fmt.Errorf("actions: github-pr create pr %d: %s", prStatus, string(prData))
	}
	var prResp struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	_ = json.Unmarshal(prData, &prResp)

	return &Output{
		Action:  ActionGitHubPR,
		Success: true,
		Links:   []string{prResp.HTMLURL},
		Message: fmt.Sprintf("PR #%d opened: %s (%d files committed)", prResp.Number, prResp.HTMLURL, len(committed)),
		Metadata: map[string]string{
			"pr_url":      prResp.HTMLURL,
			"head_branch": cfg.HeadBranch,
			"base_branch": cfg.BaseBranch,
		},
	}, nil
}

// RepoCommitAction commits artifacts to a repository branch without opening a PR.
// Config fields:
//
//	repo   – "owner/repo" (required)
//	branch – target branch name (default: "main")
//	path   – subdirectory prefix for artifact files (default: "")
//	token  – GitHub personal access token (required; overrides env GITHUB_TOKEN)
type RepoCommitAction struct{}

func (a *RepoCommitAction) Kind() ActionKind { return ActionRepoCommit }
func (a *RepoCommitAction) Description() string {
	return "Commit artifacts directly to a repository branch without creating a PR."
}

func (a *RepoCommitAction) Execute(ctx context.Context, in Input) (*Output, error) {
	var cfg struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		Path   string `json:"path"`
		Token  string `json:"token"`
	}
	if in.Config != nil {
		_ = json.Unmarshal(in.Config, &cfg)
	}
	if cfg.Repo == "" {
		return nil, fmt.Errorf("actions: repo-commit-only requires config.repo (\"owner/repo\")")
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	token := cfg.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("actions: repo-commit-only requires a GitHub token (config.token or GITHUB_TOKEN env)")
	}

	apiBase := "https://api.github.com/repos/" + cfg.Repo
	var committed []string
	var links []string

	for _, art := range in.Artifacts {
		filePath := art.Path
		if filePath == "" {
			filePath = art.Name
		}
		if cfg.Path != "" {
			filePath = strings.TrimRight(cfg.Path, "/") + "/" + filePath
		}

		content := base64.StdEncoding.EncodeToString([]byte(art.Content))
		putPayload := map[string]string{
			"message": "add " + art.Name,
			"content": content,
			"branch":  cfg.Branch,
		}

		data, err := json.Marshal(putPayload)
		if err != nil {
			return nil, fmt.Errorf("actions: repo-commit marshal: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut,
			apiBase+"/contents/"+filePath, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("actions: repo-commit create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("actions: repo-commit PUT %s: %w", filePath, err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("actions: repo-commit PUT %s (%d): %s", filePath, resp.StatusCode, string(respBody))
		}

		var putResp struct {
			Content struct {
				HTMLURL string `json:"html_url"`
			} `json:"content"`
		}
		_ = json.Unmarshal(respBody, &putResp)
		committed = append(committed, filePath)
		if putResp.Content.HTMLURL != "" {
			links = append(links, putResp.Content.HTMLURL)
		}
	}

	meta := map[string]string{
		"repo":   cfg.Repo,
		"branch": cfg.Branch,
	}
	for _, art := range in.Artifacts {
		meta["artifact:"+art.Name] = art.Path
	}
	return &Output{
		Action:   ActionRepoCommit,
		Success:  true,
		Links:    links,
		Message:  fmt.Sprintf("Committed %d artifacts to %s@%s", len(committed), cfg.Repo, cfg.Branch),
		Metadata: meta,
	}, nil
}

// Global is the process-wide action registry, pre-loaded with built-in actions.
var Global = func() *Registry {
	r := NewRegistry()
	r.Register(&MarkdownExportAction{})
	r.Register(&ArtifactBundleAction{})
	r.Register(&BlogDraftAction{})
	r.Register(&WebhookAction{})
	r.Register(&GitHubPRAction{})
	r.Register(&RepoCommitAction{})
	return r
}()
