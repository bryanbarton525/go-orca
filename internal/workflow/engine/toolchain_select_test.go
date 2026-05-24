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

func TestSelectToolchain_PrefersNextJSOverNodeForAppRouterRequest(t *testing.T) {
	e := New(nil, Options{
		Toolchains: []ToolchainConfig{
			{ID: "go", Languages: []string{"go", "golang"}},
			{ID: "nextjs", Languages: []string{"nextjs", "next.js", "next"}},
			{ID: "node", Languages: []string{"javascript", "typescript", "node"}},
		},
	})
	ws := state.NewWorkflowState("t", "s", "Build a Next.js 14 App Router app with TypeScript (no Go backend).")
	ws.Title = "RSS Newspaper"

	tc, lang, ok := e.selectToolchain(ws)
	if !ok {
		t.Fatal("expected toolchain match")
	}
	if tc.ID != "nextjs" {
		t.Fatalf("toolchain ID = %q, want nextjs (typescript must not steal node over next.js)", tc.ID)
	}
	if lang != "next.js" && lang != "nextjs" {
		t.Fatalf("language = %q, want next.js or nextjs", lang)
	}
}

func TestToolchainLanguageMentioned_SkipsNegatedGo(t *testing.T) {
	hay := "use nextjs only (no go backend)"
	if toolchainLanguageMentioned(hay, "go") {
		t.Fatal("go should not match after no")
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

func TestSelectToolchain_V2NewspaperRequest(t *testing.T) {
	e := New(nil, Options{
		Toolchains: []ToolchainConfig{
			{ID: "go", Languages: []string{"go", "golang"}},
			{ID: "nextjs", Languages: []string{"nextjs", "next.js", "next"}},
			{ID: "node", Languages: []string{"javascript", "typescript", "node"}},
		},
	})
	req := `Build a production-ready Next.js 14+ App Router web application (TypeScript, pnpm).
- Use Next.js App Router with TypeScript only (no Go backend unless explicitly required).
- Toolchain validation must use the Next.js MCP toolchain (install, build, test).`
	ws := state.NewWorkflowState("t", "s", req)
	ws.Title = "RSS Newspaper Web Service (v2)"
	tc, lang, ok := e.selectToolchain(ws)
	if !ok {
		t.Fatal("expected toolchain match")
	}
	t.Logf("selected %s lang=%s", tc.ID, lang)
	if tc.ID != "nextjs" {
		t.Fatalf("toolchain ID = %q, want nextjs", tc.ID)
	}
}
