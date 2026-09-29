package middleware

import (
	"math"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	cleanupInterval = 5 * time.Minute
	bucketTTL       = 10 * time.Minute
)

// bucket is a simple token bucket for a single client IP.
type bucket struct {
	tokens float64
	rate   float64 // tokens added per second
	burst  float64 // maximum bucket capacity
	last   time.Time
}

func (b *bucket) allow(now time.Time) bool {
	if b.last.IsZero() {
		b.last = now
		b.tokens = b.burst
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens = math.Min(b.burst, b.tokens+elapsed*b.rate)
			b.last = now
		}
	}

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// ipLimiter stores token buckets keyed by client IP. A cleanup goroutine
// removes stale buckets periodically.
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
	now     func() time.Time
	stopCh  chan struct{}
}

func newIPLimiter(rps, burst int, now func() time.Time) *ipLimiter {
	if now == nil {
		now = time.Now
	}
	l := &ipLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(rps),
		burst:   float64(burst),
		now:     now,
		stopCh:  make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *ipLimiter) stop() {
	close(l.stopCh)
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{rate: l.rate, burst: l.burst, last: l.now(), tokens: l.burst}
		l.buckets[ip] = b
	}
	return b.allow(l.now())
}

func (l *ipLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.mu.Lock()
			now := l.now()
			for ip, b := range l.buckets {
				if now.Sub(b.last) > bucketTTL {
					delete(l.buckets, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

// isExempt reports whether a request path should bypass rate limiting.
func isExempt(p string) bool {
	switch p {
	case "/health", "/healthz":
		return true
	}

	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".css", ".js", ".png", ".jpg", ".jpeg", ".svg", ".ico",
		".woff", ".woff2", ".ttf", ".eot", ".json", ".xml", ".webmanifest":
		return true
	}

	if p == "/" || p == "/index.html" || strings.HasPrefix(p, "/_next/") || strings.HasPrefix(p, "/swagger") {
		return true
	}
	return false
}

// rateLimitMiddlewareWithClock is the testable core of the middleware.
func rateLimitMiddlewareWithClock(rps, burst int, now func() time.Time) gin.HandlerFunc {
	if rps <= 0 || burst <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	limiter := newIPLimiter(rps, burst, now)
	return func(c *gin.Context) {
		if isExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}
		if !limiter.allow(ip) {
			c.Header("Retry-After", "1")
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

// RateLimitMiddleware returns a Gin middleware that applies a per-IP token
// bucket rate limiter. Health checks and static assets are exempt.
func RateLimitMiddleware(rps, burst int) gin.HandlerFunc {
	return rateLimitMiddlewareWithClock(rps, burst, time.Now)
}
