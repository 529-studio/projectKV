package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
	"github.com/529-studio/projectKV/services/go-server/internal/harvester"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
)

// startProxyCachePurge runs periodic cache purging until ctx is cancelled.
func startProxyCachePurge(ctx context.Context, cache *proxycache.Cache) {
	if cache == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cache.Purge()
			}
		}
	}()
}

// startRetentionPurge executes the 30-day raw data retention cleaner (ADR-012 D2).
// Runs an initial non-blocking purge 15s after boot, then executes every 24 hours.
func startRetentionPurge(ctx context.Context, repo *store.VerdictHistoryRepo) {
	if repo == nil {
		return
	}
	go func() {
		select {
		case <-time.After(15 * time.Second):
			cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
			if deleted, err := repo.PurgeOlderThan(ctx, cutoff, 5000); err != nil {
				log.Printf("[WARN] Initial 30-day retention purge failed: %v", err)
			} else if deleted > 0 {
				log.Printf("[RETENTION] Initial purge cleared %d observations older than 30 days (%s)", deleted, cutoff.Format(time.RFC3339))
			}
		case <-ctx.Done():
			return
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
				if deleted, err := repo.PurgeOlderThan(ctx, cutoff, 5000); err != nil {
					log.Printf("[WARN] Daily 30-day retention purge failed: %v", err)
				} else if deleted > 0 {
					log.Printf("[RETENTION] Daily purge cleared %d observations older than 30 days", deleted)
				}
			}
		}
	}()
}

// startFloodHexReaper runs the periodic reaper for stale flood hex cells (runs every 30s).
func startFloodHexReaper(ctx context.Context, agg *live.FloodHexAggregator) {
	if agg == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleared := agg.ReapStaleCells()
				if len(cleared) > 0 {
					log.Printf("[LIVE] Reaped %d stale flood hex cells", len(cleared))
				}
			}
		}
	}()
}

// preseedHotsetFromStore pre-seeds in-memory hotset store and aggregators with
// latest stored verdicts from SQLite so GET /api/v1/cameras/hotset/status,
// GET /api/flood-hex, and GET /api/traffic-status return cached data immediately
// on cold boot with sub-5ms latency.
func preseedHotsetFromStore(repo *store.VerdictRepo, s *hotset.Store, hexAgg *live.HexAggregator, floodHexAgg *live.FloodHexAggregator, staleTTL time.Duration) {
	if repo == nil || s == nil {
		return
	}
	storedVerdicts, err := repo.GetLatest(context.Background())
	if err != nil || len(storedVerdicts) == 0 {
		return
	}
	if staleTTL <= 0 {
		staleTTL = 180 * time.Second
	}
	now := time.Now()
	for _, sv := range storedVerdicts {
		obs := sv.ObservedAt
		if obs.IsZero() {
			obs = sv.Timestamp
		}
		isStale := false
		if !obs.IsZero() && now.Sub(obs) > staleTTL {
			isStale = true
		}
		hv := hotsetVerdictFromStore(sv)
		hv.ObservedAt = obs
		hv.Stale = isStale
		s.Set(hv)
		if hexAgg != nil {
			hexAgg.OnVerdict(hv)
		}
		if floodHexAgg != nil {
			floodHexAgg.OnVerdict(hv)
		}
	}
	log.Printf("[INIT] Pre-seeded in-memory hotset store and hex aggregators with %d stored verdicts from database", len(storedVerdicts))
}

type hotsetDispatcherParams struct {
	proxyCache         *proxycache.Cache
	frozenDetector     *hotset.FrozenDetector
	hotsetStore        *hotset.Store
	liveHub            *live.Hub
	hexAggregator      *live.HexAggregator
	floodHexAggregator *live.FloodHexAggregator
	dispatchFloodAlert func(hotset.Verdict)
	verdictRepo        *store.VerdictRepo
	verdictHistoryRepo *store.VerdictHistoryRepo
	arch               *archiver.Archiver
	analyzerMode       string
}

// buildHotsetOnVerdict constructs the fan-out callback invoked by hotset.Scheduler
// whenever an analysis cycle completes for a camera.
func buildHotsetOnVerdict(p hotsetDispatcherParams) func(v hotset.Verdict) {
	return func(v hotset.Verdict) {
		now := time.Now()
		if v.ObservedAt.IsZero() {
			v.ObservedAt = now
		}
		if v.Timestamp.IsZero() {
			v.Timestamp = v.ObservedAt
		}

		// Check frozen frame detector if cached frame data is available
		if p.proxyCache != nil && p.frozenDetector != nil {
			if cachedData, ok := p.proxyCache.Get(v.CameraID); ok && len(cachedData) > 0 {
				if p.frozenDetector.CheckBytes(v.CameraID, cachedData) {
					v.Stale = true
				}
			}
		}

		// Fan-out 0: record in thread-safe in-memory cache for GET /api/v1/cameras/hotset/status (< 5ms latency).
		if p.hotsetStore != nil {
			p.hotsetStore.Set(v)
		}

		// Fan-out 1: push to all SSE clients immediately.
		if p.liveHub != nil {
			p.liveHub.Broadcast(v)
		}

		// Fan-out 1b: push hex cell update if floor of qualifying cameras is met.
		if p.hexAggregator != nil {
			p.hexAggregator.OnVerdict(v)
		}

		// Fan-out 1c: push flood hex cell update if safety floor (>= 1 camera) is met.
		if p.floodHexAggregator != nil {
			p.floodHexAggregator.OnVerdict(v)
		}

		// Fan-out 1d: dispatch real-time flood alert notification to subscribers.
		if p.dispatchFloodAlert != nil {
			p.dispatchFloodAlert(v)
		}

		// Fan-out 2: persist to SQLite for GET /api/traffic-status hydration.
		if p.verdictRepo != nil {
			sv := store.Verdict{
				CameraID:             v.CameraID,
				Severity:             v.Severity,
				Confidence:           v.Confidence,
				Source:               v.Source,
				ObservedAt:           v.ObservedAt,
				Timestamp:            v.Timestamp,
				FloodSeverity:        v.FloodSeverity,
				FloodDepthCm:         v.FloodDepthCm,
				FloodHazardLevel:     v.FloodHazardLevel,
				FloodHazardBasis:     v.FloodHazardBasis,
				FloodDepthConfidence: v.FloodDepthConfidence,
				FloodCalibrationRef:  v.FloodCalibrationRef,
			}
			if err := p.verdictRepo.Save(context.Background(), sv); err != nil {
				log.Printf("[WARN] Failed to persist verdict for %s: %v", v.CameraID, err)
			}
		}

		// Fan-out 2b: append to the Hot Set time series consumed by the
		// short-horizon congestion forecast feature store (Issue #302).
		// The recordability gate lives in hotset.RecordableObservation so the
		// 24h coverage verification harness shares one definition with
		// production (Issue #441).
		if p.verdictHistoryRepo != nil && hotset.RecordableObservation(v) {
			observedAt := v.ObservedAt
			if observedAt.IsZero() {
				observedAt = v.Timestamp
			}
			// 1. Frame-wide observation (direction = '')
			hv := store.VerdictHistory{
				CameraID:        v.CameraID,
				Direction:       "",
				RegionID:        "",
				PCUTotal:        v.PCUTotal,
				SaturationIndex: v.SaturationIndex,
				Severity:        v.Severity,
				Confidence:      v.Confidence,
				Source:          v.Source,
				ObservedAt:      observedAt,
			}
			if err := p.verdictHistoryRepo.Record(context.Background(), hv); err != nil {
				log.Printf("[WARN] Failed to record verdict history for %s: %v", v.CameraID, err)
			}

			// 2. Directional observations (ADR-012 D1)
			for _, dir := range v.Directions {
				dirHV := store.VerdictHistory{
					CameraID:        v.CameraID,
					Direction:       dir.Direction,
					RegionID:        dir.RegionID,
					PCUTotal:        dir.PCUTotal,
					SaturationIndex: dir.SaturationIndex,
					Severity:        dir.Severity,
					Confidence:      v.Confidence,
					Source:          v.Source,
					ObservedAt:      observedAt,
				}
				if err := p.verdictHistoryRepo.Record(context.Background(), dirHV); err != nil {
					log.Printf("[WARN] Failed to record directional verdict history for %s (dir %s): %v", v.CameraID, dir.Direction, err)
				}
			}
		}

		// Fan-out 3: event-driven snapshot archiver with retention policy (Issue #62, #182).
		if p.arch != nil {
			var frameReader io.Reader
			if len(v.RawFrame) > 0 {
				// Direct frame delivery from analysis pipeline (T2b / Issue #182)
				frameReader = bytes.NewReader(v.RawFrame)
			} else if p.proxyCache != nil {
				if cachedData, ok := p.proxyCache.Get(v.CameraID); ok && len(cachedData) > 0 {
					// Preserved fallback for non-HotSet or viewer-touched flows
					frameReader = bytes.NewReader(cachedData)
				}
			}
			if frameReader == nil && p.analyzerMode == "mock" {
				// In mock analysis mode without pre-fetched proxy cache, provide a minimal mock JPEG
				// so dev environment starts collecting proof frames immediately.
				frameReader = bytes.NewReader(archiver.MockJPEGData)
			}
			if frameReader != nil {
				if err := p.arch.OnVerdict(v, frameReader); err != nil {
					log.Printf("[WARN] Archiver failed for %s: %v", v.CameraID, err)
				}
			}
		}
	}
}

// startWeatherSamplerJob starts the periodic weather observation recorder.
func startWeatherSamplerJob(ctx context.Context, sampler *weather.Sampler, interval time.Duration) {
	if sampler == nil {
		return
	}
	go sampler.Start(ctx, interval)
}

// startForecastReloaderJob starts the periodic forecast model file reloader.
func startForecastReloaderJob(ctx context.Context, forecastService *forecast.Service) {
	if forecastService == nil {
		return
	}
	if forecastService.ReloadInterval() > 0 && forecastService.ModelPath() != "" {
		forecastService.StartReloader(ctx, forecastService.ReloadInterval())
	}
}

// startFloodEnvironmentalTrigger starts the environmental rain polling loop.
func startFloodEnvironmentalTrigger(ctx context.Context, floodService *flood.Service, interval time.Duration, onVerdict func(hotset.Verdict)) {
	if floodService == nil {
		return
	}
	go floodService.StartEnvironmentalTrigger(ctx, interval, onVerdict)
}

// startArchiverRetention starts the snapshot archiver disk retention worker.
func startArchiverRetention(ctx context.Context, arch *archiver.Archiver, retentionRootDir string) {
	if arch == nil || retentionRootDir == "" {
		return
	}
	go arch.StartRetention(ctx)
}

// startHotsetScheduler runs the hotset analysis scheduler.
func startHotsetScheduler(ctx context.Context, s *hotset.Scheduler) {
	if s == nil {
		return
	}
	go s.Run(ctx)
}

// startOpsWatchdogJob runs the operational alert watchdog (S14-T1, Issue #441).
// It owns its own ticker so a stuck analysis cycle cannot silence alerting.
func startOpsWatchdogJob(ctx context.Context, w *hotset.Watchdog) {
	if w == nil {
		return
	}
	go w.Run(ctx)
}

// startHarvesterJob starts the autonomous harvester coordinator.
func startHarvesterJob(ctx context.Context, coord *harvester.Coordinator, enabled bool) {
	if coord == nil {
		return
	}
	if !enabled {
		log.Println("[INIT] Autonomous harvester disabled via configuration (HARVESTER_ENABLED=false)")
		return
	}
	if err := coord.Start(ctx); err != nil {
		log.Printf("[WARN] Failed to start autonomous harvester: %v", err)
	}
}
