// Package builtin_attachment provides attachment retrieval tools for personas.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
)

// AttachmentStore is the subset of storage required by attachment tools.
type AttachmentStore interface {
	ListAttachmentsByWorkflow(ctx context.Context, workflowID string) ([]*state.Attachment, error)
	GetAttachment(ctx context.Context, id string) (*state.Attachment, error)
	ListAttachmentChunks(ctx context.Context, attachmentID string) ([]state.AttachmentChunk, error)
	GetAttachmentChunk(ctx context.Context, attachmentID string, index int) (*state.AttachmentChunk, error)
}

// RegisterAttachmentTools registers the attachment retrieval tools.
func RegisterAttachmentTools(reg *tools.Registry, store AttachmentStore) {
	reg.Register(&ListInputDocumentsTool{store: store})
	reg.Register(&GetInputDocumentSummaryTool{store: store})
	reg.Register(&ReadInputDocumentTool{store: store})
	reg.Register(&ReadInputChunkTool{store: store})
}

// ─── list_input_documents ─────────────────────────────────────────────────────

type ListInputDocumentsTool struct{ store AttachmentStore }

var _ tools.Tool = (*ListInputDocumentsTool)(nil)

func (t *ListInputDocumentsTool) Name() string { return "list_input_documents" }
func (t *ListInputDocumentsTool) Description() string {
	return "Lists all input documents attached to a workflow, returning their IDs, filenames, sizes, and summaries."
}
func (t *ListInputDocumentsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "workflow_id": { "type": "string", "description": "The workflow ID to list documents for." }
  },
  "required": ["workflow_id"]
}`)
}
func (t *ListInputDocumentsTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var p struct{ WorkflowID string `json:"workflow_id"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	atts, err := t.store.ListAttachmentsByWorkflow(ctx, p.WorkflowID)
	if err != nil {
		return nil, err
	}
	type doc struct {
		ID         string `json:"id"`
		Filename   string `json:"filename"`
		SizeBytes  int64  `json:"size_bytes"`
		ChunkCount int    `json:"chunk_count"`
		Summary    string `json:"summary"`
	}
	docs := make([]doc, 0, len(atts))
	for _, a := range atts {
		if a.Status == state.AttachmentCompleted {
			docs = append(docs, doc{
				ID:         a.ID,
				Filename:   a.Filename,
				SizeBytes:  a.SizeBytes,
				ChunkCount: a.ChunkCount,
				Summary:    a.Summary,
			})
		}
	}
	return json.Marshal(docs)
}

// ─── get_input_document_summary ───────────────────────────────────────────────

type GetInputDocumentSummaryTool struct{ store AttachmentStore }

var _ tools.Tool = (*GetInputDocumentSummaryTool)(nil)

func (t *GetInputDocumentSummaryTool) Name() string { return "get_input_document_summary" }
func (t *GetInputDocumentSummaryTool) Description() string {
	return "Returns the summary for a specific input document by attachment ID."
}
func (t *GetInputDocumentSummaryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "attachment_id": { "type": "string", "description": "The attachment ID." }
  },
  "required": ["attachment_id"]
}`)
}
func (t *GetInputDocumentSummaryTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var p struct{ AttachmentID string `json:"attachment_id"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	att, err := t.store.GetAttachment(ctx, p.AttachmentID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{
		"filename": att.Filename,
		"summary":  att.Summary,
	})
}

// ─── read_input_document ──────────────────────────────────────────────────────

type ReadInputDocumentTool struct{ store AttachmentStore }

var _ tools.Tool = (*ReadInputDocumentTool)(nil)

func (t *ReadInputDocumentTool) Name() string { return "read_input_document" }
func (t *ReadInputDocumentTool) Description() string {
	return "Returns the full content of an input document by concatenating all its chunks."
}
func (t *ReadInputDocumentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "attachment_id": { "type": "string", "description": "The attachment ID." }
  },
  "required": ["attachment_id"]
}`)
}
func (t *ReadInputDocumentTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var p struct{ AttachmentID string `json:"attachment_id"` }
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	chunks, err := t.store.ListAttachmentChunks(ctx, p.AttachmentID)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(c.Content)
	}
	return json.Marshal(map[string]string{"content": sb.String()})
}

// ─── read_input_chunk ─────────────────────────────────────────────────────────

type ReadInputChunkTool struct{ store AttachmentStore }

var _ tools.Tool = (*ReadInputChunkTool)(nil)

func (t *ReadInputChunkTool) Name() string { return "read_input_chunk" }
func (t *ReadInputChunkTool) Description() string {
	return "Returns a specific chunk of an input document by attachment ID and chunk index."
}
func (t *ReadInputChunkTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "attachment_id": { "type": "string", "description": "The attachment ID." },
    "index": { "type": "integer", "description": "The 0-based chunk index." }
  },
  "required": ["attachment_id", "index"]
}`)
}
func (t *ReadInputChunkTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var p struct {
		AttachmentID string `json:"attachment_id"`
		Index        int    `json:"index"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	chunk, err := t.store.GetAttachmentChunk(ctx, p.AttachmentID, p.Index)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"content": chunk.Content})
}
