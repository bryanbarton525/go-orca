package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
	"github.com/go-orca/go-orca/internal/streaming"
)

// StreamWorkflowEvents handles GET /workflows/:id/stream as SSE.
//
// When a Redpanda hub is configured, live events are delivered from the topic
// fan-out after an initial journal snapshot from the database. Otherwise the
// handler falls back to database polling.
func StreamWorkflowEvents(store storage.Store, hub *streaming.Hub) gin.HandlerFunc {
	const defaultTimeoutSec = 300

	return func(c *gin.Context) {
		id := c.Param("id")
		ws, ok := checkWorkflowOwnership(c, store)
		if !ok {
			return
		}

		timeoutSec := defaultTimeoutSec
		if raw := c.Query("timeout"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				timeoutSec = v
			}
		}

		terminal := workflowTerminal
		if terminal(ws.Status) {
			writeTerminalSnapshot(c, store, id)
			return
		}

		useRedpanda := hub != nil && streamSource(c.Query("source")) != "database"
		if useRedpanda {
			streamWorkflowEventsRedpanda(c, store, hub, id, timeoutSec, terminal)
			return
		}
		streamWorkflowEventsDatabase(c, store, id, timeoutSec, terminal)
	}
}

func streamSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "database", "db":
		return "database"
	case "redpanda", "kafka":
		return "redpanda"
	default:
		return "auto"
	}
}

func workflowTerminal(status state.WorkflowStatus) bool {
	return status == state.WorkflowStatusCompleted ||
		status == state.WorkflowStatusFailed ||
		status == state.WorkflowStatusCancelled
}

func writeTerminalSnapshot(c *gin.Context, store storage.Store, workflowID string) {
	evts, err := store.ListEvents(c.Request.Context(), workflowID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list events")
		return
	}
	initSSE(c, "database")
	for _, evt := range evts {
		writeSSEEvent(c, evt)
	}
	writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
}

func initSSE(c *gin.Context, transport string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if transport != "" {
		c.Header("X-Stream-Transport", transport)
	}

	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Time{})
}

func streamWorkflowEventsDatabase(
	c *gin.Context,
	store storage.Store,
	workflowID string,
	timeoutSec int,
	terminal func(state.WorkflowStatus) bool,
) {
	const pollInterval = time.Second

	initSSE(c, "database")

	ctx := c.Request.Context()
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastEventTime time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if now.After(deadline) {
				writeSSEData(c, `{"type":"stream.closed","reason":"timeout"}`)
				return
			}

			evts, err := store.ListEvents(ctx, workflowID)
			if err != nil {
				continue
			}

			newCount := 0
			for _, evt := range evts {
				if evt.OccurredAt.After(lastEventTime) {
					writeSSEEvent(c, evt)
					lastEventTime = evt.OccurredAt
					newCount++
				}
			}

			if newCount == 0 {
				writeSSEKeepalive(c)
			}

			current, err := store.GetWorkflow(ctx, workflowID)
			if err == nil && terminal(current.Status) {
				writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
				return
			}
		}
	}
}

func streamWorkflowEventsRedpanda(
	c *gin.Context,
	store storage.Store,
	hub *streaming.Hub,
	workflowID string,
	timeoutSec int,
	terminal func(state.WorkflowStatus) bool,
) {
	const keepaliveInterval = 15 * time.Second

	initSSE(c, "redpanda")

	ctx := c.Request.Context()
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()

	sent := make(map[string]struct{})
	var lastEventTime time.Time

	evts, err := store.ListEvents(ctx, workflowID)
	if err == nil {
		for _, evt := range evts {
			writeSSEEvent(c, evt)
			sent[evt.ID] = struct{}{}
			if evt.OccurredAt.After(lastEventTime) {
				lastEventTime = evt.OccurredAt
			}
		}
	}

	live := hub.Subscribe(workflowID)
	defer hub.Unsubscribe(workflowID, live)

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			writeSSEKeepalive(c)
			current, err := store.GetWorkflow(ctx, workflowID)
			if err == nil && terminal(current.Status) {
				writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
				return
			}
			if time.Now().After(deadline) {
				writeSSEData(c, `{"type":"stream.closed","reason":"timeout"}`)
				return
			}
		case evt, ok := <-live:
			if !ok {
				writeSSEData(c, `{"type":"stream.closed","reason":"hub_closed"}`)
				return
			}
			if evt == nil {
				continue
			}
			if _, exists := sent[evt.ID]; exists {
				continue
			}
			if !lastEventTime.IsZero() && !evt.OccurredAt.After(lastEventTime) {
				continue
			}
			writeSSEEvent(c, evt)
			sent[evt.ID] = struct{}{}
			lastEventTime = evt.OccurredAt

			if terminalEvent(evt) {
				writeSSEData(c, `{"type":"stream.closed","reason":"workflow_terminal"}`)
				return
			}
		}
	}
}

func terminalEvent(evt *events.Event) bool {
	if evt == nil {
		return false
	}
	switch evt.Type {
	case events.EventWorkflowCompleted,
		events.EventWorkflowFailed,
		events.EventWorkflowCancelled:
		return true
	default:
		return false
	}
}

func writeSSEKeepalive(c *gin.Context) {
	if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
		return
	}
	c.Writer.Flush()
}
