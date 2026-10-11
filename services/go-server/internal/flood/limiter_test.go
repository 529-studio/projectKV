package flood_test

import (
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/flood"
)

func TestQuotaLimiter_AllowWithinLimit(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	limiter := flood.NewQuotaLimiter(10, 300, clock)

	for i := 1; i <= 10; i++ {
		allowed, remMin, remDay := limiter.TryAcquire()
		if !allowed {
			t.Fatalf("expected acquire %d to be allowed", i)
		}
		if remMin != 10-i {
			t.Errorf("expected remMin %d, got %d", 10-i, remMin)
		}
		if remDay != 300-i {
			t.Errorf("expected remDay %d, got %d", 300-i, remDay)
		}
	}
}

func TestQuotaLimiter_RejectExceedingRPM(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	limiter := flood.NewQuotaLimiter(10, 300, clock)

	for i := 1; i <= 10; i++ {
		if !limiter.Allow() {
			t.Fatalf("expected call %d to succeed", i)
		}
	}

	allowed, remMin, _ := limiter.TryAcquire()
	if allowed {
		t.Fatalf("expected 11th call to be rejected by RPM")
	}
	if remMin != 0 {
		t.Errorf("expected remMin 0, got %d", remMin)
	}
}

func TestQuotaLimiter_SlidingWindowExpiration(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	limiter := flood.NewQuotaLimiter(10, 300, clock)

	// Consume 10 at T=0
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	if limiter.Allow() {
		t.Fatalf("expected 11th call at T=0 to fail")
	}

	// Advance clock by 65 seconds
	now = now.Add(65 * time.Second)

	allowed, remMin, remDay := limiter.TryAcquire()
	if !allowed {
		t.Fatalf("expected call at T+65s to succeed after sliding window pruned")
	}
	if remMin != 9 {
		t.Errorf("expected remMin 9, got %d", remMin)
	}
	if remDay != 289 { // 10 from previous minute + 1 now = 11 consumed total today
		t.Errorf("expected remDay 289, got %d", remDay)
	}
}

func TestQuotaLimiter_DailyQuotaExceeded(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	// Create limiter with RPM=100, RPD=5
	limiter := flood.NewQuotaLimiter(100, 5, clock)

	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Fatalf("expected call %d to succeed", i+1)
		}
	}

	allowed, _, remDay := limiter.TryAcquire()
	if allowed {
		t.Fatalf("expected 6th call to fail due to RPD")
	}
	if remDay != 0 {
		t.Errorf("expected remDay 0, got %d", remDay)
	}
}

func TestQuotaLimiter_DailyQuotaResetOnNewDay(t *testing.T) {
	now := time.Date(2026, 10, 11, 23, 59, 30, 0, time.UTC)
	clock := func() time.Time { return now }

	limiter := flood.NewQuotaLimiter(10, 5, clock)

	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	if limiter.Allow() {
		t.Fatalf("expected exhaustion on day 1")
	}

	// Roll over to next day
	now = time.Date(2026, 10, 12, 0, 1, 0, 0, time.UTC)

	allowed, remMin, remDay := limiter.TryAcquire()
	if !allowed {
		t.Fatalf("expected call to succeed on new day")
	}
	if remMin != 9 {
		t.Errorf("expected remMin 9, got %d", remMin)
	}
	if remDay != 4 {
		t.Errorf("expected remDay 4 on new day, got %d", remDay)
	}
}

func TestQuotaLimiter_ConcurrentSafety(t *testing.T) {
	limiter := flood.NewQuotaLimiter(10, 10)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	rejectCount := 0

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow() {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else {
				mu.Lock()
				rejectCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount != 10 {
		t.Errorf("expected exactly 10 successes, got %d", successCount)
	}
	if rejectCount != 40 {
		t.Errorf("expected exactly 40 rejects, got %d", rejectCount)
	}
}
