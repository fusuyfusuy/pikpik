package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type rateRecord struct {
	timestamps []time.Time
}

// RateLimiter implements an in-memory sliding-window rate limiter.
type RateLimiter struct {
	limit   int
	window  time.Duration
	records map[string]*rateRecord
	mu      sync.Mutex
}

// NewRateLimiter creates a new RateLimiter with the given limit and window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 600
	}
	if window <= 0 {
		window = time.Minute
	}
	rl := &RateLimiter{
		limit:   limit,
		window:  window,
		records: make(map[string]*rateRecord),
	}
	return rl
}

// Allow checks whether a request with the given key is allowed under the sliding window limit.
func (rl *RateLimiter) Allow(key string) (allowed bool, remaining int, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	rec, exists := rl.records[key]
	if !exists {
		rec = &rateRecord{timestamps: make([]time.Time, 0, rl.limit)}
		rl.records[key] = rec
	}

	// Prune timestamps older than cutoff
	validIdx := 0
	for i, ts := range rec.timestamps {
		if ts.After(cutoff) {
			validIdx = i
			break
		}
		if i == len(rec.timestamps)-1 {
			validIdx = len(rec.timestamps)
		}
	}
	if validIdx > 0 {
		rec.timestamps = rec.timestamps[validIdx:]
	}

	count := len(rec.timestamps)
	if count >= rl.limit {
		// Rate limited: calculate retry after based on oldest timestamp in window
		oldest := rec.timestamps[0]
		retry := oldest.Add(rl.window).Sub(now)
		if retry <= 0 {
			retry = time.Second
		}
		return false, 0, retry
	}

	// Allow request and record timestamp
	rec.timestamps = append(rec.timestamps, now)
	rem := rl.limit - len(rec.timestamps)
	return true, rem, 0
}

// TieredRateLimiter coordinates rate limiting across endpoint scopes.
type TieredRateLimiter struct {
	loginLimiter *RateLimiter // 5 / min
	nudgeLimiter *RateLimiter // 60 / min
	restLimiter  *RateLimiter // 600 / min
	wsLimiter    *RateLimiter // 30 / min
}

// NewTieredRateLimiter initializes standard rate limiting tiers.
func NewTieredRateLimiter() *TieredRateLimiter {
	return &TieredRateLimiter{
		loginLimiter: NewRateLimiter(5, time.Minute),
		nudgeLimiter: NewRateLimiter(60, time.Minute),
		restLimiter:  NewRateLimiter(600, time.Minute),
		wsLimiter:    NewRateLimiter(30, time.Minute),
	}
}

// SetRateLimitHeaders injects X-RateLimit headers into the response.
func SetRateLimitHeaders(w http.ResponseWriter, limit, remaining int, retryAfter time.Duration) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	resetUnix := time.Now().Add(retryAfter).Unix()
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetUnix))
	if retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
	}
}
