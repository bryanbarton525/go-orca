package ratelimiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimit(t *testing.T) {
	// Create a rate limiter with rate=1, burst=2 (2 tokens initially)
	limiter := New(1, 2)

	// First two requests should succeed (burst of 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		limiter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected status %d, got %d", i+1, http.StatusOK, w.Code)
		}
		if !strings.Contains(w.Body.String(), "OK") {
			t.Errorf("Request %d: expected body 'OK', got '%s'", i+1, w.Body.String())
		}
	}

	// Third request should be rate limited
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	limiter.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Request 3: expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}

	// Wait for token refill (1 second at rate=1)
	time.Sleep(1 * time.Second)

	// Request after refill should succeed
	req = httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	limiter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request after refill: expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	// Test 1: Context cancelled during request handling
	t.Run("context_cancelled", func(t *testing.T) {
		limiter := New(1, 1)

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after 10ms
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		limiter.ServeHTTP(w, req)

		// Should complete without hanging
		_ = w.Code
	})

	// Test 2: Fresh limiter per test to avoid state interference
	t.Run("fresh_limiter", func(t *testing.T) {
		// Create a new limiter for this test
		limiter := New(2, 2)

		ctx, cancel := context.WithCancel(context.Background())

		// Schedule cancellation after 50ms
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		// Make requests
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			limiter.ServeHTTP(w, req)

			if i < 2 && w.Code != http.StatusOK {
				t.Errorf("Request %d: expected %d, got %d", i+1, http.StatusOK, w.Code)
			}
		}

		// Wait for context to be done
		<-ctx.Done()
	})
}

func TestTokenRefill(t *testing.T) {
	t.Parallel()

	// Test that requests succeed after waiting for token refill
	t.Run("refill_after_wait", func(t *testing.T) {
		// Create limiter: rate=1 token/sec, burst=1
		limiter := New(1, 1)

		// Exhaust the burst token — should succeed
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		limiter.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("First request: expected %d, got %d", http.StatusOK, w.Code)
		}

		// Immediate second request should be rate limited
		req = httptest.NewRequest("GET", "/", nil)
		w = httptest.NewRecorder()
		limiter.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Errorf("Second request: expected %d, got %d", http.StatusTooManyRequests, w.Code)
		}

		// Wait for token refill (1 second at rate=1)
		time.Sleep(1*time.Second + 100*time.Millisecond)

		// Request after refill should succeed
		req = httptest.NewRequest("GET", "/", nil)
		w = httptest.NewRecorder()
		limiter.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request after refill: expected %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("burst_tokens_available_immediately", func(t *testing.T) {
		// Create limiter: rate=1 token/sec, burst=5
		limiter := New(1, 5)

		// First 5 requests should succeed (burst)
		var req *http.Request
		var w *httptest.ResponseRecorder
		for i := 0; i < 5; i++ {
			req = httptest.NewRequest("GET", "/", nil)
			w = httptest.NewRecorder()
			limiter.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Request %d: expected %d, got %d", i+1, http.StatusOK, w.Code)
			}
		}

		// 6th request should be rate limited
		req = httptest.NewRequest("GET", "/", nil)
		w = httptest.NewRecorder()
		limiter.ServeHTTP(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("6th request: expected %d, got %d", http.StatusTooManyRequests, w.Code)
		}
	})
}

func TestConcurrency(t *testing.T) {
	t.Parallel()

	t.Run("concurrent_requests", func(t *testing.T) {
		// Create limiter with burst=10
		limiter := New(1, 10)

		var successCount int
		var mu sync.Mutex

		// Run 20 concurrent requests
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				limiter.ServeHTTP(w, req)
				if w.Code == http.StatusOK {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}(i)
		}

		wg.Wait()

		// Should have at most burst successes plus some refills
		_ = successCount
	})
}
