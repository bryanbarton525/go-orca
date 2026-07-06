package sglang

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-orca/go-orca/internal/config"
)

func TestNewRequiresBaseURL(t *testing.T) {
	_, err := New(config.SGLangConfig{})
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestHealthEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://localhost:30000/v1":  "http://localhost:30000/health",
		"http://localhost:30000/v1/": "http://localhost:30000/health",
		"http://sglang.svc:8080":     "http://sglang.svc:8080/health",
	}
	for in, want := range cases {
		if got := healthEndpoint(in); got != want {
			t.Errorf("healthEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthCheckUsesNativeHealthEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := New(config.SGLangConfig{
		BaseURL: srv.URL + "/v1",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if gotPath != "/health" {
		t.Errorf("health check hit %q, want /health", gotPath)
	}
}

func TestHealthCheckFallsBackToModelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		// Native /health not exposed by this gateway.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p, err := New(config.SGLangConfig{
		BaseURL: srv.URL + "/v1",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck fallback: %v", err)
	}
}

func TestModelsAcceptsSGLangModelsShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":["Qwen/Qwen2.5-Coder-14B-Instruct-AWQ"]}`))
	}))
	defer srv.Close()

	p, err := New(config.SGLangConfig{
		BaseURL: srv.URL + "/v1",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "Qwen/Qwen2.5-Coder-14B-Instruct-AWQ" {
		t.Fatalf("Models = %#v", models)
	}
}

func TestModelsFallsBackToGetModelInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		case "/get_model_info":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"model_path":"Qwen/Qwen2.5-Coder-14B-Instruct-AWQ",
				"tokenizer_path":"Qwen/Qwen2.5-Coder-14B-Instruct-AWQ",
				"model_type":"qwen2",
				"architectures":["Qwen2ForCausalLM"]
			}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p, err := New(config.SGLangConfig{
		BaseURL: srv.URL + "/v1",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "Qwen/Qwen2.5-Coder-14B-Instruct-AWQ" {
		t.Fatalf("Models = %#v", models)
	}
	if got := models[0].Metadata["model_type"]; got != "qwen2" {
		t.Fatalf("model_type metadata = %q", got)
	}
}
