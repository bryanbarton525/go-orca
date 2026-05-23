package streaming

import (
	"sync"

	"github.com/go-orca/go-orca/internal/events"
)

// Hub fans out workflow journal events to live SSE subscribers.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan *events.Event]struct{}
}

// NewHub constructs an in-memory workflow event broadcaster.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan *events.Event]struct{}),
	}
}

// Publish delivers an event to all subscribers for the workflow ID.
func (h *Hub) Publish(evt *events.Event) {
	if h == nil || evt == nil || evt.WorkflowID == "" {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	subs := h.subscribers[evt.WorkflowID]
	for ch := range subs {
		select {
		case ch <- evt:
		default:
			// Drop when a client is slow; UI still refreshes journal queries.
		}
	}
}

// Subscribe returns a channel of live workflow events for a workflow ID.
func (h *Hub) Subscribe(workflowID string) <-chan *events.Event {
	ch := make(chan *events.Event, 64)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscribers[workflowID] == nil {
		h.subscribers[workflowID] = make(map[chan *events.Event]struct{})
	}
	h.subscribers[workflowID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel.
func (h *Hub) Unsubscribe(workflowID string, ch <-chan *events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.subscribers[workflowID]
	if !ok {
		return
	}

	for registered := range subs {
		if registered == ch {
			delete(subs, registered)
			close(registered)
			break
		}
	}
	if len(subs) == 0 {
		delete(h.subscribers, workflowID)
	}
}

// SubscriberCount returns active subscribers for observability/tests.
func (h *Hub) SubscriberCount(workflowID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[workflowID])
}
