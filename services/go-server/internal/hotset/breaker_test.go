package hotset

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Second)
	if cb.State() != StateClosed {
		t.Fatalf("expected initial state %s, got %s", StateClosed, cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow() to be true in initial closed state")
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Second)

	cb.RecordFailure(errors.New("fail 1"))
	if cb.State() != StateClosed {
		t.Fatalf("expected state %s after 1 fail, got %s", StateClosed, cb.State())
	}
	cb.RecordFailure(errors.New("fail 2"))
	if cb.State() != StateClosed {
		t.Fatalf("expected state %s after 2 fails, got %s", StateClosed, cb.State())
	}

	cb.RecordFailure(errors.New("fail 3"))
	if cb.State() != StateOpen {
		t.Fatalf("expected state %s after 3 fails, got %s", StateOpen, cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected Allow() to be false when Open")
	}
}

func TestCircuitBreaker_CooldownTransitionToHalfOpen(t *testing.T) {
	currTime := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cb := NewCircuitBreakerWithClock(2, 5*time.Second, func() time.Time { return currTime })

	cb.RecordFailure(errors.New("fail 1"))
	cb.RecordFailure(errors.New("fail 2"))
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Advance time by 6s past cooldown
	currTime = currTime.Add(6 * time.Second)
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected HalfOpen after cooldown, got %s", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow() true in HalfOpen for probe")
	}

	// Success resets to Closed
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("expected Closed after success probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	currTime := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cb := NewCircuitBreakerWithClock(1, 5*time.Second, func() time.Time { return currTime })

	cb.RecordFailure(errors.New("fail 1"))
	if cb.State() != StateOpen {
		t.Fatalf("expected Open, got %s", cb.State())
	}

	// Advance past cooldown -> HalfOpen
	currTime = currTime.Add(6 * time.Second)
	_ = cb.Allow()

	// Failure in HalfOpen immediately trips back to Open
	cb.RecordFailure(errors.New("probe failed"))
	if cb.State() != StateOpen {
		t.Fatalf("expected Open after probe failure, got %s", cb.State())
	}
}
