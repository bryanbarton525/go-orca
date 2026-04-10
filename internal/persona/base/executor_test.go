package base

import (
	"strings"
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

// ─── extractJSON ─────────────────────────────────────────────────────────────

func TestExtractJSON_NoFence(t *testing.T) {
	input := `{"key":"value"}`
	got := extractJSON(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractJSON_JsonFence(t *testing.T) {
	input := "```json\n{\"key\":\"value\"}\n```"
	got := extractJSON(input)
	want := `{"key":"value"}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_PlainFence(t *testing.T) {
	input := "```\n{\"key\":\"value\"}\n```"
	got := extractJSON(input)
	want := `{"key":"value"}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_LeadingTrailingWhitespace(t *testing.T) {
	input := "  \n  ```json\n  {\"key\":\"value\"}\n  ```  \n  "
	got := extractJSON(input)
	// After outer TrimSpace the fence is found; inner TrimSpace removes surrounding whitespace.
	want := `{"key":"value"}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_PrefixTextBeforeFence(t *testing.T) {
	input := "Here is the output:\n```json\n{\"a\":1}\n```"
	got := extractJSON(input)
	want := `{"a":1}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// ─── ParseJSON ────────────────────────────────────────────────────────────────

func TestParseJSON_CleanJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	raw := `{"name":"alice","age":30}`
	var p payload
	if err := ParseJSON(raw, &p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "alice" || p.Age != 30 {
		t.Errorf("unexpected values: %+v", p)
	}
}

func TestParseJSON_WithJsonFence(t *testing.T) {
	type payload struct {
		Mode string `json:"mode"`
	}
	raw := "```json\n{\"mode\":\"software\"}\n```"
	var p payload
	if err := ParseJSON(raw, &p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode != "software" {
		t.Errorf("expected mode %q, got %q", "software", p.Mode)
	}
}

func TestParseJSON_InvalidJSON_ReturnsError(t *testing.T) {
	var m map[string]interface{}
	if err := ParseJSON("not json at all", &m); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ─── BuildHandoffContext ──────────────────────────────────────────────────────

func TestBuildHandoffContext_BasicFields(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-abc",
		Mode:       state.WorkflowModeSoftware,
		Request:    "build a thing",
	}
	out := BuildHandoffContext(packet)

	for _, want := range []string{"wf-abc", "software", "build a thing"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

func TestBuildHandoffContext_IncludesSummaries(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-xyz",
		Mode:       state.WorkflowModeContent,
		Request:    "write a blog post",
		Summaries: map[state.PersonaKind]string{
			state.PersonaDirector:   "classified as content",
			state.PersonaProjectMgr: "requirements captured",
		},
	}
	out := BuildHandoffContext(packet)

	for _, want := range []string{"classified as content", "requirements captured"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

func TestBuildHandoffContext_IncludesTasks(t *testing.T) {
	packet := state.HandoffPacket{
		WorkflowID: "wf-tasks",
		Mode:       state.WorkflowModeSoftware,
		Request:    "implement feature",
		Tasks: []state.Task{
			{
				ID:     "12345678-0000-0000-0000-000000000000",
				Title:  "Write tests",
				Status: state.TaskStatusPending,
			},
		},
	}
	out := BuildHandoffContext(packet)

	if !strings.Contains(out, "Write tests") {
		t.Errorf("expected output to contain task title; got:\n%s", out)
	}
	if !strings.Contains(out, "pending") {
		t.Errorf("expected output to contain task status; got:\n%s", out)
	}
}

func TestBuildHandoffContext_EmptyPacket(t *testing.T) {
	// Should not panic with zero-value packet.
	packet := state.HandoffPacket{}
	out := BuildHandoffContext(packet)
	if !strings.Contains(out, "## Workflow") {
		t.Errorf("expected at least a Workflow header; got:\n%s", out)
	}
}

// ─── trimToolResult ───────────────────────────────────────────────────────────

func TestTrimToolResult_ShortContent_Unchanged(t *testing.T) {
	input := []byte(`{"result":"hello"}`)
	got := trimToolResult(input)
	if got != string(input) {
		t.Errorf("expected unchanged; got %q", got)
	}
}

func TestTrimToolResult_ExactLimit_Unchanged(t *testing.T) {
	input := []byte(strings.Repeat("x", maxToolResultBytes))
	got := trimToolResult(input)
	if got != string(input) {
		t.Errorf("expected unchanged at exact limit")
	}
}

func TestTrimToolResult_OverLimit_Truncated(t *testing.T) {
	input := []byte(strings.Repeat("a", maxToolResultBytes+500))
	got := trimToolResult(input)
	if len(got) <= maxToolResultBytes {
		// trimmed content plus notice should be longer than the raw cap
		t.Errorf("expected truncated output to include notice; got length %d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice in output; got %q", got[:200])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", maxToolResultBytes)) {
		t.Errorf("expected first %d bytes preserved", maxToolResultBytes)
	}
}
