package hotset

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type testAnalyzer struct {
	mu        sync.Mutex
	failCount int
	failErr   error
	verdict   Verdict
}

func (m *testAnalyzer) Analyze(ctx context.Context, cam Camera) (Verdict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCount > 0 {
		m.failCount--
		return Verdict{}, m.failErr
	}
	v := m.verdict
	v.CameraID = cam.ID
	return v, nil
}

func TestScheduler_GracefulDegradationOnAnalysisFailure(t *testing.T) {
	cam := Camera{ID: "cam_err_01", Name: "Error Camera"}
	reg := NewRegistry([]Camera{cam}, 60*time.Second)

	analyzer := &testAnalyzer{
		failCount: 1,
		failErr:   errors.New("connection refused: warded-model:8000 unreachable"),
	}

	var received Verdict
	var mu sync.Mutex
	done := make(chan struct{})

	onVerdict := func(v Verdict) {
		mu.Lock()
		received = v
		mu.Unlock()
		close(done)
	}

	sched := NewScheduler(reg, analyzer, 100*time.Millisecond, 1, onVerdict, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sched.runOnce(ctx)

	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		if received.Severity != SeverityUnknown {
			t.Fatalf("expected SeverityUnknown, got %s", received.Severity)
		}
		if received.FloodSeverity != SeverityUnknown {
			t.Fatalf("expected FloodSeverity Unknown, got %s", received.FloodSeverity)
		}
		if received.Source != "fallback" {
			t.Fatalf("expected source 'fallback', got %s", received.Source)
		}
		if !received.Stale {
			t.Fatal("expected Stale=true on degraded verdict")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for degraded fallback verdict")
	}
}

func TestScheduler_CircuitBreakerTripsAndDegrades(t *testing.T) {
	cam := Camera{ID: "cam_cb_01", Name: "Breaker Camera"}
	reg := NewRegistry([]Camera{cam}, 60*time.Second)

	analyzer := &testAnalyzer{
		failCount: 10, // fail consecutively
		failErr:   errors.New("model timeout"),
	}

	var verdicts []Verdict
	var mu sync.Mutex

	onVerdict := func(v Verdict) {
		mu.Lock()
		verdicts = append(verdicts, v)
		mu.Unlock()
	}

	sched := NewScheduler(reg, analyzer, 100*time.Millisecond, 1, onVerdict, nil)
	// Threshold = 2 fails
	sched.SetCircuitBreaker(NewCircuitBreaker(2, 10*time.Second))

	ctx := context.Background()

	// Run 1: 1st failure -> fallback
	sched.runOnce(ctx)
	// Run 2: 2nd failure -> trips breaker
	sched.runOnce(ctx)
	// Run 3: Breaker is Open -> trips immediately to circuit_breaker
	sched.runOnce(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(verdicts) < 3 {
		t.Fatalf("expected at least 3 verdicts, got %d", len(verdicts))
	}
	if verdicts[0].Source != "fallback" {
		t.Errorf("verdict 0: expected fallback, got %s", verdicts[0].Source)
	}
	if verdicts[1].Source != "fallback" {
		t.Errorf("verdict 1: expected fallback, got %s", verdicts[1].Source)
	}
	if verdicts[2].Source != "circuit_breaker" {
		t.Errorf("verdict 2: expected circuit_breaker, got %s", verdicts[2].Source)
	}
	if verdicts[2].Severity != SeverityUnknown {
		t.Errorf("verdict 2: expected SeverityUnknown, got %s", verdicts[2].Severity)
	}
}
