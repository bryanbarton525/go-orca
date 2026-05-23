package streaming

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/events"
)

// Producer is an async Redpanda/Kafka producer for event ingestion.
type Producer struct {
	client *kgo.Client
	cfg    config.StreamingConfig
	log    *zap.Logger
	stats  *Metrics
}

// NewProducer creates a franz-go client and producer wrapper.
func NewProducer(cfg config.StreamingConfig, log *zap.Logger, stats *Metrics) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("streaming: brokers are required")
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return nil, fmt.Errorf("streaming: topic is required")
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.DefaultProduceTopic(cfg.Topic),
		kgo.RequiredAcks(parseRequiredAcks(cfg.RequiredAcks)),
	}

	if cfg.ProduceTimeout > 0 {
		opts = append(opts, kgo.RecordDeliveryTimeout(cfg.ProduceTimeout))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("streaming: create kafka client: %w", err)
	}

	return &Producer{
		client: client,
		cfg:    cfg,
		log:    log,
		stats:  stats,
	}, nil
}

// Produce enqueues a record asynchronously and returns immediately.
func (p *Producer) Produce(userID string, payload []byte) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("streaming producer unavailable")
	}
	key := strings.TrimSpace(userID)
	if key == "" {
		return fmt.Errorf("user id is required")
	}

	start := time.Now()
	record := &kgo.Record{
		Topic: p.cfg.Topic,
		Key:   []byte(key),
		Value: payload,
	}

	if p.stats != nil {
		p.stats.Enqueue()
	}

	produceCtx := context.Background()
	cancel := func() {}
	if p.cfg.ProduceTimeout > 0 {
		produceCtx, cancel = context.WithTimeout(context.Background(), p.cfg.ProduceTimeout)
	}

	// Produce is asynchronous and returns immediately after the record is
	// accepted into the local franz-go buffer. Broker acks are reported via the
	// callback and should not block HTTP request handling.
	p.client.Produce(produceCtx, record, func(r *kgo.Record, err error) {
		cancel()
		if p.stats != nil {
			p.stats.ObserveProduceResult(time.Since(start), err)
		}
		if err != nil {
			p.log.Warn("streaming produce failed",
				zap.String("user_id", key),
				zap.String("topic", p.cfg.Topic),
				zap.Error(err),
			)
			return
		}
		p.log.Debug("streaming produce acked",
			zap.String("user_id", key),
			zap.String("topic", r.Topic),
			zap.Int32("partition", r.Partition),
			zap.Int64("offset", r.Offset),
		)
	})

	return nil
}

// PublishWorkflowEvent enqueues a workflow journal event keyed by workflow ID.
func (p *Producer) PublishWorkflowEvent(evt *events.Event) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("streaming producer unavailable")
	}
	if evt == nil || strings.TrimSpace(evt.WorkflowID) == "" {
		return fmt.Errorf("workflow id is required")
	}

	payload, err := MarshalWorkflowEvent(evt)
	if err != nil {
		return err
	}

	start := time.Now()
	record := &kgo.Record{
		Topic: p.cfg.Topic,
		Key:   []byte(evt.WorkflowID),
		Value: payload,
	}

	if p.stats != nil {
		p.stats.Enqueue()
	}

	produceCtx := context.Background()
	cancel := func() {}
	if p.cfg.ProduceTimeout > 0 {
		produceCtx, cancel = context.WithTimeout(context.Background(), p.cfg.ProduceTimeout)
	}

	p.client.Produce(produceCtx, record, func(r *kgo.Record, err error) {
		cancel()
		if p.stats != nil {
			p.stats.ObserveProduceResult(time.Since(start), err)
		}
		if err != nil {
			p.log.Warn("workflow event publish failed",
				zap.String("workflow_id", evt.WorkflowID),
				zap.String("event_type", string(evt.Type)),
				zap.Error(err),
			)
			return
		}
		p.log.Debug("workflow event publish acked",
			zap.String("workflow_id", evt.WorkflowID),
			zap.String("event_type", string(evt.Type)),
			zap.Int32("partition", r.Partition),
			zap.Int64("offset", r.Offset),
		)
	})

	return nil
}

// Ping checks whether any broker is currently reachable.
func (p *Producer) Ping(ctx context.Context) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("streaming producer unavailable")
	}
	return p.client.Ping(ctx)
}

// Flush drains producer buffers before shutdown.
func (p *Producer) Flush(ctx context.Context) error {
	if p == nil || p.client == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.client.Flush(ctx)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// Close closes the underlying kafka client.
func (p *Producer) Close() {
	if p == nil || p.client == nil {
		return
	}
	p.client.Close()
}

// Topic returns the configured destination topic.
func (p *Producer) Topic() string {
	if p == nil {
		return ""
	}
	return p.cfg.Topic
}

func parseRequiredAcks(raw string) kgo.Acks {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none", "0":
		return kgo.NoAck()
	case "1", "leader":
		return kgo.LeaderAck()
	default:
		return kgo.AllISRAcks()
	}
}
