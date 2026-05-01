package ratelimit

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LimitTier defines the rate limit configuration for different endpoint types
type LimitTier struct {
	// Requests is the maximum number of requests allowed in the window
	Requests int
	// Window is the time window for rate limiting
	Window time.Duration
}

// Default limit tiers
var (
	// AuthLimit: 5 requests per minute for login/register endpoints
	AuthLimit = LimitTier{Requests: 5, Window: time.Minute}
	// APILimit: 60 requests per minute for general API endpoints
	APILimit = LimitTier{Requests: 60, Window: time.Minute}
	// WebSocketLimit: 10 connections per minute for WebSocket endpoints
	WebSocketLimit = LimitTier{Requests: 10, Window: time.Minute}
)

// tokenBucket implements a token bucket rate limiter for a single client
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// newTokenBucket creates a new token bucket with the given limit tier
func newTokenBucket(tier LimitTier) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(tier.Requests),
		maxTokens:  float64(tier.Requests),
		refillRate: float64(tier.Requests) / tier.Window.Seconds(),
		lastRefill: time.Now(),
	}
}

// allow checks if a request is allowed and consumes a token if so
// Returns (allowed, retryAfter) where retryAfter is seconds until next token
func (tb *tokenBucket) allow() (bool, int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true, 0
	}

	// Calculate time until next token
	retryAfter := int((1 - tb.tokens) / tb.refillRate)
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

// RateLimiter manages rate limiting for multiple clients using token buckets
type RateLimiter struct {
	// buckets stores token buckets per IP address per tier
	// Key format: "tier:ip" (e.g., "auth:192.168.1.1")
	buckets sync.Map

	// cleanupInterval is how often to run garbage collection
	cleanupInterval time.Duration

	// stopCleanup channel to stop the cleanup goroutine
	stopCleanup chan struct{}
}

// New creates a new RateLimiter and starts the cleanup goroutine
func New() *RateLimiter {
	rl := &RateLimiter{
		cleanupInterval: 5 * time.Minute,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// cleanupLoop periodically removes stale token buckets
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// cleanup removes token buckets that haven't been used recently
func (rl *RateLimiter) cleanup() {
	threshold := time.Now().Add(-10 * time.Minute)

	rl.buckets.Range(func(key, value interface{}) bool {
		bucket := value.(*tokenBucket)
		bucket.mu.Lock()
		lastUsed := bucket.lastRefill
		bucket.mu.Unlock()

		if lastUsed.Before(threshold) {
			rl.buckets.Delete(key)
		}
		return true
	})
}

// getBucket gets or creates a token bucket for the given key and tier
func (rl *RateLimiter) getBucket(key string, tier LimitTier) *tokenBucket {
	if bucket, ok := rl.buckets.Load(key); ok {
		return bucket.(*tokenBucket)
	}

	// Create new bucket
	bucket := newTokenBucket(tier)
	actual, _ := rl.buckets.LoadOrStore(key, bucket)
	return actual.(*tokenBucket)
}

// Allow checks if a request from the given IP should be allowed for the given tier
// Returns (allowed, retryAfter) where retryAfter is seconds until retry is allowed
func (rl *RateLimiter) Allow(tierName, ip string, tier LimitTier) (bool, int) {
	key := tierName + ":" + ip
	bucket := rl.getBucket(key, tier)
	return bucket.allow()
}

// Middleware creates a Gin middleware for the given tier
func (rl *RateLimiter) Middleware(tierName string, tier LimitTier) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		allowed, retryAfter := rl.Allow(tierName, ip, tier)
		if !allowed {
			log.Printf("[RATE_LIMIT] %s tier exceeded for IP %s, retry after %d seconds",
				tierName, ip, retryAfter)

			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.Header("X-RateLimit-Limit", strconv.Itoa(tier.Requests))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(retryAfter)*time.Second).Unix(), 10))

			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": retryAfter,
				"message":     "Too many requests. Please wait before trying again.",
			})
			return
		}

		// Add rate limit headers for successful requests too
		bucket := rl.getBucket(tierName+":"+ip, tier)
		bucket.mu.Lock()
		remaining := int(bucket.tokens)
		bucket.mu.Unlock()

		c.Header("X-RateLimit-Limit", strconv.Itoa(tier.Requests))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

		c.Next()
	}
}

// AuthMiddleware returns middleware configured for auth endpoints (login/register)
func (rl *RateLimiter) AuthMiddleware() gin.HandlerFunc {
	return rl.Middleware("auth", AuthLimit)
}

// APIMiddleware returns middleware configured for general API endpoints
func (rl *RateLimiter) APIMiddleware() gin.HandlerFunc {
	return rl.Middleware("api", APILimit)
}

// WebSocketMiddleware returns middleware configured for WebSocket connections
func (rl *RateLimiter) WebSocketMiddleware() gin.HandlerFunc {
	return rl.Middleware("websocket", WebSocketLimit)
}

// SetAuthLimit updates the auth tier limit (useful for testing or runtime config)
func SetAuthLimit(requests int, window time.Duration) {
	AuthLimit = LimitTier{Requests: requests, Window: window}
}

// SetAPILimit updates the API tier limit
func SetAPILimit(requests int, window time.Duration) {
	APILimit = LimitTier{Requests: requests, Window: window}
}

// SetWebSocketLimit updates the WebSocket tier limit
func SetWebSocketLimit(requests int, window time.Duration) {
	WebSocketLimit = LimitTier{Requests: requests, Window: window}
}
