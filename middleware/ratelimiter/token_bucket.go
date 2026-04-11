package ratelimiter

import (
	"sync"
	"time"
)

// TokenBucket represents a token bucket for rate limiting.
type TokenBucket struct {
	tokens      float64
	maxTokens   float64
	refillRate  float64 // tokens per second
	lastRefill  time.Time
	mu          sync.Mutex
}

// NewTokenBucket creates a new token bucket rate limiter.
// rate: tokens added per second
// burst: maximum bucket capacity
func NewTokenBucket(rate int, burst int) *TokenBucket {
	tb := &TokenBucket{
		tokens:      float64(burst),
		maxTokens:   float64(burst),
		refillRate:  float64(rate),
		lastRefill:  time.Now(),
	}
	return tb
}

// consume attempts to take tokens from the bucket.
// Returns true if tokens were consumed, false if the request should be rejected.
func (tb *TokenBucket) consume() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = tb.tokens + tb.refillRate*elapsed
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens -= 1
		return true
	}
	return false
}
