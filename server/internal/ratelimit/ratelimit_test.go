package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTokenBucketAllow(t *testing.T) {
	tier := LimitTier{Requests: 3, Window: time.Minute}
	bucket := newTokenBucket(tier)

	// Should allow first 3 requests
	for i := 0; i < 3; i++ {
		allowed, _ := bucket.allow()
		if !allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	allowed, retryAfter := bucket.allow()
	if allowed {
		t.Error("4th request should be denied")
	}
	if retryAfter < 1 {
		t.Error("retryAfter should be at least 1 second")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	// Use a short window for testing
	tier := LimitTier{Requests: 1, Window: 100 * time.Millisecond}
	bucket := newTokenBucket(tier)

	// Use the single token
	allowed, _ := bucket.allow()
	if !allowed {
		t.Error("First request should be allowed")
	}

	// Should be denied immediately
	allowed, _ = bucket.allow()
	if allowed {
		t.Error("Second request should be denied immediately")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	allowed, _ = bucket.allow()
	if !allowed {
		t.Error("Request after refill should be allowed")
	}
}

func TestRateLimiterAllow(t *testing.T) {
	rl := New()
	defer rl.Stop()

	tier := LimitTier{Requests: 2, Window: time.Minute}

	// First two requests from same IP should be allowed
	allowed1, _ := rl.Allow("test", "192.168.1.1", tier)
	allowed2, _ := rl.Allow("test", "192.168.1.1", tier)
	allowed3, retryAfter := rl.Allow("test", "192.168.1.1", tier)

	if !allowed1 || !allowed2 {
		t.Error("First two requests should be allowed")
	}
	if allowed3 {
		t.Error("Third request should be denied")
	}
	if retryAfter < 1 {
		t.Error("retryAfter should be at least 1 second")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := New()
	defer rl.Stop()

	tier := LimitTier{Requests: 1, Window: time.Minute}

	// Different IPs should have separate limits
	allowed1, _ := rl.Allow("test", "192.168.1.1", tier)
	allowed2, _ := rl.Allow("test", "192.168.1.2", tier)

	if !allowed1 || !allowed2 {
		t.Error("Different IPs should have separate limits")
	}

	// Same IP should be limited
	allowed3, _ := rl.Allow("test", "192.168.1.1", tier)
	if allowed3 {
		t.Error("Same IP should be limited after exceeding quota")
	}
}

func TestRateLimiterDifferentTiers(t *testing.T) {
	rl := New()
	defer rl.Stop()

	authTier := LimitTier{Requests: 1, Window: time.Minute}
	apiTier := LimitTier{Requests: 1, Window: time.Minute}

	// Same IP, different tiers should have separate limits
	allowed1, _ := rl.Allow("auth", "192.168.1.1", authTier)
	allowed2, _ := rl.Allow("api", "192.168.1.1", apiTier)

	if !allowed1 || !allowed2 {
		t.Error("Different tiers should have separate limits")
	}
}

func TestMiddleware429Response(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rl := New()
	defer rl.Stop()

	tier := LimitTier{Requests: 1, Window: time.Minute}

	router := gin.New()
	router.Use(rl.Middleware("test", tier))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// First request should succeed
	req1, _ := http.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Forwarded-For", "192.168.1.100")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should return 200, got %d", w1.Code)
	}

	// Second request should be rate limited
	req2, _ := http.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Forwarded-For", "192.168.1.100")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should return 429, got %d", w2.Code)
	}

	// Check Retry-After header
	retryAfter := w2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header should be set")
	}

	// Check rate limit headers
	if w2.Header().Get("X-RateLimit-Limit") != "1" {
		t.Error("X-RateLimit-Limit header should be set to 1")
	}
	if w2.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Error("X-RateLimit-Remaining header should be 0")
	}
}

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rl := New()
	defer rl.Stop()

	router := gin.New()
	router.Use(rl.AuthMiddleware())
	router.POST("/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "logged in"})
	})

	// Auth limit is 5 per minute - first 5 should succeed
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("POST", "/login", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d should return 200, got %d", i+1, w.Code)
		}
	}

	// 6th request should be rate limited
	req, _ := http.NewRequest("POST", "/login", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th request should return 429, got %d", w.Code)
	}
}

func TestCleanup(t *testing.T) {
	rl := &RateLimiter{
		cleanupInterval: 10 * time.Millisecond,
		stopCleanup:     make(chan struct{}),
	}

	tier := LimitTier{Requests: 1, Window: time.Minute}

	// Add a bucket
	rl.Allow("test", "192.168.1.1", tier)

	// Verify bucket exists
	_, exists := rl.buckets.Load("test:192.168.1.1")
	if !exists {
		t.Error("Bucket should exist")
	}

	// Manually set lastRefill to old time
	bucket, _ := rl.buckets.Load("test:192.168.1.1")
	tb := bucket.(*tokenBucket)
	tb.mu.Lock()
	tb.lastRefill = time.Now().Add(-15 * time.Minute)
	tb.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	// Verify bucket was removed
	_, exists = rl.buckets.Load("test:192.168.1.1")
	if exists {
		t.Error("Old bucket should be cleaned up")
	}
}
