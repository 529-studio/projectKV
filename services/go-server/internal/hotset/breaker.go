package hotset

import (
	"sync"
	"time"
)

// BreakerState represents the current lifecycle state of a CircuitBreaker.
type BreakerState string

const (
	StateClosed   BreakerState = "CLOSED"
	StateHalfOpen BreakerState = "HALF_OPEN"
	StateOpen     BreakerState = "OPEN"
)

// CircuitBreaker guards against cascading failures when an upstream or downstream
// analyzer service becomes unreachable or unhealthy. When consecutive failures exceed
// failureThreshold, the breaker transitions to StateOpen, preventing calls to the downstream
// dependency and allowing immediate graceful degradation to SeverityUnknown.
type CircuitBreaker struct {
	mu               sync.RWMutex
	failureThreshold int
	cooldown         time.Duration
	state            BreakerState
	consecutiveFails int
	lastFailureTime  time.Time
	lastError        error
	now              func() time.Time
}

// NewCircuitBreaker constructs a CircuitBreaker with specified failure threshold and cooldown.
func NewCircuitBreaker(failureThreshold int, cooldown time.Duration) *CircuitBreaker {
	return NewCircuitBreakerWithClock(failureThreshold, cooldown, time.Now)
}

// NewCircuitBreakerWithClock is exported for tests that require deterministic time mocking.
func NewCircuitBreakerWithClock(failureThreshold int, cooldown time.Duration, now func() time.Time) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		state:            StateClosed,
		now:              now,
	}
}

// Allow reports whether a downstream analyzer execution is permitted.
// If the breaker is Open and cooldown has elapsed, it transitions to HalfOpen to allow a probe.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.now()
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if now.Sub(cb.lastFailureTime) >= cb.cooldown {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		// In HalfOpen state, allow the trial probe
		return true
	default:
		return true
	}
}

// RecordSuccess registers a successful execution, resetting failure counters and closing the breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails = 0
	cb.state = StateClosed
	cb.lastError = nil
}

// RecordFailure registers a failure, advancing failure counts and opening the breaker if threshold is met.
func (cb *CircuitBreaker) RecordFailure(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastError = err
	cb.lastFailureTime = cb.now()
	cb.consecutiveFails++

	if cb.state == StateHalfOpen || cb.consecutiveFails >= cb.failureThreshold {
		cb.state = StateOpen
	}
}

// State returns the current BreakerState.
func (cb *CircuitBreaker) State() BreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	now := cb.now()
	if cb.state == StateOpen && now.Sub(cb.lastFailureTime) >= cb.cooldown {
		return StateHalfOpen
	}
	return cb.state
}

// ConsecutiveFailures returns the number of recorded consecutive failures.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.consecutiveFails
}

// LastError returns the most recent error recorded by the breaker.
func (cb *CircuitBreaker) LastError() error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.lastError
}
