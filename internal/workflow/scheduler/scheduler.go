// Package scheduler manages concurrent workflow execution across multiple Engine
// instances.  It maintains a bounded worker pool, routes incoming workflow IDs
// to available workers, and supports graceful shutdown.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/workflow/engine"
)

// Options configures the Scheduler.
type Options struct {
	// Concurrency is the maximum number of workflows that may run in parallel.
	// Defaults to 4.
	Concurrency int

	// RetryDelay is the duration to wait before retrying a failed workflow.
	// Defaults to 5s.  Set to 0 to disable retries.
	RetryDelay time.Duration

	// MaxRetries is the maximum number of automatic retries for a failed
	// workflow.  Defaults to 0 (no retries).
	MaxRetries int
}

func (o *Options) applyDefaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 5 * time.Second
	}
}

// job is an internal work item.
type job struct {
	workflowID string
	attempt    int
}

// Scheduler dispatches workflow runs to a bounded pool of goroutines.
type Scheduler struct {
	eng    *engine.Engine
	opts   Options
	logger *zap.Logger

	queue  chan job
	wg     sync.WaitGroup
	cancel context.CancelFunc
	mu     sync.Mutex
	done   chan struct{}
}

// New creates and starts a Scheduler.
func New(eng *engine.Engine, opts Options, logger *zap.Logger) *Scheduler {
	opts.applyDefaults()
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		eng:    eng,
		opts:   opts,
		logger: logger,
		queue:  make(chan job, opts.Concurrency*4),
		cancel: cancel,
		done:   make(chan struct{}),
	}

	s.start(ctx)
	return s
}

// Enqueue adds a workflow to the run queue.  Returns an error if the scheduler
// is shutting down or the queue is full.
func (s *Scheduler) Enqueue(workflowID string) error {
	select {
	case <-s.done:
		return fmt.Errorf("scheduler: shutting down, cannot enqueue %s", workflowID)
	default:
	}

	select {
	case s.queue <- job{workflowID: workflowID, attempt: 0}:
		s.logger.Info("scheduler: enqueued", zap.String("workflow_id", workflowID))
		return nil
	default:
		return fmt.Errorf("scheduler: queue full, cannot enqueue %s", workflowID)
	}
}

// Shutdown drains the queue and waits for all running workflows to finish.
// The context deadline controls how long we wait.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.logger.Info("scheduler: shutting down")
	s.cancel()
	close(s.done)

	finished := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		s.logger.Info("scheduler: shutdown complete")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("scheduler: shutdown timed out: %w", ctx.Err())
	}
}

// ─── Internal worker pool ─────────────────────────────────────────────────────

func (s *Scheduler) start(ctx context.Context) {
	for i := 0; i < s.opts.Concurrency; i++ {
		s.wg.Add(1)
		go s.worker(ctx, i)
	}
}

func (s *Scheduler) worker(ctx context.Context, id int) {
	defer s.wg.Done()
	log := s.logger.With(zap.Int("worker", id))
	log.Debug("scheduler: worker started")

	for {
		select {
		case <-ctx.Done():
			log.Debug("scheduler: worker stopping (context cancelled)")
			return
		case j, ok := <-s.queue:
			if !ok {
				return
			}
			s.runJob(ctx, j, log)
		}
	}
}

func (s *Scheduler) runJob(ctx context.Context, j job, log *zap.Logger) {
	log = log.With(
		zap.String("workflow_id", j.workflowID),
		zap.Int("attempt", j.attempt),
	)
	log.Info("scheduler: running workflow")

	if err := s.eng.Run(ctx, j.workflowID); err != nil {
		// ErrPaused is not a failure; the workflow has been transitioned to
		// paused and must be explicitly resumed via the API.
		if err == engine.ErrPaused {
			log.Info("scheduler: workflow paused", zap.String("workflow_id", j.workflowID))
			return
		}
		log.Error("scheduler: workflow failed", zap.Error(err))

		if j.attempt < s.opts.MaxRetries {
			next := job{workflowID: j.workflowID, attempt: j.attempt + 1}
			go func() {
				if s.opts.RetryDelay > 0 {
					timer := time.NewTimer(s.opts.RetryDelay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-ctx.Done():
						return
					}
				}
				select {
				case s.queue <- next:
					log.Info("scheduler: re-enqueued for retry",
						zap.Int("next_attempt", next.attempt))
				case <-ctx.Done():
				}
			}()
		}
		return
	}

	log.Info("scheduler: workflow completed")
}
