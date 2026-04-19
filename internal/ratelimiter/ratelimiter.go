package ratelimiter

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

var (
	ErrRateLimited = errors.New("rate limit exceeded")
	ErrInvalidRate = errors.New("invalid rate parameter")
)

// Middleware is an http.Handler that applies token-bucket rate limiting.
type Middleware interface {
	http.Handler
}

// New creates a rate-limiting middleware using token bucket algorithm.
func New(rate int, burst int) Middleware {
	if rate <= 0 {
		panic(ErrInvalidRate)
	}
	if burst <= 0 {
		burst = rate
	}

	return &RateLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		tokensMu:   sync.Mutex{},
		lastRefill: time.Now(),
	}
}

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	rate       int
	burst      int
	tokens     float64
	lastRefill time.Time
	tokensMu   sync.Mutex
}

// ServeHTTP implements http.Handler with rate limiting.
func (rl *RateLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Refill tokens at the start of request handling
	if err := rl.refill(r.Context()); err != nil {
		// Context cancelled during refill, don't block
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Check if tokens are available and consume atomically
	rl.tokensMu.Lock()
	available := rl.tokens >= 1.0
	if available {
		rl.tokens--
	}
	rl.tokensMu.Unlock()

	if available {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.Header().Set("Retry-After", rl.recalcWaitTime().String())
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("Rate limit exceeded"))
	}
}

// refill replenishes tokens based on elapsed time.
func (rl *RateLimiter) refill(ctx context.Context) error {
	now := time.Now()

	rl.tokensMu.Lock()
	defer rl.tokensMu.Unlock()

	elapsed := now.Sub(rl.lastRefill).Seconds()
	tokensToAdd := elapsed * float64(rl.rate)
	rl.tokens = min(float64(rl.burst), rl.tokens+tokensToAdd)
	rl.lastRefill = now

	// Check context cancellation during refill
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

// recalcWaitTime calculates how long to wait before tokens are available again.
func (rl *RateLimiter) recalcWaitTime() time.Duration {
	rl.tokensMu.Lock()
	needed := 1.0 - rl.tokens
	rl.tokensMu.Unlock()
	return time.Duration(float64(time.Second) * needed / float64(rl.rate))
}
