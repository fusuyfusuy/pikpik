package deploy

import (
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// TokenBucketLimiter provides in-memory rate limiting with token-bucket algorithm.
type TokenBucketLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

// NewTokenBucketLimiter creates a new rate limiter instance.
func NewTokenBucketLimiter() *TokenBucketLimiter {
	limiter := &TokenBucketLimiter{
		buckets: make(map[string]*tokenBucket),
	}
	return limiter
}

// Allow checks whether a request associated with key is allowed under ratePerMin and burst capacity.
// Returns allowed bool and the duration to wait before retry if not allowed.
func (l *TokenBucketLimiter) Allow(key string, ratePerMin int, burst int) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Cleanup old buckets periodically if map gets large
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if now.Sub(b.lastRefill) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}

	b, exists := l.buckets[key]
	capacity := float64(burst)
	if capacity <= 0 {
		capacity = 1
	}

	rate := float64(ratePerMin) / 60.0
	if rate <= 0 {
		rate = 1.0 / 60.0
	}

	if !exists {
		// New bucket starts full
		b = &tokenBucket{
			tokens:     capacity - 1.0, // consume 1 token immediately
			capacity:   capacity,
			refillRate: rate,
			lastRefill: now,
		}
		l.buckets[key] = b
		return true, 0
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rate
	if b.tokens > capacity {
		b.tokens = capacity
	}
	b.lastRefill = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}

	// Calculate wait time
	missingTokens := 1.0 - b.tokens
	waitSeconds := missingTokens / rate
	retryAfter := time.Duration(waitSeconds * float64(time.Second))
	if retryAfter < time.Second {
		retryAfter = time.Second
	}

	return false, retryAfter
}
