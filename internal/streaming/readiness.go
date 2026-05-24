package streaming

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BrokerReadiness caches Redpanda reachability checks for /readyz.
type BrokerReadiness struct {
	producer *Producer
	interval time.Duration
	timeout  time.Duration

	lastReady atomic.Bool
	lastCheck atomic.Int64
	lastErr   atomic.Value

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewBrokerReadiness constructs a readiness cache.
func NewBrokerReadiness(producer *Producer, interval, timeout time.Duration) *BrokerReadiness {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	r := &BrokerReadiness{
		producer: producer,
		interval: interval,
		timeout:  timeout,
	}
	r.lastErr.Store("readiness probe has not completed yet")
	return r
}

// Start begins periodic broker pings.
func (r *BrokerReadiness) Start() {
	if r == nil || r.producer == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.runProbe()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.runProbe()
			}
		}
	}()
}

func (r *BrokerReadiness) runProbe() {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	err := r.producer.Ping(ctx)
	r.lastCheck.Store(time.Now().UnixNano())
	if err != nil {
		r.lastReady.Store(false)
		r.lastErr.Store(err.Error())
		return
	}

	r.lastReady.Store(true)
	r.lastErr.Store("")
}

// Ready returns a cached readiness status without pinging brokers.
func (r *BrokerReadiness) Ready(context.Context) error {
	if r == nil || r.producer == nil {
		return nil
	}

	last := r.lastCheck.Load()
	if last == 0 {
		return fmt.Errorf("streaming broker readiness pending")
	}
	if !r.lastReady.Load() {
		if msg, ok := r.lastErr.Load().(string); ok && msg != "" {
			return fmt.Errorf("streaming broker not ready: %s", msg)
		}
		return fmt.Errorf("streaming broker not ready")
	}
	if time.Since(time.Unix(0, last)) > r.interval*3 {
		return fmt.Errorf("streaming broker readiness stale")
	}
	return nil
}

// Stop stops background probing.
func (r *BrokerReadiness) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}
