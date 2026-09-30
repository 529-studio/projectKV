package hotset

import (
	"sync"
	"testing"
	"time"
)

func TestAnalysisBudget_AllowCountsDown(t *testing.T) {
	b := NewAnalysisBudgetWithClock(5, func() time.Time {
		return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	})

	for i := 0; i < 5; i++ {
		if !b.Allow() {
			t.Fatalf("expected Allow() true on call %d", i+1)
		}
	}
	if b.Allow() {
		t.Fatal("expected Allow() false after budget exhausted")
	}
	if got := b.Remaining(); got != 0 {
		t.Fatalf("expected Remaining=0, got %d", got)
	}
}

func TestAnalysisBudget_Tripped(t *testing.T) {
	b := NewAnalysisBudgetWithClock(2, nil)

	if b.Tripped() {
		t.Fatal("expected not tripped initially")
	}
	_ = b.Allow()
	_ = b.Allow()
	if !b.Tripped() {
		t.Fatal("expected tripped after budget exhausted")
	}
}

func TestAnalysisBudget_ResetsAfterMidnight(t *testing.T) {
	now := time.Date(2026, 9, 29, 23, 59, 59, 0, time.UTC)
	b := NewAnalysisBudgetWithClock(1, func() time.Time { return now })

	if !b.Allow() {
		t.Fatal("expected first Allow true")
	}
	if b.Allow() {
		t.Fatal("expected Allow false before midnight")
	}

	// Roll the clock past midnight UTC.
	now = time.Date(2026, 9, 30, 0, 0, 1, 0, time.UTC)
	if !b.Allow() {
		t.Fatal("expected Allow true after midnight reset")
	}
}

func TestAnalysisBudget_ConcurrentAllow(t *testing.T) {
	const limit = 1000
	b := NewAnalysisBudgetWithClock(limit, nil)

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if b.Allow() {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if allowed != limit {
		t.Fatalf("expected exactly %d allowed calls, got %d", limit, allowed)
	}
}

func TestAnalysisBudget_NegativeLimit(t *testing.T) {
	b := NewAnalysisBudgetWithClock(-1, nil)
	if b.Allow() {
		t.Fatal("expected Allow false with negative limit")
	}
}
