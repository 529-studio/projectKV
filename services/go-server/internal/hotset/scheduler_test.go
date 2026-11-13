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

// feedError mimics cvclient.FeedError: it marks an error as originating in the
// camera feed rather than in the inference engine.
type feedError struct {
	cameraID string
	reason   string
}

func (e feedError) Error() string     { return "feed failure for " + e.cameraID + ": " + e.reason }
func (e feedError) IsFeedError() bool { return true }

// TestScheduler_FeedErrorDoesNotTripBreaker verifies that a dead camera feed
// degrades only that camera to UNKNOWN and never records a breaker failure, so
// other cameras in the same Hot Set keep analysing normally.
func TestScheduler_FeedErrorDoesNotTripBreaker(t *testing.T) {
	cameras := []Camera{
		{ID: "cam_dead_feed", Name: "Dead Feed"},
		{ID: "cam_live_feed", Name: "Live Feed"},
	}
	reg := NewRegistry(cameras, 60*time.Second)

	analyzer := &perCameraAnalyzer{
		failure: feedError{cameraID: "cam_dead_feed", reason: "connection refused"},
		// Wrap the feed error the way real analyzers do, to prove errors.As
		// still detects it through a wrap chain.
		wrapped: true,
	}

	var mu sync.Mutex
	sources := make(map[string][]string)
	onVerdict := func(v Verdict) {
		mu.Lock()
		defer mu.Unlock()
		sources[v.CameraID] = append(sources[v.CameraID], v.Source)
	}

	sched := NewScheduler(reg, analyzer, 100*time.Millisecond, 1, onVerdict, nil)
	breaker := NewCircuitBreaker(2, time.Minute)
	sched.SetCircuitBreaker(breaker)

	ctx := context.Background()
	// Four cycles would be enough for two genuine failures to trip a
	// threshold-2 breaker; feed errors must never count.
	for i := 0; i < 4; i++ {
		sched.runOnce(ctx)
	}

	if got := breaker.ConsecutiveFailures(); got != 0 {
		t.Fatalf("feed errors must not advance the breaker, got %d consecutive failures", got)
	}
	if state := breaker.State(); state != StateClosed {
		t.Fatalf("expected breaker to stay CLOSED on feed errors, got %s", state)
	}
	if err := breaker.LastError(); err != nil {
		t.Fatalf("feed errors must not be recorded on the breaker, got LastError=%v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	deadSources := sources["cam_dead_feed"]
	if len(deadSources) != 4 {
		t.Fatalf("expected 4 verdicts for the dead camera, got %d", len(deadSources))
	}
	for _, src := range deadSources {
		if src != "fallback" {
			t.Errorf("dead camera must degrade via fallback every cycle, got source %q", src)
		}
	}
	if len(sources["cam_live_feed"]) != 4 {
		t.Fatalf("expected live camera to keep analysing for 4 cycles, got %d verdicts", len(sources["cam_live_feed"]))
	}
}

// TestScheduler_EngineErrorTripsBreaker verifies the complementary case: an
// inference-engine outage (not a feed error) still opens the shared breaker.
func TestScheduler_EngineErrorTripsBreaker(t *testing.T) {
	cam := Camera{ID: "cam_engine_down", Name: "Engine Down"}
	reg := NewRegistry([]Camera{cam}, 60*time.Second)

	analyzer := &testAnalyzer{
		failCount: 2,
		failErr:   errors.New("cv-engine /predict: status 503 (standby)"),
	}

	var mdl sync.Mutex
	var sources []string
	onVerdict := func(v Verdict) {
		mdl.Lock()
		defer mdl.Unlock()
		sources = append(sources, v.Source)
	}

	sched := NewScheduler(reg, analyzer, 100*time.Millisecond, 1, onVerdict, nil)
	breaker := NewCircuitBreaker(2, time.Minute)
	sched.SetCircuitBreaker(breaker)

	ctx := context.Background()
	sched.runOnce(ctx)
	sched.runOnce(ctx)

	if got := breaker.ConsecutiveFailures(); got != 2 {
		t.Fatalf("expected 2 engine failures recorded, got %d", got)
	}
	if state := breaker.State(); state != StateOpen {
		t.Fatalf("expected breaker OPEN after engine failures, got %s", state)
	}

	// Third cycle is short-circuited by the open breaker.
	sched.runOnce(ctx)

	mdl.Lock()
	defer mdl.Unlock()
	if len(sources) != 3 {
		t.Fatalf("expected 3 verdicts, got %d", len(sources))
	}
	for i := 0; i < 2; i++ {
		if sources[i] != "fallback" {
			t.Errorf("verdict %d: expected fallback, got %s", i, sources[i])
		}
	}
	if sources[2] != "circuit_breaker" {
		t.Errorf("verdict 2: expected circuit_breaker, got %s", sources[2])
	}
}

// perCameraAnalyzer fails only for one camera ID, succeeding for the rest so a
// single camera's defect can be observed in isolation.
type perCameraAnalyzer struct {
	failure error
	wrapped bool
}

func (a *perCameraAnalyzer) Analyze(ctx context.Context, cam Camera) (Verdict, error) {
	if cam.ID != "cam_dead_feed" {
		return Verdict{CameraID: cam.ID, Severity: SeverityClear, FloodSeverity: SeverityDry, Source: "test"}, nil
	}
	if a.wrapped {
		return Verdict{}, errors.Join(errors.New("analyze camera"), a.failure)
	}
	return Verdict{}, a.failure
}
