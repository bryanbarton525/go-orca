package streaming

import (
	"context"
	"fmt"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/config"
)

// Consumer reads workflow journal envelopes from Redpanda and publishes to a Hub.
type Consumer struct {
	client *kgo.Client
	hub    *Hub
	log    *zap.Logger
}

// NewConsumer creates a consumer group member for workflow event fan-out.
func NewConsumer(cfg config.StreamingConfig, hub *Hub, log *zap.Logger) (*Consumer, error) {
	if hub == nil {
		return nil, fmt.Errorf("streaming hub is required")
	}
	group := strings.TrimSpace(cfg.ConsumerGroup)
	if group == "" {
		group = "go-orca-workflow-stream"
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID + "-consumer"),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("streaming: create kafka consumer: %w", err)
	}

	return &Consumer{
		client: client,
		hub:    hub,
		log:    log,
	}, nil
}

// Run polls Redpanda until the context is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	if c == nil || c.client == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fetches := c.client.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Warn("streaming consumer poll failed", zap.Error(err))
			continue
		}

		fetches.EachRecord(func(r *kgo.Record) {
			evt, err := ParseWorkflowEvent(r.Value)
			if err != nil {
				return
			}
			c.hub.Publish(evt)
		})
	}
}

// Close shuts down the underlying franz-go client.
func (c *Consumer) Close() {
	if c == nil || c.client == nil {
		return
	}
	c.client.Close()
}
