package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
)

// 24-hour continuous-history and operational-alerting simulation (S14-T1,
// Issue #441 scope 3 and 4).
//
// The simulation drives the production components rather than a model of them:
//
//   - hotset.Scheduler.Cycle runs the real scheduling and circuit-breaker path,
//     once per simulated 30s poll, for 2,880 cycles.
//   - hotset.FrozenDetector and hotset.RecordableObservation are the same gate
//     the production fan-out in buildHotsetOnVerdict uses.
//   - hotset.Watchdog is the production alerter, fed by the real scheduler.
//   - store.MeasureBinCoverage is the same bin coverage the /api/v1/hotset/
//     history/coverage endpoint reports (ADR-012 D2).
//
// Only the clock, the camera feeds and the inference engine are simulated.
const (
	simCameraCount = 59
	simDeadCameras = 3
	simActiveCams  = simCameraCount - simDeadCameras

	simPollInterval = 30 * time.Second
	simWindow       = 24 * time.Hour
	simCoverageBin  = 10 * time.Minute
	simAlertLimit   = 5 * time.Minute

	// simEngineOutageFrom/To model a continuous cv-engine outage (no model
	// loaded / engine unreachable) in the middle of the day.
	simEngineOutageFrom = 12 * time.Hour
	simEngineOutageTo   = 12*time.Hour + 30*time.Minute
)

// simOutage is a feed outage window relative to the start of the simulation.
type simOutage struct {
	from time.Duration
	to   time.Duration
}

// simCamera is one simulated Hot Set camera.
type simCamera struct {
	id string
	// refresh is how often the upstream source republishes a new frame. A
	// value <= simPollInterval means a new frame on every poll.
	refresh time.Duration
	// dead cameras never recover: the three permanently offline feeds the
	// production audit found in the 59-camera Hot Set.
	dead bool
	// outages are windows in which the feed is unreachable.
	outages []simOutage
}

// buildSimCameras constructs the 59-camera Hot Set:
//
//   - 3 permanently dead feeds, placed first in the registry. Before S14-T1 a
//     feed failure was recorded on the shared breaker, so a dead feed analysed
//     before the healthy majority degraded every later camera in the cycle.
//     Placing them first keeps that worst case in the fixture: the test proves
//     the current code still reaches 95% coverage with three such feeds.
//   - 12 slow-refresh feeds (four each at 60s, 75s and 90s): sources that
//     legitimately repeat a frame for two or three 30s polls.
//   - 2 transient dropouts of 7 and 12 minutes, both longer than the 5-minute
//     alerting threshold.
//   - 42 normal feeds refreshed on every poll.
func buildSimCameras() []simCamera {
	specs := make([]simCamera, 0, simCameraCount)

	for i := 1; i <= simDeadCameras; i++ {
		specs = append(specs, simCamera{id: fmt.Sprintf("cam-dead-%d", i), dead: true})
	}

	for _, refresh := range []time.Duration{60 * time.Second, 75 * time.Second, 90 * time.Second} {
		for i := 1; i <= 4; i++ {
			specs = append(specs,
				simCamera{id: fmt.Sprintf("cam-slow-%ds-%d", int(refresh/time.Second), i), refresh: refresh})
		}
	}

	specs = append(specs,
		simCamera{id: "cam-outage-1", outages: []simOutage{{from: 10 * time.Hour, to: 10*time.Hour + 7*time.Minute}}},
		simCamera{id: "cam-outage-2", outages: []simOutage{{from: 15 * time.Hour, to: 15*time.Hour + 12*time.Minute}}},
	)

	remaining := simCameraCount - len(specs)
	for i := 1; i <= remaining; i++ {
		specs = append(specs, simCamera{id: fmt.Sprintf("cam-normal-%02d", i)})
	}

	return specs
}

// simWorld is the simulated environment: a controllable clock, the camera feeds
// and the inference engine.
type simWorld struct {
	start time.Time
	specs []simCamera
	byID  map[string]simCamera

	mu  sync.RWMutex
	now time.Time
}

func newSimWorld(start time.Time, specs []simCamera) *simWorld {
	byID := make(map[string]simCamera, len(specs))
	for _, s := range specs {
		byID[s.id] = s
	}
	return &simWorld{start: start, specs: specs, byID: byID, now: start}
}

func (w *simWorld) Now() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.now
}

func (w *simWorld) SetNow(t time.Time) {
	w.mu.Lock()
	w.now = t
	w.mu.Unlock()
}

// feedUp reports whether the camera's upstream is reachable at t.
func (w *simWorld) feedUp(spec simCamera, at time.Time) bool {
	if spec.dead {
		return false
	}
	elapsed := at.Sub(w.start)
	for _, o := range spec.outages {
		if elapsed >= o.from && elapsed < o.to {
			return false
		}
	}
	return true
}

// frame returns the JPEG bytes the proxy would serve for spec at t. A feed that
// republishes every `refresh` returns identical bytes until the source changes,
// which is what the FrozenDetector sees in production.
func (w *simWorld) frame(spec simCamera, at time.Time) ([]byte, bool) {
	if !w.feedUp(spec, at) {
		return nil, false
	}
	refresh := spec.refresh
	if refresh <= 0 {
		refresh = simPollInterval
	}
	bucket := int64(at.Sub(w.start) / refresh)
	return []byte(fmt.Sprintf("frame|%s|%d", spec.id, bucket)), true
}

// engineHealthy reports whether the cv-engine can serve inference at t.
func (w *simWorld) engineHealthy(at time.Time) (bool, string) {
	elapsed := at.Sub(w.start)
	if elapsed >= simEngineOutageFrom && elapsed < simEngineOutageTo {
		return false, "model_loaded=false (standby)"
	}
	return true, ""
}

func (w *simWorld) hotSetCameras() []hotset.Camera {
	cams := make([]hotset.Camera, 0, len(w.specs))
	for _, s := range w.specs {
		cams = append(cams, hotset.Camera{
			ID:          s.id,
			Name:        s.id,
			SnapshotURL: "sim://" + s.id + "/snapshot",
		})
	}
	return cams
}

// simAnalyzer implements hotset.Analyzer over the simulated world.
type simAnalyzer struct{ world *simWorld }

func (a *simAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	spec, ok := a.world.byID[cam.ID]
	if !ok {
		return hotset.Verdict{}, fmt.Errorf("sim: unknown camera %s", cam.ID)
	}
	now := a.world.Now()
	if !a.world.feedUp(spec, now) {
		return hotset.Verdict{}, &cvclient.FeedError{
			CameraID: cam.ID,
			URL:      cam.SnapshotURL,
			Err:      errors.New("snapshot unreachable"),
		}
	}
	if healthy, detail := a.world.engineHealthy(now); !healthy {
		return hotset.Verdict{}, fmt.Errorf("cv-engine /predict: %s", detail)
	}
	return hotset.Verdict{
		CameraID:        cam.ID,
		Severity:        hotset.SeverityModerate,
		FloodSeverity:   hotset.SeverityDry,
		Confidence:      0.9,
		Source:          "onnx",
		PCUTotal:        18.5,
		SaturationIndex: 0.42,
		ObservedAt:      now,
		Timestamp:       now,
	}, nil
}

// simRecorder captures the observations that would be appended to
// hotset_verdict_history.
type simRecorder struct {
	mu       sync.Mutex
	byCamera map[string][]time.Time
}

func newSimRecorder() *simRecorder {
	return &simRecorder{byCamera: make(map[string][]time.Time)}
}

func (r *simRecorder) record(cameraID string, at time.Time) {
	r.mu.Lock()
	r.byCamera[cameraID] = append(r.byCamera[cameraID], at)
	r.mu.Unlock()
}

func (r *simRecorder) timestamps(cameraID string) []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Time, len(r.byCamera[cameraID]))
	copy(out, r.byCamera[cameraID])
	return out
}

// simAlertCollector is a thread-safe sink for watchdog alerts.
type simAlertCollector struct {
	mu     sync.Mutex
	alerts []hotset.Alert
}

func (c *simAlertCollector) add(a hotset.Alert) {
	c.mu.Lock()
	c.alerts = append(c.alerts, a)
	c.mu.Unlock()
}

func (c *simAlertCollector) all() []hotset.Alert {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]hotset.Alert, len(c.alerts))
	copy(out, c.alerts)
	return out
}

func (c *simAlertCollector) byKind(kind string) []hotset.Alert {
	var out []hotset.Alert
	for _, a := range c.all() {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

type simRunConfig struct {
	start time.Time
	// frozenThreshold is the number of consecutive identical frames before a
	// feed is considered frozen. hotset.DefaultFrozenThreshold is the production
	// value; 3 reproduces the pre-S14-T1 default.
	frozenThreshold int
}

type simResult struct {
	coverage      map[string]store.BinCoverage
	observations  map[string]int
	alerts        []hotset.Alert
	totalRecorded int
	expectedPolls int
}

// runHotSetSimulation executes one full 24h simulated day.
func runHotSetSimulation(t *testing.T, cfg simRunConfig) simResult {
	t.Helper()

	specs := buildSimCameras()
	world := newSimWorld(cfg.start, specs)
	rec := newSimRecorder()
	frozen := hotset.NewFrozenDetector(cfg.frozenThreshold)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// The fan-out mirrors cmd/server buildHotsetOnVerdict: apply the frozen-frame
	// check to the frame the proxy served, then record only what the shared
	// production gate accepts.
	onVerdict := func(v hotset.Verdict) {
		if v.Source == "onnx" {
			if spec, ok := world.byID[v.CameraID]; ok {
				if frame, ok := world.frame(spec, v.ObservedAt); ok {
					if frozen.CheckBytes(v.CameraID, frame) {
						v.Stale = true
					}
				}
			}
		}
		if hotset.RecordableObservation(v) {
			rec.record(v.CameraID, v.ObservedAt)
		}
	}

	sched := hotset.NewScheduler(
		hotset.NewRegistry(world.hotSetCameras(), 2*time.Minute),
		&simAnalyzer{world: world},
		simPollInterval,
		16,
		onVerdict,
		logger,
	)
	// The breaker cooldown must advance on the simulated clock; otherwise a
	// 30-minute engine outage would leave it open for the rest of the run
	// because the whole day executes in a few seconds of real time.
	sched.SetCircuitBreaker(hotset.NewCircuitBreakerWithClock(3, 15*time.Second, world.Now))

	alerts := &simAlertCollector{}
	watchdog := hotset.NewWatchdog(
		hotset.WithFeedDropoutThreshold(simAlertLimit),
		hotset.WithCVEngineFailureThreshold(simAlertLimit),
		hotset.WithWatchdogClock(world.Now),
		hotset.WithWatchdogLogger(logger),
		hotset.WithAlertSink(alerts.add),
	)
	sched.SetWatchdog(watchdog)

	ctx := context.Background()
	for elapsed := time.Duration(0); elapsed < simWindow; elapsed += simPollInterval {
		world.SetNow(cfg.start.Add(elapsed))
		healthy, detail := world.engineHealthy(world.Now())
		watchdog.ObserveEngineHealth(healthy, detail)
		sched.Cycle(ctx)
		watchdog.Evaluate()
	}

	res := simResult{
		coverage:      make(map[string]store.BinCoverage, simActiveCams),
		observations:  make(map[string]int, simActiveCams),
		alerts:        alerts.all(),
		expectedPolls: int(simWindow / simPollInterval),
	}
	for _, spec := range specs {
		if spec.dead {
			continue
		}
		ts := rec.timestamps(spec.id)
		res.coverage[spec.id] = store.MeasureBinCoverage(ts, cfg.start, simWindow, simCoverageBin)
		res.observations[spec.id] = len(ts)
		res.totalRecorded += len(ts)
	}
	return res
}

func (r simResult) meanCoverage() float64 {
	if len(r.coverage) == 0 {
		return 0
	}
	sum := 0.0
	for _, cov := range r.coverage {
		sum += cov.Ratio()
	}
	return sum / float64(len(r.coverage))
}

func (r simResult) minCoverage() (string, float64) {
	worstID, worst := "", 1.0
	for id, cov := range r.coverage {
		if ratio := cov.Ratio(); ratio < worst {
			worstID, worst = id, ratio
		}
	}
	return worstID, worst
}

func (r simResult) observationRatio(cameraID string) float64 {
	if r.expectedPolls == 0 {
		return 0
	}
	return float64(r.observations[cameraID]) / float64(r.expectedPolls)
}

// TestHotSet24hCoverageAndOperationalAlerting is the S14-T1 / #441 evidence
// test: one simulated day of the 59-camera Hot Set with 3 dead feeds, 12
// slow-refresh feeds, 2 multi-minute dropouts and a 30-minute cv-engine outage.
func TestHotSet24hCoverageAndOperationalAlerting(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	fixed := runHotSetSimulation(t, simRunConfig{
		start:           start,
		frozenThreshold: hotset.DefaultFrozenThreshold,
	})

	t.Logf("active Hot Set cameras: %d, cycles: %d, recorded observations: %d",
		simActiveCams, fixed.expectedPolls, fixed.totalRecorded)

	t.Run("a) continuous 24h history coverage >= 95% on active cameras", func(t *testing.T) {
		mean := fixed.meanCoverage()
		worstID, worst := fixed.minCoverage()
		t.Logf("mean 24h bin coverage: %.4f (%.2f%%), worst camera %s: %.4f",
			mean, mean*100, worstID, worst)

		if mean < 0.95 {
			t.Fatalf("mean 24h coverage %.4f is below the 95%% requirement", mean)
		}
		if worst < 0.95 {
			t.Fatalf("camera %s coverage %.4f is below the 95%% requirement", worstID, worst)
		}
		if len(fixed.coverage) != simActiveCams {
			t.Fatalf("expected %d active cameras in the coverage set, got %d", simActiveCams, len(fixed.coverage))
		}
		// The 3 dead cameras must contribute zero history, not fake observations.
		for _, spec := range buildSimCameras() {
			if !spec.dead {
				continue
			}
			if got := fixed.observations[spec.id]; got != 0 {
				t.Errorf("dead camera %s recorded %d observations, want 0", spec.id, got)
			}
		}
	})

	t.Run("b) feeds down longer than 5m trigger exactly one dropout alert", func(t *testing.T) {
		feedAlerts := alertsOfKind(fixed.alerts, hotset.AlertKindFeedDropout)

		// One episode per dead camera, plus one per transient outage. Every
		// other camera must stay silent.
		want := map[string]bool{
			"cam-dead-1":   true,
			"cam-dead-2":   true,
			"cam-dead-3":   true,
			"cam-outage-1": true,
			"cam-outage-2": true,
		}
		counts := make(map[string]int, len(want))
		for _, a := range feedAlerts {
			counts[a.CameraID]++
			if a.DownFor < simAlertLimit {
				t.Errorf("camera %s alert fired after %s, want >= %s", a.CameraID, a.DownFor, simAlertLimit)
			}
			if !want[a.CameraID] {
				t.Errorf("unexpected dropout alert for healthy camera %s", a.CameraID)
			}
		}
		for id := range want {
			if counts[id] != 1 {
				t.Errorf("camera %s raised %d dropout alerts, want exactly 1 (debounced)", id, counts[id])
			}
		}
		if len(feedAlerts) != len(want) {
			t.Errorf("expected %d dropout alerts in total, got %d", len(want), len(feedAlerts))
		}
		for _, a := range feedAlerts {
			t.Logf("feed alert: %s", a.Summary())
		}
	})

	t.Run("c) continuous cv-engine outage triggers one engine alert", func(t *testing.T) {
		engineAlerts := alertsOfKind(fixed.alerts, hotset.AlertKindCVEngineFailure)
		if len(engineAlerts) != 1 {
			t.Fatalf("expected exactly 1 cv-engine alert for one outage episode, got %d", len(engineAlerts))
		}
		alert := engineAlerts[0]
		if alert.DownFor < simAlertLimit {
			t.Errorf("engine alert fired after %s, want >= %s", alert.DownFor, simAlertLimit)
		}
		if alert.CameraID != "" {
			t.Errorf("engine alert must not be attributed to a camera, got %q", alert.CameraID)
		}
		// It must fire inside the simulated outage window, not before it.
		if alert.At.Before(start.Add(simEngineOutageFrom)) || alert.At.After(start.Add(simEngineOutageTo)) {
			t.Errorf("engine alert fired at %s, outside the simulated outage %s..%s",
				alert.At.Format(time.RFC3339), start.Add(simEngineOutageFrom).Format(time.RFC3339), start.Add(simEngineOutageTo).Format(time.RFC3339))
		}
		t.Logf("engine alert: %s", alert.Summary())
	})

	t.Run("d) slow-refresh feeds are not misread as frozen", func(t *testing.T) {
		// With the production threshold of 10 identical frames (5 minutes at the
		// 30s cadence), a 90s upstream that repeats a frame for three polls is
		// never marked frozen.
		for _, spec := range buildSimCameras() {
			if spec.refresh != 90*time.Second {
				continue
			}
			if got := fixed.observationRatio(spec.id); got < 0.95 {
				t.Errorf("slow feed %s recorded %.2f%% of polls, want >= 95%%", spec.id, got*100)
			}
		}
	})

	t.Run("e) pre-fix frozen threshold silently discards slow-refresh history", func(t *testing.T) {
		// The audit found feeds that never accumulated history because a
		// three-identical-frame threshold marked every third 90s-refresh poll
		// stale, and stale observations are rejected from history. Bin coverage
		// alone hides this: the surviving polls still land in every 10m bin, but
		// a third of the raw series is missing. Measure the observation ratio,
		// which is what the forecast feature store actually consumes.
		legacy := runHotSetSimulation(t, simRunConfig{
			start:           start,
			frozenThreshold: 3, // DefaultFrozenThreshold before S14-T1
		})

		checked := 0
		for _, spec := range buildSimCameras() {
			if spec.refresh != 90*time.Second {
				continue
			}
			checked++
			legacyRatio := legacy.observationRatio(spec.id)
			fixedRatio := fixed.observationRatio(spec.id)
			if legacyRatio >= 0.75 {
				t.Errorf("pre-fix threshold recorded %.2f%% of polls for slow feed %s, want < 75%%", legacyRatio*100, spec.id)
			}
			if fixedRatio < 0.95 {
				t.Errorf("fixed threshold recorded %.2f%% of polls for slow feed %s, want >= 95%%", fixedRatio*100, spec.id)
			}
			t.Logf("slow feed %s: pre-fix %.2f%% -> fixed %.2f%%", spec.id, legacyRatio*100, fixedRatio*100)
		}
		if checked == 0 {
			t.Fatal("simulation has no 90s-refresh feeds to verify")
		}
	})
}

func alertsOfKind(alerts []hotset.Alert, kind string) []hotset.Alert {
	var out []hotset.Alert
	for _, a := range alerts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}
