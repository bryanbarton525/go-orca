package engine

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestSelectToolchain_NextJSNotConfusedByConfiguration(t *testing.T) {
	e := New(nil, Options{
		Toolchains: []ToolchainConfig{
			{ID: "go", Languages: []string{"go", "golang"}},
			{ID: "nextjs", Languages: []string{"nextjs", "next.js", "next"}},
		},
	})
	ws := state.NewWorkflowState("t", "s", "Using nextjs create a web service with a configuration page for rss feeds.")
	ws.Title = "RSS Newspaper Web Service"

	tc, lang, ok := e.selectToolchain(ws)
	if !ok {
		t.Fatal("expected toolchain match")
	}
	if tc.ID != "nextjs" {
		t.Fatalf("toolchain ID = %q, want nextjs (configuration must not match go)", tc.ID)
	}
	if lang != "nextjs" {
		t.Fatalf("language = %q, want nextjs", lang)
	}
}

func TestSelectToolchain_GoStillMatchesExplicitMention(t *testing.T) {
	e := New(nil, Options{
		Toolchains: []ToolchainConfig{
			{ID: "go", Languages: []string{"go", "golang"}},
			{ID: "nextjs", Languages: []string{"nextjs", "next.js", "next"}},
		},
	})
	ws := state.NewWorkflowState("t", "s", "Build a Go HTTP service with configuration endpoints.")

	tc, _, ok := e.selectToolchain(ws)
	if !ok || tc.ID != "go" {
		t.Fatalf("toolchain ID = %q, want go", tc.ID)
	}
}

func TestToolchainLanguageMentioned_WordBoundaries(t *testing.T) {
	hay := "using nextjs create a configuration page"
	if toolchainLanguageMentioned(hay, "go") {
		t.Fatal("go should not match inside configuration")
	}
	if !toolchainLanguageMentioned(hay, "nextjs") {
		t.Fatal("nextjs should match")
	}
}
