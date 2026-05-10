package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/provider/common"
)

func TestProvider_ModelsAndHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string][]string{"items": {"m1", "m2"}})
		case r.URL.Path == "/v1/me" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"apiKeyName": "t"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(config.CursorConfig{
		APIKey:   "k",
		BaseURL:  srv.URL,
		RepoURL:  "https://github.com/o/r",
		Timeout:  5 * time.Second,
		DefaultModel: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := p.HealthCheck(ctx); err != nil {
		t.Fatal(err)
	}
	models, err := p.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "m1" {
		t.Fatalf("models: %#v", models)
	}
}

func TestProvider_ModelsObjectItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"items":[{"id":"composer-2","name":"Composer"},{"id":"gpt-5"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p, err := New(config.CursorConfig{
		APIKey:  "k",
		BaseURL: srv.URL,
		RepoURL: "https://github.com/o/r",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "composer-2" || models[1].ID != "gpt-5" {
		t.Fatalf("models: %#v", models)
	}
}

func TestProvider_ChatUsesConfigStartingRefWhenMetadataOmitsIt(t *testing.T) {
	var gotBody createAgentBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/agents" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = w.Write([]byte(`{"agent":{"id":"bc-agent-1"},"run":{"id":"run-1"}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/agents/") && strings.HasSuffix(r.URL.Path, "/stream"):
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: assistant\ndata: {\"text\":\"Hello\"}\n\n")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			fmt.Fprintf(w, "event: result\ndata: {\"runId\":\"run-1\",\"status\":\"FINISHED\"}\n\n")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(config.CursorConfig{
		APIKey:       "secret",
		BaseURL:      srv.URL,
		RepoURL:      "https://github.com/o/r",
		StartingRef:  "main",
		Timeout:      10 * time.Second,
		DefaultModel: "composer-2",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Chat(context.Background(), common.ChatRequest{
		Model: "composer-2",
		Messages: []common.Message{
			{Role: common.RoleUser, Content: "Say hello"},
		},
		Metadata: map[string]string{
			metaRepoURL: "https://github.com/workspace/repo",
			// metadata omits starting_ref — must fall back to config StartingRef
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Message.Content, "Hello") {
		t.Fatalf("content: %q", resp.Message.Content)
	}
	if resp.SessionHints == nil || resp.SessionHints[SessionHintCursorAgentID] != "bc-agent-1" {
		t.Fatalf("session hints: %#v", resp.SessionHints)
	}
	if len(gotBody.Repos) != 1 || gotBody.Repos[0].URL != "https://github.com/workspace/repo" || gotBody.Repos[0].StartingRef != "main" {
		t.Fatalf("create agent body repos: %#v", gotBody.Repos)
	}
}

func TestProvider_ChatEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/agents" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"agent":{"id":"bc-agent-1"},"run":{"id":"run-1"}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/agents/") && strings.HasSuffix(r.URL.Path, "/stream"):
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: assistant\ndata: {\"text\":\"Hello\"}\n\n")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			fmt.Fprintf(w, "event: result\ndata: {\"runId\":\"run-1\",\"status\":\"FINISHED\"}\n\n")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(config.CursorConfig{
		APIKey:       "secret",
		BaseURL:      srv.URL,
		RepoURL:      "https://github.com/o/r",
		StartingRef:  "main",
		Timeout:      10 * time.Second,
		DefaultModel: "composer-2",
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Chat(context.Background(), common.ChatRequest{
		Model: "composer-2",
		Messages: []common.Message{
			{Role: common.RoleUser, Content: "Say hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Message.Content, "Hello") {
		t.Fatalf("content: %q", resp.Message.Content)
	}
	if resp.SessionHints == nil || resp.SessionHints[SessionHintCursorAgentID] != "bc-agent-1" {
		t.Fatalf("session hints: %#v", resp.SessionHints)
	}
}

func TestSerializeMessagesIncludesSchemaHint(t *testing.T) {
	s := serializeMessages([]common.Message{
		{Role: common.RoleSystem, Content: "sys"},
		{Role: common.RoleUser, Content: "hi"},
	}, true, map[string]any{"type": "object"}, "out")
	if !strings.Contains(s, "Target JSON schema name") {
		t.Fatalf("missing schema hint: %s", s)
	}
}
