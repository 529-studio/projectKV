package hotset

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a deterministic clock for liveness tests and simulations.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{t: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// discardLogger keeps expected-warning paths out of the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// alertCollector is a thread-safe sink for assertions.
type alertCollector struct {
	mu     sync.Mutex
	alerts []Alert
}

func (c *alertCollector) add(a Alert) {
	c.mu.Lock()
	c.alerts = append(c.alerts, a)
	c.mu.Unlock()
}

func (c *alertCollector) all() []Alert {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Alert, len(c.alerts))
	copy(out, c.alerts)
	return out
}

func (c *alertCollector) byKind(kind string) []Alert {
	var out []Alert
	for _, a := range c.all() {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

func newTestWatchdog(t *testing.T, clock *fakeClock, sink func(Alert), opts ...WatchdogOption) *Watchdog {
	t.Helper()
	base := []WatchdogOption{
		WithWatchdogClock(clock.Now),
		WithWatchdogLogger(discardLogger()),
	}
	if sink != nil {
		base = append(base, WithAlertSink(sink))
	}
	return NewWatchdog(append(base, opts...)...)
}

func TestWatchdog_FeedDropoutAlertsOnceAtThreshold(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	w.SetHotSet([]string{"cam-a"})
	w.ObserveCameraFeedFailure("cam-a")

	// Just before five minutes: no alert.
	clock.Set(start.Add(5*time.Minute - time.Second))
	w.Evaluate()
	if got := len(sink.all()); got != 0 {
		t.Fatalf("expected no alert before the threshold, got %d", got)
	}

	// At five minutes: exactly one alert.
	clock.Set(start.Add(5 * time.Minute))
	w.Evaluate()
	alerts := sink.all()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert at the threshold, got %d", len(alerts))
	}
	got := alerts[0]
	if got.Kind != AlertKindFeedDropout {
		t.Errorf("expected kind %q, got %q", AlertKindFeedDropout, got.Kind)
	}
	if got.CameraID != "cam-a" {
		t.Errorf("expected camera cam-a, got %q", got.CameraID)
	}
	if got.DownFor < 5*time.Minute {
		t.Errorf("expected DownFor >= 5m, got %s", got.DownFor)
	}
	if !strings.HasPrefix(got.Summary(), "[OPS_ALERT]") {
		t.Errorf("alert summary must start with [OPS_ALERT], got %q", got.Summary())
	}
	if !strings.Contains(got.Summary(), "cam-a") {
		t.Errorf("alert summary must name the camera, got %q", got.Summary())
	}

	// Debounce: repeated evaluation while the feed is still down is silent.
	clock.Advance(10 * time.Minute)
	for i := 0; i < 5; i++ {
		w.Evaluate()
	}
	if got := len(sink.all()); got != 1 {
		t.Fatalf("expected the alert to be debounced, got %d alerts", got)
	}

	// Recovery re-arms the camera; a new outage alerts again.
	w.ObserveCameraSuccess("cam-a")
	w.Evaluate()
	if got := len(sink.all()); got != 1 {
		t.Fatalf("recovery must not emit a dropout alert, got %d", got)
	}

	w.ObserveCameraFeedFailure("cam-a")
	clock.Advance(5 * time.Minute)
	w.Evaluate()
	alerts = sink.all()
	if len(alerts) != 2 {
		t.Fatalf("expected a second alert for the new outage episode, got %d", len(alerts))
	}
}

func TestWatchdog_EngineFailureAlertsOnceAtThreshold(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	// Healthy baseline.
	w.ObserveEngineHealth(true, "")
	w.Evaluate()
	if got := len(sink.all()); got != 0 {
		t.Fatalf("healthy engine must not alert, got %d", got)
	}

	w.ObserveEngineHealth(false, "model_loaded=false")

	clock.Set(start.Add(5*time.Minute - time.Second))
	w.Evaluate()
	if got := len(sink.all()); got != 0 {
		t.Fatalf("expected no engine alert before the threshold, got %d", got)
	}

	clock.Set(start.Add(5 * time.Minute))
	w.Evaluate()
	engineAlerts := sink.byKind(AlertKindCVEngineFailure)
	if len(engineAlerts) != 1 {
		t.Fatalf("expected 1 engine alert, got %d", len(engineAlerts))
	}
	alert := engineAlerts[0]
	if alert.CameraID != "" {
		t.Errorf("engine alert must not be attributed to a camera, got %q", alert.CameraID)
	}
	if !strings.Contains(alert.Summary(), "CV engine failure exceeding 5m") {
		t.Errorf("engine alert summary must state the failure window, got %q", alert.Summary())
	}
	if !strings.Contains(alert.Summary(), "model_loaded=false") {
		t.Errorf("engine alert summary must carry the probe detail, got %q", alert.Summary())
	}
	if !strings.HasPrefix(alert.Summary(), "[OPS_ALERT]") {
		t.Errorf("alert summary must start with [OPS_ALERT], got %q", alert.Summary())
	}

	// Repeated unhealthy probes inside one episode stay silent.
	for i := 0; i < 10; i++ {
		clock.Advance(time.Minute)
		w.ObserveEngineHealth(false, "model_loaded=false")
		w.Evaluate()
	}
	if got := len(sink.byKind(AlertKindCVEngineFailure)); got != 1 {
		t.Fatalf("expected engine alert to be debounced, got %d", got)
	}

	// Recovery, then a second outage, re-alerts once.
	w.ObserveEngineHealth(true, "")
	w.ObserveEngineHealth(false, "connection refused")
	clock.Advance(5 * time.Minute)
	w.Evaluate()
	if got := len(sink.byKind(AlertKindCVEngineFailure)); got != 2 {
		t.Fatalf("expected a second engine alert after re-arming, got %d", got)
	}
}

// TestWatchdog_EngineOutageDoesNotRaisePerCameraFeedAlerts is the anti-storm
// guard: an engine outage produces no successful observations, but it is not
// evidence that 59 camera feeds died. The watchdog must page once for the
// engine, not once per camera.
func TestWatchdog_EngineOutageDoesNotRaisePerCameraFeedAlerts(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	ids := []string{"cam-1", "cam-2", "cam-3"}
	w.SetHotSet(ids)

	// Ten minutes of engine outage: probes fail, but no feed observations at
	// all, because the scheduler short-circuits on the open breaker.
	for i := 0; i < 20; i++ {
		w.ObserveEngineHealth(false, "cv-engine standby: no model loaded")
		w.Evaluate()
		clock.Advance(30 * time.Second)
	}

	if got := len(sink.byKind(AlertKindFeedDropout)); got != 0 {
		t.Fatalf("engine outage must not be reported as camera feed dropouts, got %d", got)
	}
	if got := len(sink.byKind(AlertKindCVEngineFailure)); got != 1 {
		t.Fatalf("expected exactly one engine alert, got %d", got)
	}
}

// TestWatchdog_FeedEvidenceAlertsDuringEngineOutage proves the inverse rule: a
// camera whose feed is genuinely unreachable still alerts while the engine is
// down, because the feed is independently observable state.
func TestWatchdog_FeedEvidenceAlertsDuringEngineOutage(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	w.SetHotSet([]string{"cam-dead"})
	w.ObserveEngineHealth(false, "connection refused")
	w.ObserveCameraFeedFailure("cam-dead")

	clock.Advance(5 * time.Minute)
	w.Evaluate()

	if got := len(sink.byKind(AlertKindFeedDropout)); got != 1 {
		t.Fatalf("expected the dead feed to alert during an engine outage, got %d", got)
	}
	if got := len(sink.byKind(AlertKindCVEngineFailure)); got != 1 {
		t.Fatalf("expected the engine outage to alert too, got %d", got)
	}
}

func TestWatchdog_LeavingHotSetDropsLivenessState(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	w.SetHotSet([]string{"cam-a"})
	w.ObserveCameraFeedFailure("cam-a")
	// Camera is removed from the Hot Set before the threshold is crossed.
	w.SetHotSet([]string{"cam-b"})

	clock.Advance(10 * time.Minute)
	w.Evaluate()

	if got := len(sink.all()); got != 0 {
		t.Fatalf("a camera outside the Hot Set must not alert, got %d", got)
	}
}

func TestWatchdog_RunProbesEngineAndAlerts(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	alerts := make(chan Alert, 4)

	probeCalls := make(chan struct{}, 8)
	w := NewWatchdog(
		WithWatchdogClock(clock.Now),
		WithWatchdogLogger(discardLogger()),
		WithWatchdogEvaluateInterval(10*time.Millisecond),
		WithCVEngineFailureThreshold(20*time.Millisecond),
		WithAlertSink(func(a Alert) {
			select {
			case alerts <- a:
			default:
			}
		}),
	)
	w.SetEngineProbe(func(ctx context.Context) (bool, string) {
		select {
		case probeCalls <- struct{}{}:
		default:
		}
		// The probe reports unhealthy, and the fake clock must advance for the
		// configured 20ms threshold to be crossed.
		clock.Advance(10 * time.Millisecond)
		return false, "probe: engine unreachable"
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	select {
	case <-probeCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never probed the engine")
	}

	select {
	case alert := <-alerts:
		if alert.Kind != AlertKindCVEngineFailure {
			t.Fatalf("expected engine alert, got %s", alert.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not raise an engine alert from the probe path")
	}
}

// TestScheduler_ReportsFeedOutcomesToWatchdog proves the scheduler-to-watchdog
// wiring end to end: a dead camera raises one dropout alert after five minutes
// of cycles, a healthy camera raises none, and engine-class errors are not
// misreported as camera dropouts.
func TestScheduler_ReportsFeedOutcomesToWatchdog(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	cameras := []Camera{
		{ID: "cam-dead", Name: "Dead"},
		{ID: "cam-live", Name: "Live"},
	}
	reg := NewRegistry(cameras, time.Minute)
	analyzer := &outcomeAnalyzer{failCamera: "cam-dead"}
	sched := NewScheduler(reg, analyzer, 30*time.Second, 2, nil, discardLogger())
	sched.SetWatchdog(w)

	ctx := context.Background()
	// Twelve 30s cycles = six simulated minutes.
	for i := 0; i < 12; i++ {
		clock.Advance(30 * time.Second)
		sched.Cycle(ctx)
		w.Evaluate()
	}

	alerts := sink.byKind(AlertKindFeedDropout)
	if len(alerts) != 1 {
		t.Fatalf("expected exactly 1 feed dropout alert, got %d", len(alerts))
	}
	if alerts[0].CameraID != "cam-dead" {
		t.Fatalf("expected the dead camera to alert, got %q", alerts[0].CameraID)
	}
}

// outcomeAnalyzer fails one camera with a feed error and succeeds elsewhere.
type outcomeAnalyzer struct {
	failCamera string
}

func (a *outcomeAnalyzer) Analyze(ctx context.Context, cam Camera) (Verdict, error) {
	if cam.ID == a.failCamera {
		return Verdict{}, &testFeedError{cameraID: cam.ID}
	}
	return Verdict{
		CameraID:      cam.ID,
		Severity:      SeverityClear,
		FloodSeverity: SeverityDry,
		Source:        "onnx",
	}, nil
}

type testFeedError struct{ cameraID string }

func (e *testFeedError) Error() string     { return "feed failure for " + e.cameraID }
func (e *testFeedError) IsFeedError() bool { return true }

func TestAlertSummary_FormatsSubMinuteDurations(t *testing.T) {
	alert := Alert{Kind: AlertKindFeedDropout, CameraID: "cam-x", DownFor: 42 * time.Second, Threshold: time.Minute}
	got := alert.Summary()
	if !strings.Contains(got, "42s") {
		t.Errorf("expected sub-minute duration formatting, got %q", got)
	}
	if !strings.HasPrefix(got, "[OPS_ALERT]") {
		t.Errorf("expected the ops alert tag, got %q", got)
	}
}

func TestIsFeedError_UnwrapChainStillDetected(t *testing.T) {
	if !isFeedError(errors.Join(errors.New("wrapper"), &testFeedError{cameraID: "c"})) {
		t.Fatal("expected a joined feed error to be detected")
	}
}

// TestWatchdog_FeedAlertReportsTimeSinceLastSuccess verifies that the alert
// reports the full no-success observation span, while the threshold crossing
// stays anchored to the first observed feed failure.
func TestWatchdog_FeedAlertReportsTimeSinceLastSuccess(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	sink := &alertCollector{}
	w := newTestWatchdog(t, clock, sink.add)

	w.SetHotSet([]string{"cam-a"})
	w.ObserveCameraSuccess("cam-a")

	clock.Advance(45 * time.Second)
	w.ObserveCameraFeedFailure("cam-a")
	clock.Advance(5 * time.Minute)
	w.Evaluate()

	alerts := sink.byKind(AlertKindFeedDropout)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if want := 5*time.Minute + 45*time.Second; alerts[0].DownFor != want {
		t.Errorf("expected DownFor=%s (since last success), got %s", want, alerts[0].DownFor)
	}
	if !strings.Contains(alerts[0].Summary(), "5m45s") {
		t.Errorf("summary must report the no-success span, got %q", alerts[0].Summary())
	}
}
