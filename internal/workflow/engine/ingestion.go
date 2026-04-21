package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

// ensureAttachmentProcessing runs the pre-Director ingestion stage.
// It is idempotent: completed workflows are skipped, partial progress is
// resumed, and only unfinished attachments are processed.
func (e *Engine) ensureAttachmentProcessing(ctx context.Context, ws *state.WorkflowState) error {
	// Already completed — skip.
	if ws.AttachmentProcessing != nil && ws.AttachmentProcessing.Status == state.AttachmentProcessingCompleted {
		return nil
	}

	atts, err := e.opts.AttachmentStore.ListAttachmentsBySession(ctx, ws.UploadSessionID)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}
	if len(atts) == 0 {
		return nil // nothing to ingest
	}

	// Initialise processing state on first entry.
	now := time.Now().UTC()
	if ws.AttachmentProcessing == nil {
		ws.AttachmentProcessing = &state.AttachmentProcessing{
			Status:     state.AttachmentProcessingRunning,
			TotalCount: len(atts),
			StartedAt:  &now,
		}
	} else if ws.AttachmentProcessing.Status == state.AttachmentProcessingFailed {
		// Resume: reset to running.
		ws.AttachmentProcessing.Status = state.AttachmentProcessingRunning
		ws.AttachmentProcessing.ErrorMessage = ""
	}
	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save processing state: %w", err)
	}

	startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventAttachmentProcessingStarted, "",
		events.AttachmentProcessingStartedPayload{TotalCount: len(atts)})
	_ = e.store.AppendEvents(ctx, startEvt)

	// Resolve provider/model for ingestion.
	provName := e.opts.IngestionProvider
	if provName == "" {
		provName = e.opts.DefaultProvider
	}
	modelName := e.opts.IngestionModel
	if modelName == "" {
		modelName = e.opts.DefaultModel
	}
	prov, ok := common.Get(provName)
	if !ok {
		return e.failIngestion(ctx, ws, fmt.Errorf("ingestion provider %q not found", provName))
	}

	// Filter to pending/failed attachments only (resume-safe).
	var pending []*state.Attachment
	for _, a := range atts {
		if a.Status == state.AttachmentPending || a.Status == state.AttachmentFailed {
			pending = append(pending, a)
		}
	}

	// Process in parallel with bounded concurrency.
	sem := make(chan struct{}, e.opts.IngestionMaxWorkers)
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for _, att := range pending {
		wg.Add(1)
		go func(a *state.Attachment) {
			defer wg.Done()

			// Check for cancellation.
			if ctx.Err() != nil {
				return
			}

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := e.processAttachment(ctx, ws, a, prov, modelName); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("attachment %s (%s): %w", a.ID, a.Filename, err)
				}
				mu.Unlock()
			}
		}(att)
	}
	wg.Wait()

	if firstErr != nil {
		return e.failIngestion(ctx, ws, firstErr)
	}

	// Build the final workflow snapshot from all (including previously completed) attachments.
	return e.finalizeIngestion(ctx, ws, atts, prov, modelName)
}

// processAttachment reads a single file, chunks it, summarises it, and persists chunks.
func (e *Engine) processAttachment(ctx context.Context, ws *state.WorkflowState, att *state.Attachment, prov common.Provider, model string) error {
	_ = e.opts.AttachmentStore.UpdateAttachmentStatus(ctx, att.ID, state.AttachmentStatusRunning, "", 0, "")

	// Read file content.
	content, err := os.ReadFile(att.StoragePath)
	if err != nil {
		_ = e.opts.AttachmentStore.UpdateAttachmentStatus(ctx, att.ID, state.AttachmentFailed, "", 0, err.Error())
		return fmt.Errorf("read file: %w", err)
	}

	text := string(content)
	chunkSize := e.opts.IngestionChunkSize

	// Split into chunks.
	chunkTexts := splitIntoChunks(text, chunkSize)

	// Build and persist chunks.
	chunks := make([]state.AttachmentChunk, len(chunkTexts))
	for i, ct := range chunkTexts {
		chunks[i] = state.AttachmentChunk{
			ID:           uuid.New().String(),
			AttachmentID: att.ID,
			Index:        i,
			Content:      ct,
		}
	}
	if err := e.opts.AttachmentStore.CreateAttachmentChunks(ctx, chunks); err != nil {
		_ = e.opts.AttachmentStore.UpdateAttachmentStatus(ctx, att.ID, state.AttachmentFailed, "", 0, err.Error())
		return fmt.Errorf("persist chunks: %w", err)
	}

	// Summarise the document via LLM.
	summary, err := e.summariseDocument(ctx, prov, model, att.RelativePath, text)
	if err != nil {
		_ = e.opts.AttachmentStore.UpdateAttachmentStatus(ctx, att.ID, state.AttachmentFailed, "", len(chunks), err.Error())
		return fmt.Errorf("summarise: %w", err)
	}

	att.Summary = summary
	att.ChunkCount = len(chunks)
	att.Status = state.AttachmentCompleted
	_ = e.opts.AttachmentStore.UpdateAttachmentStatus(ctx, att.ID, state.AttachmentCompleted, summary, len(chunks), "")

	// Emit per-attachment event.
	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventAttachmentProcessed, "",
		events.AttachmentProcessedPayload{
			AttachmentID: att.ID,
			Filename:     att.Filename,
			ChunkCount:   len(chunks),
		})
	_ = e.store.AppendEvents(ctx, evt)

	return nil
}

// summariseDocument calls the LLM to produce a concise summary of a document.
func (e *Engine) summariseDocument(ctx context.Context, prov common.Provider, model, filename, content string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.opts.IngestionTimeout)
	defer cancel()

	// Truncate very large documents to avoid blowing token limits.
	maxChars := e.opts.IngestionChunkSize * 10
	if len(content) > maxChars {
		content = content[:maxChars] + "\n\n[truncated]"
	}

	resp, err := prov.Chat(ctx, common.ChatRequest{
		Model: model,
		Messages: []common.Message{
			{
				Role: common.RoleSystem,
				Content: "You are a document analyst. Produce a concise summary (3-5 sentences) of the provided document. " +
					"Focus on: what the document is, its purpose, key topics, and anything a software engineer would need to know to work with it. " +
					"Respond with only the summary text.",
			},
			{
				Role:    common.RoleUser,
				Content: fmt.Sprintf("File: %s\n\n```\n%s\n```", filename, content),
			},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message.Content), nil
}

// finalizeIngestion builds the workflow-level InputDocuments snapshot and corpus summary.
func (e *Engine) finalizeIngestion(ctx context.Context, ws *state.WorkflowState, atts []*state.Attachment, prov common.Provider, model string) error {
	// Reload all attachments to get updated summaries.
	fresh, err := e.opts.AttachmentStore.ListAttachmentsBySession(ctx, ws.UploadSessionID)
	if err != nil {
		return e.failIngestion(ctx, ws, fmt.Errorf("reload attachments: %w", err))
	}

	docs := make([]state.InputDocument, 0, len(fresh))
	var summaryParts []string
	for _, a := range fresh {
		if a.Status != state.AttachmentCompleted {
			return e.failIngestion(ctx, ws, fmt.Errorf("attachment %s still in status %s after processing", a.ID, a.Status))
		}
		docs = append(docs, state.InputDocument{
			AttachmentID: a.ID,
			Filename:     a.Filename,
			RelativePath: a.RelativePath,
			ContentType:  a.ContentType,
			SizeBytes:    a.SizeBytes,
			ChunkCount:   a.ChunkCount,
			Summary:      a.Summary,
		})
		if a.Summary != "" {
			displayPath := a.RelativePath
			if displayPath == "" {
				displayPath = a.Filename
			}
			summaryParts = append(summaryParts, fmt.Sprintf("- **%s**: %s", displayPath, a.Summary))
		}
	}

	// Build corpus summary via LLM if there are multiple documents.
	corpusSummary := ""
	if len(summaryParts) > 1 {
		var err error
		corpusSummary, err = e.buildCorpusSummary(ctx, prov, model, summaryParts)
		if err != nil {
			return e.failIngestion(ctx, ws, fmt.Errorf("corpus summary: %w", err))
		}
	} else if len(summaryParts) == 1 {
		corpusSummary = summaryParts[0]
	}

	now := time.Now().UTC()
	ws.InputDocuments = docs
	ws.InputDocumentCorpusSummary = corpusSummary
	ws.AttachmentProcessing.Status = state.AttachmentProcessingCompleted
	ws.AttachmentProcessing.DoneCount = len(fresh)
	ws.AttachmentProcessing.CompletedAt = &now

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save final ingestion state: %w", err)
	}

	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventAttachmentProcessingCompleted, "",
		events.AttachmentProcessingCompletedPayload{
			TotalCount: len(fresh),
		})
	_ = e.store.AppendEvents(ctx, evt)

	return nil
}

// buildCorpusSummary synthesises a single summary from per-document summaries.
func (e *Engine) buildCorpusSummary(ctx context.Context, prov common.Provider, model string, parts []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.opts.IngestionTimeout)
	defer cancel()

	resp, err := prov.Chat(ctx, common.ChatRequest{
		Model: model,
		Messages: []common.Message{
			{
				Role: common.RoleSystem,
				Content: "You are a document analyst. Given per-document summaries, produce a single cohesive corpus summary (3-7 sentences) " +
					"describing the overall collection of documents, their relationships, and what a software engineer would need to know. " +
					"Respond with only the summary text.",
			},
			{
				Role:    common.RoleUser,
				Content: "Document summaries:\n\n" + strings.Join(parts, "\n"),
			},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message.Content), nil
}

// failIngestion marks the workflow ingestion as failed and emits a failure event.
func (e *Engine) failIngestion(ctx context.Context, ws *state.WorkflowState, cause error) error {
	now := time.Now().UTC()
	if ws.AttachmentProcessing == nil {
		ws.AttachmentProcessing = &state.AttachmentProcessing{}
	}
	ws.AttachmentProcessing.Status = state.AttachmentProcessingFailed
	ws.AttachmentProcessing.ErrorMessage = cause.Error()
	ws.AttachmentProcessing.CompletedAt = &now
	_ = e.store.SaveWorkflow(ctx, ws)

	evt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventAttachmentProcessingFailed, "",
		events.AttachmentProcessingFailedPayload{Error: cause.Error()})
	_ = e.store.AppendEvents(ctx, evt)

	return cause
}

// splitIntoChunks splits text into chunks of at most maxChars characters,
// preferring to break at newline boundaries.
func splitIntoChunks(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxChars {
			chunks = append(chunks, text)
			break
		}
		// Try to break at a newline within the last 20% of the chunk.
		cutoff := maxChars
		searchStart := maxChars * 4 / 5
		if idx := strings.LastIndex(text[searchStart:cutoff], "\n"); idx >= 0 {
			cutoff = searchStart + idx + 1
		}
		chunks = append(chunks, text[:cutoff])
		text = text[cutoff:]
	}
	return chunks
}
