package hotset

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Operational alert defaults (S14-T1, Issue #441 scope 4).
//
// A five-minute window is the operator-facing promise: a camera that has not
// produced a successful observation for five minutes is not merely late, it is
// an incident, and so is an inference engine that has not answered for five
// minutes. Both thresholds are configuration (OPS_ALERT_* environment
// variables) rather than constants so a deployment can tighten them without a
// rebuild.
const (
	// DefaultFeedDropoutThreshold is how long a camera in the Hot Set may go
	// without a successful observation before a dropout alert is raised.
	DefaultFeedDropoutThreshold = 5 * time.Minute
	// DefaultCVEngineFailureThreshold is how long the inference engine may stay
	// unhealthy before an engine-failure alert is raised.
	DefaultCVEngineFailureThreshold = 5 * time.Minute
	// DefaultWatchdogEvaluateInterval is how often the watchdog re-examines
	// liveness on its own ticker. It is deliberately independent of the analysis
	// cycle: alerting must not stop because the analysis loop itself is stuck.
	DefaultWatchdogEvaluateInterval = 30 * time.Second
	// defaultEngineProbeTimeout bounds one engine health probe so a hung engine
	// cannot stall the watchdog.
	defaultEngineProbeTimeout = 5 * time.Second
)

// Operational alert kinds. They are stable identifiers, safe to match in log
// rules, dashboards and paging integrations.
const (
	// AlertKindFeedDropout reports a camera in the Hot Set that stopped
	// producing successful observations.
	AlertKindFeedDropout = "camera_feed_dropout"
	// AlertKindCVEngineFailure reports a continuously unhealthy inference
	// engine. Engine errors are system-wide, never attributed to one camera.
	AlertKindCVEngineFailure = "cv_engine_failure"
)

// Alert is one operational incident edge emitted by the Watchdog. Alerts are
// edge-triggered: one alert per outage episode, re-armed only after recovery,
// so an operator is paged once per incident instead of once per poll.
type Alert struct {
	// Kind is AlertKindFeedDropout or AlertKindCVEngineFailure.
	Kind string
	// CameraID is set for feed dropouts and empty for engine failures.
	CameraID string
	// DownFor is how long the dependency had been down when the alert fired.
	DownFor time.Duration
	// Threshold is the configured outage limit that was crossed.
	Threshold time.Duration
	// Detail carries the probe's explanation for an engine failure.
	Detail string
	// At is the watchdog clock instant the alert was raised.
	At time.Time
}

// Summary renders the single-line operational message. Every alert begins with
// the [OPS_ALERT] tag so one stable prefix can drive log-based paging rules and
// on-call runbooks.
func (a Alert) Summary() string {
	switch a.Kind {
	case AlertKindCVEngineFailure:
		msg := fmt.Sprintf("[OPS_ALERT] CV engine failure exceeding %s (unhealthy for %s)",
			formatAlertDuration(a.Threshold), formatAlertDuration(a.DownFor))
		if a.Detail != "" {
			msg += ": " + a.Detail
		}
		return msg
	case AlertKindFeedDropout:
		return fmt.Sprintf("[OPS_ALERT] camera %s feed dropout exceeding %s (no successful observation for %s)",
			a.CameraID, formatAlertDuration(a.Threshold), formatAlertDuration(a.DownFor))
	default:
		return fmt.Sprintf("[OPS_ALERT] %s on %s for %s", a.Kind, a.CameraID, formatAlertDuration(a.DownFor))
	}
}

// formatAlertDuration renders a duration for humans: 42s, 5m, 12m07s. It exists
// so the alert text says "exceeding 5m" rather than "exceeding 5m0s".
func formatAlertDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	minutes := int(d / time.Minute)
	seconds := int(d/time.Second) % 60
	if seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

// EngineProbe reports whether the downstream inference engine can serve
// inference right now, plus a human-readable reason when it cannot. cmd/server
// adapts the same cv-engine health check that backs /health, so the alert and
// the health endpoint can never disagree about what "unhealthy" means.
//
// A probe must respect ctx; the watchdog bounds it with its probe timeout.
type EngineProbe func(ctx context.Context) (healthy bool, detail string)

// WatchdogOption configures a Watchdog.
type WatchdogOption func(*Watchdog)

// WithFeedDropoutThreshold overrides DefaultFeedDropoutThreshold. Values <= 0
// are ignored so a misconfigured deployment keeps the safe default.
func WithFeedDropoutThreshold(d time.Duration) WatchdogOption {
	return func(w *Watchdog) {
		if d > 0 {
			w.dropoutThreshold = d
		}
	}
}

// WithCVEngineFailureThreshold overrides DefaultCVEngineFailureThreshold.
// Values <= 0 are ignored.
func WithCVEngineFailureThreshold(d time.Duration) WatchdogOption {
	return func(w *Watchdog) {
		if d > 0 {
			w.engineThreshold = d
		}
	}
}

// WithWatchdogEvaluateInterval overrides DefaultWatchdogEvaluateInterval.
// Values <= 0 are ignored (a non-positive ticker interval would panic). Short
// intervals are honoured so tests can exercise the probe path without waiting;
// production deployments should keep the 30s default.
func WithWatchdogEvaluateInterval(d time.Duration) WatchdogOption {
	return func(w *Watchdog) {
		if d > 0 {
			w.evaluateInterval = d
		}
	}
}

// WithWatchdogClock injects the clock. It exists for deterministic simulations
// and tests that advance 24 hours of liveness state without sleeping.
func WithWatchdogClock(now func() time.Time) WatchdogOption {
	return func(w *Watchdog) {
		if now != nil {
			w.now = now
		}
	}
}

// WithWatchdogLogger sets the logger used for alert and recovery records.
func WithWatchdogLogger(log *slog.Logger) WatchdogOption {
	return func(w *Watchdog) {
		if log != nil {
			w.log = log
		}
	}
}

// WithAlertSink registers a delivery hook for operational alerts, for example a
// pager, an internal chat webhook, or an ops-only Telegram channel. The
// watchdog logs every alert regardless of the sink, so configuring a sink adds
// a delivery path and never replaces the log record.
//
// The sink is called on the watchdog goroutine after the state lock is
// released, so it may block briefly but must not call back into the watchdog.
func WithAlertSink(sink func(Alert)) WatchdogOption {
	return func(w *Watchdog) {
		if sink != nil {
			w.sink = sink
		}
	}
}

// feedLiveness is the per-camera liveness state.
type feedLiveness struct {
	// feedDownSince is when the current run of feed failures began; zero means
	// the camera is healthy or has not been observed yet. Alert timing is driven
	// by this field, never by the absence of a success: only an observed feed
	// failure is evidence that the feed itself is down.
	feedDownSince time.Time
	// lastSuccess is the timestamp of the most recent successful observation, or
	// zero if there has not been one yet. It refines the reported outage length
	// without changing when the alert fires.
	lastSuccess time.Time
	// alerted is true once this outage episode has been reported. It suppresses
	// repeat alerts until a success re-arms the camera.
	alerted bool
}

// engineLiveness is the inference-engine liveness state. Engine failures are
// system-wide, so they are tracked once, never per camera.
type engineLiveness struct {
	down      bool
	downSince time.Time
	alerted   bool
	detail    string
}

// Watchdog turns per-camera observation outcomes and inference-engine health
// probes into edge-triggered operational alerts (S14-T1, Issue #441).
//
// Design notes:
//
//   - Feed liveness is driven by observation outcomes, not by a wall-clock
//     timer over "last success". A camera is only considered down once the
//     system has actually failed to reach its feed; a paused analysis loop or a
//     budget/breaker short-circuit has no feed evidence and therefore cannot
//     manufacture a camera alert.
//   - Engine health comes from an explicit probe, never from an analysis error.
//     A camera feed failure and an engine outage are different blast radii
//     (see docs/lessons/441-feed-error-breaker-isolation-and-health-observability.md)
//     and must not be conflated in alerting either.
//   - Evaluate runs on its own ticker inside Run, so alerting survives a stuck
//     scheduler cycle.
//
// All methods are safe for concurrent use.
type Watchdog struct {
	mu sync.Mutex

	dropoutThreshold time.Duration
	engineThreshold  time.Duration
	evaluateInterval time.Duration
	probeTimeout     time.Duration

	now  func() time.Time
	log  *slog.Logger
	sink func(Alert)

	cameras map[string]*feedLiveness
	engine  engineLiveness
	probe   EngineProbe
}

// NewWatchdog constructs a Watchdog with the operational defaults.
func NewWatchdog(opts ...WatchdogOption) *Watchdog {
	w := &Watchdog{
		dropoutThreshold: DefaultFeedDropoutThreshold,
		engineThreshold:  DefaultCVEngineFailureThreshold,
		evaluateInterval: DefaultWatchdogEvaluateInterval,
		probeTimeout:     defaultEngineProbeTimeout,
		now:              time.Now,
		log:              slog.Default(),
		cameras:          make(map[string]*feedLiveness),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// SetEngineProbe wires the inference-engine liveness probe. Without a probe the
// watchdog still tracks camera feeds and simply never raises an engine alert,
// which is the correct behaviour for deployments with no downstream engine
// (ANALYZER=none, mock).
func (w *Watchdog) SetEngineProbe(probe EngineProbe) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.probe = probe
	w.mu.Unlock()
}

// SetEngineProbeTimeout overrides the per-probe timeout. Values <= 0 are
// ignored.
func (w *Watchdog) SetEngineProbeTimeout(d time.Duration) {
	if w == nil || d <= 0 {
		return
	}
	w.mu.Lock()
	w.probeTimeout = d
	w.mu.Unlock()
}

// SetHotSet reconciles the tracked camera set with the current Hot Set. Cameras
// that joined get fresh state; cameras that left are dropped so a long-lived
// process does not accumulate liveness state for cameras nobody watches.
func (w *Watchdog) SetHotSet(cameraIDs []string) {
	if w == nil {
		return
	}
	present := make(map[string]struct{}, len(cameraIDs))
	for _, id := range cameraIDs {
		if id == "" {
			continue
		}
		present[id] = struct{}{}
	}

	w.mu.Lock()
	for id := range present {
		if _, ok := w.cameras[id]; !ok {
			w.cameras[id] = &feedLiveness{}
		}
	}
	for id := range w.cameras {
		if _, ok := present[id]; !ok {
			delete(w.cameras, id)
		}
	}
	w.mu.Unlock()
}

// ObserveCameraSuccess records a successful observation for cameraID. It ends
// an outage episode and re-arms the camera's alert.
func (w *Watchdog) ObserveCameraSuccess(cameraID string) {
	if w == nil || cameraID == "" {
		return
	}
	now := w.now()

	w.mu.Lock()
	st, ok := w.cameras[cameraID]
	if !ok {
		st = &feedLiveness{}
		w.cameras[cameraID] = st
	}
	recovered := st.alerted
	outage := time.Duration(0)
	if recovered && !st.feedDownSince.IsZero() {
		outage = now.Sub(st.feedDownSince)
	}
	st.lastSuccess = now
	st.feedDownSince = time.Time{}
	st.alerted = false
	w.mu.Unlock()

	if recovered {
		w.log.Info("[OPS_ALERT] recovered: camera feed restored",
			"kind", AlertKindFeedDropout,
			"camera", cameraID,
			"outage_duration", outage.Round(time.Second).String(),
		)
	}
}

// ObserveCameraFeedFailure records that cameraID's feed could not be reached.
// Only a genuine feed failure may call this; engine-class errors are system
// failures and must go through ObserveEngineHealth instead.
func (w *Watchdog) ObserveCameraFeedFailure(cameraID string) {
	if w == nil || cameraID == "" {
		return
	}
	now := w.now()

	w.mu.Lock()
	st, ok := w.cameras[cameraID]
	if !ok {
		st = &feedLiveness{}
		w.cameras[cameraID] = st
	}
	if st.feedDownSince.IsZero() {
		st.feedDownSince = now
	}
	w.mu.Unlock()
}

// ObserveEngineHealth records one engine probe result. Consecutive unhealthy
// results extend one outage episode; a healthy result ends it and re-arms the
// engine alert.
func (w *Watchdog) ObserveEngineHealth(healthy bool, detail string) {
	if w == nil {
		return
	}
	now := w.now()

	var recovered bool
	var downFor time.Duration

	w.mu.Lock()
	e := &w.engine
	if healthy {
		if e.alerted {
			recovered = true
			downFor = now.Sub(e.downSince)
		}
		e.down = false
		e.downSince = time.Time{}
		e.alerted = false
		e.detail = ""
	} else {
		if e.downSince.IsZero() {
			e.downSince = now
		}
		e.down = true
		e.detail = detail
	}
	w.mu.Unlock()

	if recovered {
		w.log.Info("[OPS_ALERT] recovered: CV engine healthy again",
			"kind", AlertKindCVEngineFailure,
			"outage_duration", downFor.Round(time.Second).String(),
		)
	}
}

// Evaluate compares the tracked liveness state with the configured thresholds
// and emits every alert whose episode has crossed its limit. Alerts are
// edge-triggered, so calling Evaluate on every tick reports each incident once.
func (w *Watchdog) Evaluate() {
	if w == nil {
		return
	}
	now := w.now()
	alerts := make([]Alert, 0, 4)

	w.mu.Lock()
	for id, st := range w.cameras {
		if st.alerted || st.feedDownSince.IsZero() {
			continue
		}
		downFor := now.Sub(st.feedDownSince)
		if downFor < w.dropoutThreshold {
			continue
		}
		st.alerted = true
		// Report the true no-successful-observation span when a prior success is
		// known; it is at least as long as the observed failure run.
		reported := downFor
		if !st.lastSuccess.IsZero() && now.Sub(st.lastSuccess) > reported {
			reported = now.Sub(st.lastSuccess)
		}
		alerts = append(alerts, Alert{
			Kind:      AlertKindFeedDropout,
			CameraID:  id,
			DownFor:   reported,
			Threshold: w.dropoutThreshold,
			At:        now,
		})
	}

	if e := &w.engine; !e.alerted && e.down {
		downFor := now.Sub(e.downSince)
		if downFor >= w.engineThreshold {
			e.alerted = true
			alerts = append(alerts, Alert{
				Kind:      AlertKindCVEngineFailure,
				DownFor:   downFor,
				Threshold: w.engineThreshold,
				Detail:    e.detail,
				At:        now,
			})
		}
	}
	w.mu.Unlock()

	// Deterministic order keeps logs and pager feeds stable across runs and
	// makes assertions in tests meaningful.
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Kind != alerts[j].Kind {
			return alerts[i].Kind < alerts[j].Kind
		}
		return alerts[i].CameraID < alerts[j].CameraID
	})

	for _, alert := range alerts {
		w.dispatch(alert)
	}
}

// dispatch records an alert exactly once and forwards it to the sink, if any.
func (w *Watchdog) dispatch(alert Alert) {
	w.log.Error(alert.Summary(),
		"kind", alert.Kind,
		"camera", alert.CameraID,
		"down_for_seconds", int(alert.DownFor/time.Second),
		"threshold_seconds", int(alert.Threshold/time.Second),
	)

	w.mu.Lock()
	sink := w.sink
	w.mu.Unlock()
	if sink != nil {
		sink(alert)
	}
}

// probeEngine runs the configured engine probe once, bounded by the probe
// timeout. A probe that times out counts as unhealthy: an engine that cannot
// answer a health check cannot serve inference either.
func (w *Watchdog) probeEngine(ctx context.Context) {
	w.mu.Lock()
	probe := w.probe
	timeout := w.probeTimeout
	w.mu.Unlock()
	if probe == nil {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	healthy, detail := probe(probeCtx)
	if probeCtx.Err() != nil && healthy {
		// A probe must not report healthy after its own deadline expired; the
		// caller's context deadline is evidence the engine did not answer.
		healthy = false
		if detail == "" {
			detail = "health probe timed out"
		}
	}
	w.ObserveEngineHealth(healthy, detail)
}

// Run blocks until ctx is cancelled, probing engine health and evaluating
// liveness on the configured interval. Start it once from main in its own
// goroutine: `go watchdog.Run(ctx)`.
func (w *Watchdog) Run(ctx context.Context) {
	if w == nil {
		return
	}

	w.mu.Lock()
	interval := w.evaluateInterval
	droppedThreshold := w.dropoutThreshold
	engineThreshold := w.engineThreshold
	hasProbe := w.probe != nil
	w.mu.Unlock()

	w.log.Info("[OPS] operational watchdog started",
		"feed_dropout_threshold", droppedThreshold.String(),
		"cv_engine_failure_threshold", engineThreshold.String(),
		"evaluate_interval", interval.String(),
		"engine_probe_configured", hasProbe,
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("[OPS] operational watchdog stopping")
			return
		case <-ticker.C:
			w.probeEngine(ctx)
			w.Evaluate()
		}
	}
}
