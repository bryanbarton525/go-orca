// Package actions defines the pluggable Finalizer delivery action interface
// and a registry of built-in actions.
//
// Supported delivery actions:
//   - api-response:     return all artifacts inline in the workflow state (no config required)
//   - github-pr:        open a GitHub pull request
//   - repo-commit-only: commit artifacts without opening a PR
//   - artifact-bundle:  package artifacts into an archive
//   - markdown-export:  render a single markdown document
//   - blog-draft:       produce a blog post draft artifact
//   - doc-draft:        produce a final polished document draft
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
	"strings"
	"sync"

	"github.com/go-orca/go-orca/internal/state"
)

// ActionKind is the canonical delivery action identifier.
type ActionKind string

const (
	ActionAPIResponse    ActionKind = "api-response"
	ActionGitHubPR       ActionKind = "github-pr"
	ActionRepoCommit     ActionKind = "repo-commit-only"
	ActionCreateRepo     ActionKind = "create-repo"
	ActionArtifactBundle ActionKind = "artifact-bundle"
	ActionMarkdownExport ActionKind = "markdown-export"
	ActionBlogDraft      ActionKind = "blog-draft"
	ActionDocDraft       ActionKind = "doc-draft"
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

// APIResponseAction returns all workflow artifacts inline in the finalization
// metadata with no external config required.  This is the zero-config default
// for workflows that do not specify a delivery target — the caller reads the
// result directly from GET /api/v1/workflows/{id}.
type APIResponseAction struct{}

func (a *APIResponseAction) Kind() ActionKind { return ActionAPIResponse }
func (a *APIResponseAction) Description() string {
	return "Return all artifacts inline in the API response. No config required."
}

func (a *APIResponseAction) Execute(_ context.Context, in Input) (*Output, error) {
	type artifactSummary struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	arts := make([]artifactSummary, 0, len(in.Artifacts))
	for _, art := range in.Artifacts {
		arts = append(arts, artifactSummary{
			Name:    art.Name,
			Kind:    string(art.Kind),
			Content: art.Content,
		})
	}
	artsJSON, err := json.Marshal(arts)
	if err != nil {
		return nil, fmt.Errorf("actions: api-response marshal: %w", err)
	}
	return &Output{
		Action:  ActionAPIResponse,
		Success: true,
		Message: fmt.Sprintf("%d artifact(s) returned inline.", len(in.Artifacts)),
		Metadata: map[string]string{
			"artifacts": string(artsJSON),
		},
	}, nil
}

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

// BlogDraftAction extracts the latest final-content artifact as the draft.
//
// Selection rule (newest-to-oldest):
//  1. Latest artifact with kind blog_post — preferred; produced when the
//     Implementer is mode-aware and may have been updated via remediation.
//  2. Latest textual artifact with kind markdown, document, or a plain-text
//     alias — fallback for workflows where the Implementer produced a generic
//     text artifact instead of explicitly tagging it as blog_post.
//
// Scanning newest-to-oldest ensures that a later remediation or synthesis
// task wins over an earlier draft of the same kind.
type BlogDraftAction struct{}

func (a *BlogDraftAction) Kind() ActionKind    { return ActionBlogDraft }
func (a *BlogDraftAction) Description() string { return "Produce a publication-ready blog post draft." }

func (a *BlogDraftAction) Execute(_ context.Context, in Input) (*Output, error) {
	// Scan newest-to-oldest (reverse append order) so a later remediation or
	// final-synthesis task wins over an earlier intermediate draft.
	for i := len(in.Artifacts) - 1; i >= 0; i-- {
		art := in.Artifacts[i]
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
	// Fall back to the latest textual artifact when no blog_post is present.
	// This handles content-mode workflows where the Implementer produced a
	// generic text artifact instead of explicitly tagging it as blog_post.
	for i := len(in.Artifacts) - 1; i >= 0; i-- {
		art := in.Artifacts[i]
		if isBlogDraftFallbackKind(art.Kind) {
			return &Output{
				Action:  ActionBlogDraft,
				Success: true,
				Message: fmt.Sprintf("Blog draft (fallback): %s", art.Name),
				Metadata: map[string]string{
					"draft":         art.Content,
					"fallback":      "true",
					"fallback_kind": string(art.Kind),
				},
			}, nil
		}
	}
	return &Output{
		Action:  ActionBlogDraft,
		Success: false,
		Error:   "no blog_post or textual draft artifact found",
	}, nil
}

func isBlogDraftFallbackKind(kind state.ArtifactKind) bool {
	if kind == state.ArtifactKindMarkdown || kind == state.ArtifactKindDocument {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(string(kind))) {
	case "plain_text", "plaintext", "text", "txt":
		return true
	default:
		return false
	}
}

// DocDraftAction extracts the final polished document from the workflow artifacts.
//
// Selection rule (newest-to-oldest):
//  1. Latest artifact with kind markdown — preferred for docs/research workflows.
//  2. Latest artifact with kind blog_post — fallback when only a blog_post artifact exists.
//
// Scanning newest-to-oldest ensures that a later remediation or synthesis
// task wins over an earlier draft of the same kind.
type DocDraftAction struct{}

func (a *DocDraftAction) Kind() ActionKind    { return ActionDocDraft }
func (a *DocDraftAction) Description() string { return "Produce a final polished document draft." }

func (a *DocDraftAction) Execute(_ context.Context, in Input) (*Output, error) {
	// Scan newest-to-oldest so a later remediation or synthesis task wins.
	for i := len(in.Artifacts) - 1; i >= 0; i-- {
		art := in.Artifacts[i]
		if art.Kind == state.ArtifactKindMarkdown {
			return &Output{
				Action:  ActionDocDraft,
				Success: true,
				Message: fmt.Sprintf("Doc draft: %s", art.Name),
				Metadata: map[string]string{
					"draft": art.Content,
				},
			}, nil
		}
	}
	// Fall back to the latest blog_post artifact when no markdown is present.
	for i := len(in.Artifacts) - 1; i >= 0; i-- {
		art := in.Artifacts[i]
		if art.Kind == state.ArtifactKindBlogPost {
			return &Output{
				Action:  ActionDocDraft,
				Success: true,
				Message: fmt.Sprintf("Doc draft (blog_post fallback): %s", art.Name),
				Metadata: map[string]string{
					"draft":    art.Content,
					"fallback": "true",
				},
			}, nil
		}
	}
	return &Output{
		Action:  ActionDocDraft,
		Success: false,
		Error:   "no markdown or blog_post artifact found",
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
//	token        – GitHub PAT; overrides the application-level default token
//
// Token resolution order:
//  1. config.token (per-workflow inline override)
//  2. defaultToken seeded from cfg.GitHub.Token via GOORCA_GITHUB_TOKEN
type GitHubPRAction struct {
	defaultToken string
}

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
		// Auto-generate a branch name from the workflow title + short ID so
		// callers that omit head_branch don't receive a hard failure.
		slug := strings.ToLower(in.Workflow.Title)
		slug = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return '-'
		}, slug)
		// Collapse consecutive dashes and trim.
		for strings.Contains(slug, "--") {
			slug = strings.ReplaceAll(slug, "--", "-")
		}
		slug = strings.Trim(slug, "-")
		if len(slug) > 40 {
			slug = slug[:40]
		}
		shortID := in.Workflow.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		if slug == "" {
			slug = "workflow"
		}
		cfg.HeadBranch = "workflow/" + slug + "-" + shortID
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if cfg.Title == "" {
		cfg.Title = in.Workflow.Title
	}
	if cfg.Title == "" {
		cfg.Title = fmt.Sprintf("improvement: %s", cfg.HeadBranch)
	}
	token := cfg.Token
	if token == "" {
		token = a.defaultToken
	}
	if token == "" {
		return nil, fmt.Errorf("actions: github-pr requires a GitHub token (config.token or GOORCA_GITHUB_TOKEN)")
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
	// Deduplicate by resolved file path — keep the last artifact for each path
	// (later tasks supersede earlier ones for the same file).
	type artifactEntry struct {
		name    string
		content string
	}
	// Preserve insertion order while deduplicating.
	pathOrder := make([]string, 0, len(in.Artifacts))
	pathMap := make(map[string]artifactEntry, len(in.Artifacts))
	for _, art := range in.Artifacts {
		fp := art.Path
		if fp == "" {
			fp = art.Name
		}
		if cfg.Path != "" {
			fp = strings.TrimRight(cfg.Path, "/") + "/" + fp
		}
		if _, exists := pathMap[fp]; !exists {
			pathOrder = append(pathOrder, fp)
		}
		pathMap[fp] = artifactEntry{name: art.Name, content: art.Content}
	}
	var committed []string
	for _, filePath := range pathOrder {
		art := pathMap[filePath]
		content := base64.StdEncoding.EncodeToString([]byte(art.content))
		putBody := map[string]interface{}{
			"message": "add " + art.name,
			"content": content,
			"branch":  cfg.HeadBranch,
		}
		// Check whether the file already exists on the head branch; if so, we
		// must supply its current SHA or the Contents API returns 422.
		existData, existStatus, err := ghDo(http.MethodGet,
			apiBase+"/contents/"+filePath+"?ref="+cfg.HeadBranch, nil)
		if err == nil && existStatus == http.StatusOK {
			var existResp struct {
				SHA string `json:"sha"`
			}
			if json.Unmarshal(existData, &existResp) == nil && existResp.SHA != "" {
				putBody["sha"] = existResp.SHA
				putBody["message"] = "update " + art.name
			}
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
//	token  – GitHub PAT; overrides the application-level default token
//
// Token resolution order:
//  1. config.token (per-workflow inline override)
//  2. defaultToken seeded from cfg.GitHub.Token via GOORCA_GITHUB_TOKEN
type RepoCommitAction struct {
	defaultToken string
}

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
		token = a.defaultToken
	}
	if token == "" {
		return nil, fmt.Errorf("actions: repo-commit-only requires a GitHub token (config.token or GOORCA_GITHUB_TOKEN)")
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

// CreateRepoAction creates a new GitHub repository and seeds it with all artifacts.
// Config fields:
//
//	name        – repository name (required)
//	org         – GitHub organisation to create under; when empty, creates under the
//	              authenticated user
//	description – repository description (optional)
//	private     – whether the repository should be private (default: false)
//	token       – GitHub PAT; overrides the application-level default token
//
// Token resolution order:
//  1. config.token (per-workflow inline override)
//  2. defaultToken seeded from cfg.GitHub.Token via GOORCA_GITHUB_TOKEN
type CreateRepoAction struct {
	defaultToken string
}

func (a *CreateRepoAction) Kind() ActionKind { return ActionCreateRepo }
func (a *CreateRepoAction) Description() string {
	return "Create a new GitHub repository and seed it with all artifacts."
}

func (a *CreateRepoAction) Execute(ctx context.Context, in Input) (*Output, error) {
	var cfg struct {
		Name        string `json:"name"`
		Org         string `json:"org"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
		Token       string `json:"token"`
	}
	if in.Config != nil {
		_ = json.Unmarshal(in.Config, &cfg)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("actions: create-repo requires config.name")
	}
	token := cfg.Token
	if token == "" {
		token = a.defaultToken
	}
	if token == "" {
		return nil, fmt.Errorf("actions: create-repo requires a GitHub token (config.token or GOORCA_GITHUB_TOKEN)")
	}
	if cfg.Description == "" && in.Workflow != nil && in.Workflow.Title != "" {
		cfg.Description = in.Workflow.Title
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

	// 1. Create the repository.
	createURL := "https://api.github.com/user/repos"
	createPayload := map[string]interface{}{
		"name":        cfg.Name,
		"description": cfg.Description,
		"private":     cfg.Private,
		"auto_init":   true,
	}
	if cfg.Org != "" {
		createURL = "https://api.github.com/orgs/" + cfg.Org + "/repos"
	}

	repoData, repoStatus, err := ghDo(http.MethodPost, createURL, createPayload)
	if err != nil {
		return nil, fmt.Errorf("actions: create-repo POST: %w", err)
	}
	if repoStatus != http.StatusCreated {
		return nil, fmt.Errorf("actions: create-repo POST %d: %s", repoStatus, string(repoData))
	}
	var repoResp struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	}
	_ = json.Unmarshal(repoData, &repoResp)

	// 2. Seed artifacts into the repo via the Contents API.
	apiBase := "https://api.github.com/repos/" + repoResp.FullName
	var committed []string
	for _, art := range in.Artifacts {
		filePath := art.Path
		if filePath == "" {
			filePath = art.Name
		}
		if filePath == "" {
			continue
		}
		content := base64.StdEncoding.EncodeToString([]byte(art.Content))
		putBody := map[string]interface{}{
			"message": "add " + art.Name,
			"content": content,
		}

		// If the file already exists (e.g. auto_init README.md), we must
		// include the existing file's SHA or the API returns 422.
		getResp, getStatus, _ := ghDo(http.MethodGet,
			apiBase+"/contents/"+filePath, nil)
		if getStatus == http.StatusOK {
			var existing struct {
				SHA string `json:"sha"`
			}
			if json.Unmarshal(getResp, &existing) == nil && existing.SHA != "" {
				putBody["sha"] = existing.SHA
			}
		}

		putData, putStatus, err := ghDo(http.MethodPut,
			apiBase+"/contents/"+filePath, putBody)
		if err != nil {
			return nil, fmt.Errorf("actions: create-repo PUT %s: %w", filePath, err)
		}
		if putStatus != http.StatusCreated && putStatus != http.StatusOK {
			return nil, fmt.Errorf("actions: create-repo PUT %s (%d): %s", filePath, putStatus, string(putData))
		}
		committed = append(committed, filePath)
	}

	return &Output{
		Action:  ActionCreateRepo,
		Success: true,
		Links:   []string{repoResp.HTMLURL},
		Message: fmt.Sprintf("Repository %s created with %d artifact(s) seeded", repoResp.FullName, len(committed)),
		Metadata: map[string]string{
			"repo_url":  repoResp.HTMLURL,
			"full_name": repoResp.FullName,
		},
	}, nil
}

// Global is the process-wide action registry, pre-loaded with built-in actions.
// Call InitGlobal with the application GitHub token before starting the server.
var Global = newGlobalRegistry("")

// InitGlobal rebuilds the process-wide registry with the supplied GitHub token
// as the default for the github-pr and repo-commit-only actions.  It must be
// called once at startup after config is loaded, before the first workflow runs.
func InitGlobal(githubToken string) {
	Global = newGlobalRegistry(githubToken)
}

func newGlobalRegistry(githubToken string) *Registry {
	r := NewRegistry()
	r.Register(&APIResponseAction{})
	r.Register(&MarkdownExportAction{})
	r.Register(&ArtifactBundleAction{})
	r.Register(&BlogDraftAction{})
	r.Register(&DocDraftAction{})
	r.Register(&WebhookAction{})
	r.Register(&GitHubPRAction{defaultToken: githubToken})
	r.Register(&RepoCommitAction{defaultToken: githubToken})
	r.Register(&CreateRepoAction{defaultToken: githubToken})
	return r
}
