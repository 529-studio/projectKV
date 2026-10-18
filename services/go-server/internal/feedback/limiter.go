package feedback

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter enforces sliding window cooldown per client IP and camera.
type RateLimiter struct {
	mu       sync.RWMutex
	cooldown time.Duration
	records  map[string]time.Time
}

// NewRateLimiter creates a new RateLimiter with the specified cooldown.
func NewRateLimiter(cooldown time.Duration) *RateLimiter {
	if cooldown <= 0 {
		cooldown = DefaultRateLimitCooldown
	}
	return &RateLimiter{
		cooldown: cooldown,
		records:  make(map[string]time.Time),
	}
}

// Cooldown returns the configured rate limit cooldown duration.
func (r *RateLimiter) Cooldown() time.Duration {
	return r.cooldown
}

// Allow checks if the given client IP is permitted to submit a report for cameraID at time.Now().
func (r *RateLimiter) Allow(clientIP, cameraID string) (bool, time.Duration) {
	return r.AllowAt(clientIP, cameraID, time.Now())
}

// AllowAt checks if the given client IP is permitted to submit a report for cameraID at timestamp now.
func (r *RateLimiter) AllowAt(clientIP, cameraID string, now time.Time) (bool, time.Duration) {
	if clientIP == "" || cameraID == "" {
		return false, 0
	}

	key := fmt.Sprintf("%s:%s", clientIP, cameraID)

	r.mu.Lock()
	defer r.mu.Unlock()

	last, exists := r.records[key]
	if exists {
		elapsed := now.Sub(last)
		if elapsed < r.cooldown {
			return false, r.cooldown - elapsed
		}
	}

	r.records[key] = now
	return true, 0
}

// Prune removes records older than the cooldown period relative to now.
func (r *RateLimiter) Prune(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	pruned := 0
	for key, ts := range r.records {
		if now.Sub(ts) >= r.cooldown {
			delete(r.records, key)
			pruned++
		}
	}
	return pruned
}

// Size returns the current number of tracked IP-camera entries.
func (r *RateLimiter) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.records)
}
