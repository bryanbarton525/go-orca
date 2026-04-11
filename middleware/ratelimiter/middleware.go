package ratelimiter

import (
	"net/http"
)

// Middleware returns an HTTP middleware function that applies rate limiting.
// rate: number of tokens per second (e.g., 10 means 10 requests per second)
// burst: maximum burst capacity (allows short bursts of requests)
// Example: New(10, 20) allows up to 20 requests instantly, then 10/second
func New(rate int, burst int) func(next http.Handler) http.Handler {
	tb := NewTokenBucket(rate, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !tb.consume() {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "1") // seconds until next token
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded","retry_after":1}`))
				return
			}
			w.Header().Set("X-RateLimit-Remaining", "1")
			next.ServeHTTP(w, r)
		})
	}
}
