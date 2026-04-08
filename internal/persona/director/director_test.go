package director

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestOutputFromRawFallbackPreservesExplicitMode(t *testing.T) {
	out := OutputFromRaw("not-json", state.HandoffPacket{
		Mode:    state.WorkflowModeContent,
		Request: "Write a blog post about Go generics",
	})

	if out.Mode != state.WorkflowModeContent {
		t.Fatalf("Mode: got %q, want %q", out.Mode, state.WorkflowModeContent)
	}
	if out.Title == "" {
		t.Fatal("expected fallback title to be populated from request")
	}
}
