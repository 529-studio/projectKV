// Package hotset implements the Hot Set Pool scheduler described in
// docs/SYSTEM_DESIGN.md: inference/analysis runs on a fixed schedule against
// a small, bounded set of cameras (actively viewed + pinned + fixed critical
// choke points), completely decoupled from how many users are looking at
// any given camera. Whether 1 person or 100,000 people view a camera,
// analysis for that camera still runs exactly once per interval.
package hotset

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Camera is the minimal identity needed to schedule analysis for a feed.
// SnapshotURL points at the already-cached proxy endpoint (services/node-server
// or its Go successor), never directly at the upstream DOT portal.
type Camera struct {
	ID          string
	Name        string
	SnapshotURL string
	// Zone is used later by the notification layer to route alerts to
	// subscribers of that geographic area (see internal/notify).
	Zone string
}

// Analyzer is the pluggable adapter interface. Concrete implementations:
// mock (Phase 1 testing), Cloud Vision API (Gemini/GPT-4V, Phase 1 flood MVP),
// self-hosted ONNX/YOLO via the Python cv-engine service (Phase 2).
// This mirrors the "Pluggable Adapter Pattern" in SYSTEM_DESIGN.md so the
// scheduler and API contract never change as the backend model evolves.
type Analyzer interface {
	// Analyze fetches/receives the current snapshot for cam and returns a
	// verdict. Implementations must respect ctx cancellation/deadline.
	Analyze(ctx context.Context, cam Camera) (Verdict, error)
}

// Verdict is intentionally generic so the same scheduler serves both traffic
// congestion analysis and flood detection — only the Analyzer implementation
// and the meaning of Severity differ.
type Verdict struct {
	CameraID   string    `json:"camera_id"`
	Severity   string    `json:"severity"` // e.g. "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED", or "DRY" | "WET" | "FLOODED_SHALLOW" | "FLOODED_DEEP"
	Confidence float64   `json:"confidence"`
	Source     string    `json:"source"` // "mock" | "vlm" | "onnx"
	ObservedAt time.Time `json:"observed_at"`
	Stale      bool      `json:"stale"`
	Timestamp  time.Time `json:"timestamp"`
}

// OnVerdict is called for every successful analysis result. The caller
// (main wiring) is expected to fan this out to the state store and, when a
// verdict crosses an alert threshold, to the notification service.
type OnVerdict func(Verdict)

// Registry tracks which cameras currently belong to the Hot Set: cameras
// with active viewers, pinned cameras, plus a fixed list of critical points
// (e.g. Cat Lai, My Thuy, An Suong, and known flood hotspots). It is the
// caller's responsibility to call Touch whenever a client views or pins a
// camera; entries expire from the "active viewer" bucket automatically.
type Registry struct {
	mu        sync.Mutex
	fixed     map[string]Camera // never expires: critical choke points / flood hotspots
	active    map[string]activeEntry
	activeTTL time.Duration
}

type activeEntry struct {
	cam       Camera
	expiresAt time.Time
}

func NewRegistry(fixed []Camera, activeTTL time.Duration) *Registry {
	f := make(map[string]Camera, len(fixed))
	for _, c := range fixed {
		f[c.ID] = c
	}
	return &Registry{
		fixed:     f,
		active:    make(map[string]activeEntry),
		activeTTL: activeTTL,
	}
}

// Touch marks cam as actively viewed/pinned, extending its presence in the
// Hot Set for activeTTL from now. Call this from the HTTP handler that
// serves a camera snapshot/view request, and separately when a camera is
// pinned to the dock (pins should probably use a longer TTL than casual
// views — pass a Registry per bucket, or extend this type with two TTLs,
// if that distinction matters for your traffic pattern).
func (r *Registry) Touch(cam Camera) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[cam.ID] = activeEntry{cam: cam, expiresAt: time.Now().Add(r.activeTTL)}
}

// Snapshot returns the current Hot Set: all fixed cameras plus any active
// camera whose TTL has not yet expired. Expired entries are pruned here.
func (r *Registry) Snapshot() []Camera {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	out := make([]Camera, 0, len(r.fixed)+len(r.active))
	for _, c := range r.fixed {
		out = append(out, c)
	}
	for id, e := range r.active {
		if now.After(e.expiresAt) {
			delete(r.active, id)
			continue
		}
		if _, isFixed := r.fixed[id]; isFixed {
			continue // avoid double-counting
		}
		out = append(out, e.cam)
	}
	return out
}

// Scheduler runs Analyzer.Analyze against the current Hot Set on a fixed
// interval, with a bounded worker pool so a slow upstream (Cloud Vision API,
// or an overloaded cv-engine) cannot cause unbounded goroutine growth.
type Scheduler struct {
	registry    *Registry
	analyzer    Analyzer
	interval    time.Duration
	maxParallel int
	onVerdict   OnVerdict
	budget      *AnalysisBudget
	log         *slog.Logger
}

func NewScheduler(registry *Registry, analyzer Analyzer, interval time.Duration, maxParallel int, onVerdict OnVerdict, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		registry:    registry,
		analyzer:    analyzer,
		interval:    interval,
		maxParallel: maxParallel,
		onVerdict:   onVerdict,
		log:         log,
	}
}

// SetBudget wires an optional daily analyzer budget. When the budget is
// exhausted the scheduler degrades to UNKNOWN verdicts without invoking the
// analyzer.
func (s *Scheduler) SetBudget(b *AnalysisBudget) {
	s.budget = b
}

// Run blocks until ctx is cancelled. Intended to be started once from main
// in its own goroutine: `go scheduler.Run(ctx)`.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("hotset scheduler stopping")
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	cameras := s.registry.Snapshot()
	if len(cameras) == 0 {
		return
	}

	sem := make(chan struct{}, s.maxParallel)
	var wg sync.WaitGroup

	for _, cam := range cameras {
		cam := cam
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// Per-camera timeout so one stuck upstream call cannot delay
			// the entire cycle or pile up across ticks.
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if s.budget != nil && !s.budget.Allow() {
				if s.onVerdict != nil {
					now := time.Now()
					s.onVerdict(Verdict{
						CameraID:   cam.ID,
						Severity:   SeverityUnknown,
						Confidence: 0,
						Source:     "",
						ObservedAt: now,
						Stale:      false,
						Timestamp:  now,
					})
				}
				return
			}

			verdict, err := s.analyzer.Analyze(callCtx, cam)
			if err != nil {
				s.log.Warn("analysis failed", "camera", cam.ID, "error", err)
				return
			}
			if verdict.ObservedAt.IsZero() {
				verdict.ObservedAt = time.Now()
			}
			if verdict.Timestamp.IsZero() {
				verdict.Timestamp = verdict.ObservedAt
			}
			if s.onVerdict != nil {
				s.onVerdict(verdict)
			}
		}()
	}

	wg.Wait()
}
