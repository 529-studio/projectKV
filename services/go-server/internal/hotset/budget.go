package hotset

import (
	"sync"
	"time"
)

// AnalysisBudget enforces a daily cap on analyzer invocations.
// When the cap is exhausted the circuit breaker trips: the scheduler stops
// calling the analyzer and degrades new verdicts to SeverityUnknown instead
// of reusing stale green state.
type AnalysisBudget struct {
	mu         sync.Mutex
	dailyLimit int
	used       int
	lastReset  time.Time // midnight UTC of the current budget window
	now        func() time.Time
}

// NewAnalysisBudget creates a budget that resets daily at midnight UTC.
func NewAnalysisBudget(dailyLimit int) *AnalysisBudget {
	return NewAnalysisBudgetWithClock(dailyLimit, time.Now)
}

// NewAnalysisBudgetWithClock is exported for tests that need to control time.
func NewAnalysisBudgetWithClock(dailyLimit int, now func() time.Time) *AnalysisBudget {
	if dailyLimit < 0 {
		dailyLimit = 0
	}
	if now == nil {
		now = time.Now
	}
	n := now().UTC()
	return &AnalysisBudget{
		dailyLimit: dailyLimit,
		used:       0,
		lastReset:  time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC),
		now:        now,
	}
}

// Allow reports whether one analyzer invocation is still permitted today.
// It automatically resets the counter at midnight UTC.
func (b *AnalysisBudget) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.After(b.lastReset) {
		b.used = 0
		b.lastReset = today
	}

	if b.used >= b.dailyLimit {
		return false
	}
	b.used++
	return true
}

// Tripped reports whether the daily budget is currently exhausted.
func (b *AnalysisBudget) Tripped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.After(b.lastReset) {
		return false
	}
	return b.used >= b.dailyLimit
}

// Remaining returns the number of analyzer calls still allowed today.
func (b *AnalysisBudget) Remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.After(b.lastReset) {
		return b.dailyLimit
	}
	rem := b.dailyLimit - b.used
	if rem < 0 {
		return 0
	}
	return rem
}
