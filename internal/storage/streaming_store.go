package storage

import (
	"context"

	"github.com/go-orca/go-orca/internal/events"
)

// WorkflowEventPublisher publishes workflow journal events to a streaming backend.
type WorkflowEventPublisher interface {
	PublishWorkflowEvent(evt *events.Event) error
}

// WithWorkflowEventPublisher wraps a Store and mirrors AppendEvents to Redpanda.
func WithWorkflowEventPublisher(store Store, publisher WorkflowEventPublisher) Store {
	if store == nil || publisher == nil {
		return store
	}
	return &streamingStore{
		Store:     store,
		publisher: publisher,
	}
}

type streamingStore struct {
	Store
	publisher WorkflowEventPublisher
}

func (s *streamingStore) AppendEvents(ctx context.Context, evts ...*events.Event) error {
	if err := s.Store.AppendEvents(ctx, evts...); err != nil {
		return err
	}
	for _, evt := range evts {
		if evt == nil {
			continue
		}
		_ = s.publisher.PublishWorkflowEvent(evt)
	}
	return nil
}
