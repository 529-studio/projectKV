package flood

import (
	"sync"
	"time"
)

// QuotaLimiter implements a sliding-window rate limiter (RPM) and a calendar-day quota limiter (RPD).
type QuotaLimiter struct {
	mu           sync.Mutex
	limitRPM     int
	limitRPD     int
	nowFn        func() time.Time
	minuteWindow []time.Time
	dayCount     int
	currentDay   string
	lastVerified time.Time
}

// NewQuotaLimiter creates a new rate & quota limiter.
func NewQuotaLimiter(rpm, rpd int, clock ...func() time.Time) *QuotaLimiter {
	nowFn := time.Now
	if len(clock) > 0 && clock[0] != nil {
		nowFn = clock[0]
	}
	now := nowFn()
	return &QuotaLimiter{
		limitRPM:     rpm,
		limitRPD:     rpd,
		nowFn:        nowFn,
		minuteWindow: make([]time.Time, 0, rpm),
		currentDay:   now.UTC().Format("2006-01-02"),
	}
}

// Allow is a shorthand for TryAcquire that returns only whether the request was allowed.
func (l *QuotaLimiter) Allow() bool {
	allowed, _, _ := l.TryAcquire()
	return allowed
}

// TryAcquire attempts to acquire 1 token from both RPM and RPD quotas.
// If both limits have capacity, it records the request and returns true with remaining quotas.
// If either limit is exhausted, it returns false with current remaining values.
func (l *QuotaLimiter) TryAcquire() (bool, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFn()
	l.pruneAndRotate(now)

	remMin := l.limitRPM - len(l.minuteWindow)
	remDay := l.limitRPD - l.dayCount

	if remMin <= 0 || remDay <= 0 {
		if remMin < 0 {
			remMin = 0
		}
		if remDay < 0 {
			remDay = 0
		}
		return false, remMin, remDay
	}

	// Consume
	l.minuteWindow = append(l.minuteWindow, now)
	l.dayCount++
	l.lastVerified = now

	return true, l.limitRPM - len(l.minuteWindow), l.limitRPD - l.dayCount
}

// Status returns a snapshot of current quota utilization.
func (l *QuotaLimiter) Status() QuotaStatus {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFn()
	l.pruneAndRotate(now)

	remMin := l.limitRPM - len(l.minuteWindow)
	if remMin < 0 {
		remMin = 0
	}
	remDay := l.limitRPD - l.dayCount
	if remDay < 0 {
		remDay = 0
	}

	return QuotaStatus{
		LimitRPM:       l.limitRPM,
		RemainingRPM:   remMin,
		LimitRPD:       l.limitRPD,
		RemainingRPD:   remDay,
		CurrentDay:     l.currentDay,
		LastVerifiedAt: l.lastVerified,
	}
}

// Reset resets all quota counters.
func (l *QuotaLimiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFn()
	l.minuteWindow = l.minuteWindow[:0]
	l.dayCount = 0
	l.currentDay = now.UTC().Format("2006-01-02")
}

func (l *QuotaLimiter) pruneAndRotate(now time.Time) {
	// Rotate day if calendar day changed (UTC)
	dayStr := now.UTC().Format("2006-01-02")
	if dayStr != l.currentDay {
		l.currentDay = dayStr
		l.dayCount = 0
	}

	// Prune timestamps older than 60 seconds
	cutoff := now.Add(-1 * time.Minute)
	validIdx := 0
	for i, ts := range l.minuteWindow {
		if ts.After(cutoff) {
			validIdx = i
			break
		}
		if i == len(l.minuteWindow)-1 {
			validIdx = len(l.minuteWindow)
		}
	}
	if validIdx > 0 {
		l.minuteWindow = l.minuteWindow[validIdx:]
	}
}
