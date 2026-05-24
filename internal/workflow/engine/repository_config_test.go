package engine

import (
	"encoding/json"
	"testing"

	"github.com/go-orca/go-orca/internal/finalizer/actions"
	"github.com/go-orca/go-orca/internal/state"
)

func TestRepositoryConfigFromDelivery_GitHubPRWithOrgName(t *testing.T) {
	ws := state.NewWorkflowState("t", "s", "Build a Next.js app")
	ws.DeliveryAction = string(actions.ActionGitHubPR)
	ws.DeliveryConfig = json.RawMessage(`{"org":"bryanbarton525","name":"rss-newspaper","private":false}`)
	cfg, ok := repositoryConfigFromDelivery(ws)
	if !ok {
		t.Fatal("expected config")
	}
	if cfg.Name != "rss-newspaper" || cfg.Owner != "bryanbarton525" {
		t.Fatalf("cfg = %+v", cfg)
	}
	full := requestedRepositoryFullName(cfg)
	if full != "bryanbarton525/rss-newspaper" {
		t.Fatalf("full name = %q", full)
	}
}

func TestRepositoryConfigFromDelivery_GitHubPRWithRepoSlug(t *testing.T) {
	ws := state.NewWorkflowState("t", "s", "request")
	ws.DeliveryAction = string(actions.ActionGitHubPR)
	ws.DeliveryConfig = json.RawMessage(`{"repo":"acme/widget"}`)
	cfg, ok := repositoryConfigFromDelivery(ws)
	if !ok || cfg.Owner != "acme" || cfg.Name != "widget" {
		t.Fatalf("cfg = %+v ok=%v", cfg, ok)
	}
}

func TestParseGitHubRepoSlug(t *testing.T) {
	owner, name := parseGitHubRepoSlug("https://github.com/bryanbarton525/rss-newspaper.git")
	if owner != "bryanbarton525" || name != "rss-newspaper" {
		t.Fatalf("got %q/%q", owner, name)
	}
}

func TestRequestedRepositoryConfig_PrefersDeliveryOverRequestText(t *testing.T) {
	ws := state.NewWorkflowState("t", "s", "see https://github.com/other/repo for context")
	ws.DeliveryAction = string(actions.ActionGitHubPR)
	ws.DeliveryConfig = json.RawMessage(`{"org":"bryanbarton525","name":"rss-newspaper"}`)
	cfg, ok := requestedRepositoryConfig(ws)
	if !ok || cfg.Name != "rss-newspaper" {
		t.Fatalf("cfg = %+v ok=%v", cfg, ok)
	}
}
