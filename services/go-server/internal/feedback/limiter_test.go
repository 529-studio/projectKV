package feedback

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowFirstRequest(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Minute)
	now := time.Now()

	allowed, retryAfter := limiter.AllowAt("192.168.1.100", "cam_01", now)
	if !allowed {
		t.Fatalf("expected first request to be allowed, got rejected")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter 0, got %v", retryAfter)
	}
}

func TestRateLimiter_RejectsImmediateRepeat(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Minute)
	t0 := time.Now()

	allowed, _ := limiter.AllowAt("192.168.1.100", "cam_01", t0)
	if !allowed {
		t.Fatalf("first request should be allowed")
	}

	// 1 minute later
	t1 := t0.Add(1 * time.Minute)
	allowed, retryAfter := limiter.AllowAt("192.168.1.100", "cam_01", t1)
	if allowed {
		t.Fatalf("repeat request within cooldown should be rejected")
	}
	expectedRetry := 4 * time.Minute
	if retryAfter != expectedRetry {
		t.Errorf("expected retryAfter %v, got %v", expectedRetry, retryAfter)
	}
}

func TestRateLimiter_AllowsDifferentCameraSameIP(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Minute)
	t0 := time.Now()

	allowed1, _ := limiter.AllowAt("192.168.1.100", "cam_01", t0)
	if !allowed1 {
		t.Fatalf("first camera should be allowed")
	}

	allowed2, retryAfter := limiter.AllowAt("192.168.1.100", "cam_02", t0)
	if !allowed2 {
		t.Fatalf("different camera should be allowed for same IP")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter 0, got %v", retryAfter)
	}
}

func TestRateLimiter_AllowsDifferentIPSameCamera(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Minute)
	t0 := time.Now()

	allowed1, _ := limiter.AllowAt("192.168.1.100", "cam_01", t0)
	if !allowed1 {
		t.Fatalf("first IP should be allowed")
	}

	allowed2, retryAfter := limiter.AllowAt("192.168.1.101", "cam_01", t0)
	if !allowed2 {
		t.Fatalf("different IP should be allowed for same camera")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter 0, got %v", retryAfter)
	}
}

func TestRateLimiter_AllowsAfterCooldown(t *testing.T) {
	cooldown := 5 * time.Minute
	limiter := NewRateLimiter(cooldown)
	t0 := time.Now()

	limiter.AllowAt("192.168.1.100", "cam_01", t0)

	// After exactly 5 minutes + 1ms
	t1 := t0.Add(cooldown + time.Millisecond)
	allowed, retryAfter := limiter.AllowAt("192.168.1.100", "cam_01", t1)
	if !allowed {
		t.Fatalf("request after cooldown expiration should be allowed")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter 0, got %v", retryAfter)
	}
}

func TestRateLimiter_Prune(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Minute)
	t0 := time.Now()

	limiter.AllowAt("ip1", "cam1", t0)
	limiter.AllowAt("ip2", "cam2", t0.Add(2*time.Minute))
	limiter.AllowAt("ip3", "cam3", t0.Add(6*time.Minute))

	if limiter.Size() != 3 {
		t.Fatalf("expected size 3, got %d", limiter.Size())
	}

	// At t0 + 5m30s: ip1 is older than 5m (expired), ip2 is 3m30s old (active), ip3 is in the future relative to t0
	now := t0.Add(5*time.Minute + 30*time.Second)
	pruned := limiter.Prune(now)
	if pruned != 1 {
		t.Errorf("expected 1 record pruned, got %d", pruned)
	}
	if limiter.Size() != 2 {
		t.Errorf("expected size 2 after pruning, got %d", limiter.Size())
	}
}

func TestRateLimiter_ConcurrentSafety(t *testing.T) {
	limiter := NewRateLimiter(100 * time.Millisecond)
	var wg sync.WaitGroup
	workers := 50
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ip := "10.0.0.1"
				cam := "cam_shared"
				limiter.Allow(ip, cam)
				if j%10 == 0 {
					limiter.Prune(time.Now())
				}
			}
		}(i)
	}

	wg.Wait()
}
