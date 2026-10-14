package notify

import (
	"sync"
	"testing"
	"time"
)

func TestDebouncer_SlidingWindow(t *testing.T) {
	d := NewDebouncer(30 * time.Minute)
	t0 := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)

	// 1. First alert for cam-01 at T=0: MUST be allowed
	if d.ShouldSuppress("cam-01", t0) {
		t.Fatalf("expected first alert at T=0 to be allowed (not suppressed)")
	}

	// 2. Second alert for cam-01 at T+10m: MUST be suppressed
	t10 := t0.Add(10 * time.Minute)
	if !d.ShouldSuppress("cam-01", t10) {
		t.Fatalf("expected alert at T+10m to be suppressed by 30-minute window")
	}

	// 3. Third alert for cam-01 at T+29m: MUST be suppressed
	t29 := t0.Add(29 * time.Minute)
	if !d.ShouldSuppress("cam-01", t29) {
		t.Fatalf("expected alert at T+29m to be suppressed by 30-minute window")
	}

	// 4. Alert for cam-02 at T+15m: different camera, MUST be allowed
	t15 := t0.Add(15 * time.Minute)
	if d.ShouldSuppress("cam-02", t15) {
		t.Fatalf("expected alert for different camera cam-02 to be allowed")
	}

	// 5. Fourth alert for cam-01 at T+31m: past 30m quiet period, MUST be allowed
	t31 := t0.Add(31 * time.Minute)
	if d.ShouldSuppress("cam-01", t31) {
		t.Fatalf("expected alert at T+31m to be allowed after quiet window")
	}

	// 6. Alert for cam-01 at T+45m: 14m after T+31m, MUST be suppressed
	t45 := t0.Add(45 * time.Minute)
	if !d.ShouldSuppress("cam-01", t45) {
		t.Fatalf("expected alert at T+45m to be suppressed relative to new T+31m window")
	}
}

func TestDebouncer_Reset(t *testing.T) {
	d := NewDebouncer(30 * time.Minute)
	t0 := time.Now()

	if d.ShouldSuppress("cam-01", t0) {
		t.Fatalf("expected first alert to be allowed")
	}
	if !d.ShouldSuppress("cam-01", t0.Add(5*time.Minute)) {
		t.Fatalf("expected subsequent alert to be suppressed")
	}

	d.Reset("cam-01")
	if d.ShouldSuppress("cam-01", t0.Add(5*time.Minute)) {
		t.Fatalf("expected alert to be allowed after Reset")
	}
}

func TestDebouncer_ConcurrentAccess(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cam := "cam-concurrent"
			_ = d.ShouldSuppress(cam, time.Now())
			_ = d.LastDispatched(cam)
		}(i)
	}
	wg.Wait()
}
