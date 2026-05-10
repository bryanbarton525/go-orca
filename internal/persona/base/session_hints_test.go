package base

import (
	"testing"

	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

func TestMergeProviderChatMetadata(t *testing.T) {
	pkt := state.HandoffPacket{
		WorkflowID:           "wf-1",
		CursorCloudAgentID:   "ignored-in-favor-of-arg",
		Workspace:            &state.WorkspaceInfo{RepoURL: "https://github.com/org/repo"},
	}
	req := mergeProviderChatMetadata(pkt, "agent-live", common.ChatRequest{Model: "m"})
	if req.Metadata["workflow_id"] != "wf-1" {
		t.Fatalf("workflow_id: got %q", req.Metadata["workflow_id"])
	}
	if req.Metadata["cursor_agent_id"] != "agent-live" {
		t.Fatalf("cursor_agent_id: got %q", req.Metadata["cursor_agent_id"])
	}
	if req.Metadata["repo_url"] != "https://github.com/org/repo" {
		t.Fatalf("repo_url: got %q", req.Metadata["repo_url"])
	}
}

func TestMergeProviderChatMetadataPreservesExistingKeys(t *testing.T) {
	pkt := state.HandoffPacket{WorkflowID: "wf-2"}
	req := mergeProviderChatMetadata(pkt, "", common.ChatRequest{
		Metadata: map[string]string{"custom": "v"},
	})
	if req.Metadata["custom"] != "v" {
		t.Fatalf("custom key lost")
	}
	if req.Metadata["workflow_id"] != "wf-2" {
		t.Fatalf("workflow_id: got %q", req.Metadata["workflow_id"])
	}
}

func TestApplyChatSessionHints(t *testing.T) {
	var got map[string]string
	pkt := state.HandoffPacket{
		OnProviderSessionUpdate: func(h map[string]string) {
			got = h
		},
	}
	resp := &common.ChatResponse{
		SessionHints: map[string]string{"cursor_agent_id": " bc-123 "},
	}
	id := applyChatSessionHints("", pkt, resp)
	if id != "bc-123" {
		t.Fatalf("returned id: got %q", id)
	}
	if got["cursor_agent_id"] != " bc-123 " {
		t.Fatalf("callback hints: %#v", got)
	}
}
