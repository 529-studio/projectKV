package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestIPLimiter_BurstAllowsInitialSpike(t *testing.T) {
	now := time.Now()
	l := newIPLimiter(1, 3, func() time.Time { return now })
	defer l.stop()

	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("expected allow true on burst request %d", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("expected allow false after burst consumed")
	}
}

func TestIPLimiter_SustainedRateLimited(t *testing.T) {
	now := time.Now()
	l := newIPLimiter(10, 1, func() time.Time { return now })
	defer l.stop()

	if !l.allow("1.2.3.4") {
		t.Fatal("expected first request allowed")
	}
	if l.allow("1.2.3.4") {
		t.Fatal("expected second request blocked immediately")
	}

	// Advance 100ms: 10 rps => 1 token available.
	now = now.Add(100 * time.Millisecond)
	if !l.allow("1.2.3.4") {
		t.Fatal("expected request allowed after 100ms at 10 rps")
	}
}

func TestIPLimiter_DifferentIPsIndependent(t *testing.T) {
	now := time.Now()
	l := newIPLimiter(1, 1, func() time.Time { return now })
	defer l.stop()

	if !l.allow("1.2.3.4") {
		t.Fatal("expected ip-A allowed")
	}
	if !l.allow("5.6.7.8") {
		t.Fatal("expected ip-B allowed independently")
	}
}

func TestRateLimitMiddleware_ExemptPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := rateLimitMiddlewareWithClock(1, 1, time.Now)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	called := false
	middleware(c)
	if c.IsAborted() {
		t.Fatal("expected exempt path not to be aborted")
	}
	_ = called
}

func TestRateLimitMiddleware_BlocksAfterBurst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	middleware := rateLimitMiddlewareWithClock(1, 2, func() time.Time { return now })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
		c.Request.RemoteAddr = "1.2.3.4:12345"
		middleware(c)
		if c.IsAborted() {
			t.Fatalf("expected request %d to pass", i+1)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	c.Request.RemoteAddr = "1.2.3.4:12345"
	middleware(c)
	if !c.IsAborted() {
		t.Fatal("expected third request to be aborted")
	}
	if got := w.Code; got != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", got)
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After=1, got %q", got)
	}
}

func TestRateLimitMiddleware_DisabledWhenMisconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware := rateLimitMiddlewareWithClock(0, 0, time.Now)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	middleware(c)
	if c.IsAborted() {
		t.Fatal("expected middleware to be no-op when misconfigured")
	}
}

func TestIsExempt(t *testing.T) {
	cases := []struct {
		path   string
		exempt bool
	}{
		{"/health", true},
		{"/healthz", true},
		{"/", true},
		{"/index.html", true},
		{"/_next/static/chunk.js", true},
		{"/swagger/index.html", true},
		{"/styles.css", true},
		{"/api/cameras", false},
		{"/api/traffic-status", false},
	}
	for _, tc := range cases {
		if got := isExempt(tc.path); got != tc.exempt {
			t.Errorf("isExempt(%q) = %v, want %v", tc.path, got, tc.exempt)
		}
	}
}
