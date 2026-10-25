package harvester

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// PriorityCorridors defines the 4 main arterial chokepoint corridors for daily scheduled harvest.
var PriorityCorridors = map[string]struct{}{
	"vo_van_kiet":   {},
	"mai_chi_tho":   {},
	"dien_bien_phu": {},
	"cong_hoa":      {},
}

// FloodProneCorridors defines the 24 historical flood-prone corridors for rain watcher harvest.
var FloodProneCorridors = map[string]struct{}{
	"ung_van_khiem":     {},
	"quoc_lo_13":        {},
	"huynh_tan_phat":    {},
	"tran_xuan_soan":    {},
	"nguyen_xi":         {},
	"nguyen_huu_canh":   {},
	"kha_van_can":       {},
	"le_van_luong":      {},
	"an_duong_vuong":    {},
	"kinh_duong_vuong":  {},
	"vo_van_kiet":       {},
	"dong_den":          {},
	"phan_huy_ich":      {},
	"quoc_lo_1":         {},
	"xo_viet_nghe_tinh": {},
	"dinh_bo_linh":      {},
	"to_ngoc_van":       {},
	"bach_dang":         {},
	"le_duc_tho":        {},
	"pham_van_dong":     {},
	"hoang_van_thu":     {},
	"cong_hoa":          {},
	"mai_chi_tho":       {},
	"dien_bien_phu":     {},
}

// FilterHotsetCameras selects the 57 priority dual-carriageway intersection cameras.
func FilterHotsetCameras(cameras []Camera) []Camera {
	var selected []Camera
	for _, c := range cameras {
		if len(c.RoadKeys) == 2 {
			for _, rk := range c.RoadKeys {
				if _, ok := PriorityCorridors[rk]; ok {
					selected = append(selected, c)
					break
				}
			}
		}
	}
	return selected
}

// FilterFloodCorridorCameras selects online cameras belonging to flood-prone corridors.
func FilterFloodCorridorCameras(cameras []Camera) []Camera {
	var selected []Camera
	for _, c := range cameras {
		for _, rk := range c.RoadKeys {
			if _, ok := FloodProneCorridors[rk]; ok {
				selected = append(selected, c)
				break
			}
		}
	}
	return selected
}

// Coordinator orchestrates the autonomous harvester lifecycle, scheduler, and weather watcher.
type Coordinator struct {
	cfg           Config
	weather       WeatherProvider
	fetcher       SnapshotFetcher
	predictor     VisionPredictor
	storage       StorageUploader
	hotsetCameras []Camera
	rainCameras   []Camera

	pipeline  *Pipeline
	scheduler *Scheduler
	watcher   *Watcher
	stats     *runStats

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New constructs a Coordinator instance with injected dependencies.
func New(cfg Config, deps Dependencies) (*Coordinator, error) {
	if deps.Fetcher == nil {
		return nil, fmt.Errorf("harvester: missing SnapshotFetcher")
	}
	if deps.Predictor == nil {
		return nil, fmt.Errorf("harvester: missing VisionPredictor")
	}
	if deps.Storage == nil {
		return nil, fmt.Errorf("harvester: missing StorageUploader")
	}

	loc := cfg.TimeLocation
	if loc == nil {
		loc = time.FixedZone("ICT", 7*3600)
	}

	pipeline := NewPipeline(deps.Fetcher, deps.Predictor, deps.Storage, cfg.Concurrency, loc)

	c := &Coordinator{
		cfg:           cfg,
		weather:       deps.Weather,
		fetcher:       deps.Fetcher,
		predictor:     deps.Predictor,
		storage:       deps.Storage,
		hotsetCameras: deps.HotsetCameras,
		rainCameras:   deps.RainCameras,
		pipeline:      pipeline,
		stats:         newRunStats(),
	}

	c.scheduler = NewScheduler(cfg.ScheduleHours, loc, func(ctx context.Context, slot string) error {
		_, err := c.runScheduledHarvestSlot(ctx, slot)
		return err
	}, cfg.CatchupWindow)

	if deps.Weather != nil {
		c.watcher = NewWatcher(deps.Weather, cfg.RainCooldownMinutes, cfg.WatcherInterval, func(ctx context.Context, w *WeatherInfo) error {
			_, err := c.runRainHarvestWithWeather(ctx, w)
			return err
		})
	}

	return c, nil
}

// Start launches the daily peak scheduler and weather watcher goroutines.
func (c *Coordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	if !c.cfg.Enabled {
		slog.Info("harvester: disabled via configuration (HARVESTER_ENABLED=false)")
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true

	slog.Info("harvester: starting autonomous dual-trigger harvester subsystem",
		"hotset_cameras", len(c.hotsetCameras),
		"rain_cameras", len(c.rainCameras),
		"concurrency", c.cfg.Concurrency,
		"cooldown_minutes", c.cfg.RainCooldownMinutes)

	// Launch daily peak hour scheduler
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.scheduler.Run(runCtx)
	}()

	// Launch meteorological rain watcher if weather provider is available
	if c.watcher != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.watcher.Run(runCtx)
		}()
	}

	return nil
}

// Stop gracefully terminates background scheduler and watcher loops.
func (c *Coordinator) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	c.wg.Wait()
	slog.Info("harvester: background poller terminated gracefully")
}

// RunScheduledHarvest triggers harvesting across the Hot Set cameras immediately.
func (c *Coordinator) RunScheduledHarvest(ctx context.Context) (int, error) {
	return c.runScheduledHarvestSlot(ctx, "")
}

func (c *Coordinator) runScheduledHarvestSlot(ctx context.Context, slot string) (int, error) {
	scope := string(ScopeScheduled)
	if slot == "" {
		scope = string(ScopeManual)
	}
	started := time.Now().UTC()
	slog.Info("harvester: executing scheduled peak harvest", "slot", slot, "cameras", len(c.hotsetCameras))

	var weather *WeatherInfo
	if c.weather != nil {
		if w, err := c.weather.GetWeather(ctx); err == nil {
			weather = w
		}
	}

	count, err := c.pipeline.HarvestCameras(ctx, c.hotsetCameras, ScopeScheduled, weather)
	slog.Info("harvester: scheduled harvest complete", "slot", slot, "harvested", count, "total", len(c.hotsetCameras))

	c.stats.record(RunRecord{
		Scope:      scope,
		Slot:       slot,
		StartedAt:  started,
		DurationMs: time.Since(started).Milliseconds(),
		Total:      len(c.hotsetCameras),
		Harvested:  count,
		Failed:     len(c.hotsetCameras) - count,
		Error:      errString(err),
	})
	return count, err
}

// RunRainHarvest triggers harvesting across the flood-prone corridor cameras immediately.
func (c *Coordinator) RunRainHarvest(ctx context.Context) (int, error) {
	var weather *WeatherInfo
	if c.weather != nil {
		if w, err := c.weather.GetWeather(ctx); err == nil {
			weather = w
		}
	}
	return c.runRainHarvestWithWeather(ctx, weather)
}

func (c *Coordinator) runRainHarvestWithWeather(ctx context.Context, weather *WeatherInfo) (int, error) {
	started := time.Now().UTC()
	slog.Info("harvester: executing meteorological rain harvest", "cameras", len(c.rainCameras))
	count, err := c.pipeline.HarvestCameras(ctx, c.rainCameras, ScopeRain, weather)
	slog.Info("harvester: rain harvest complete", "harvested", count, "total", len(c.rainCameras))

	c.stats.record(RunRecord{
		Scope:      string(ScopeRain),
		StartedAt:  started,
		DurationMs: time.Since(started).Milliseconds(),
		Total:      len(c.rainCameras),
		Harvested:  count,
		Failed:     len(c.rainCameras) - count,
		Error:      errString(err),
	})
	return count, err
}

// Status assembles the observability snapshot served by GET /api/harvester/status.
func (c *Coordinator) Status() Status {
	slotRuns, lastRain, totals, recentErrs, startedAt := c.stats.snapshot()

	slots := c.scheduler.Slots()
	labels := make([]string, 0, len(slots))
	for _, s := range slots {
		labels = append(labels, s.String())
	}
	nextLabel, nextAt := c.scheduler.NextSlot(time.Now())

	rain := RainWatcherStatus{Enabled: c.watcher != nil}
	if c.watcher != nil {
		rain.CooldownMinutes = c.cfg.RainCooldownMinutes
		if lt := c.watcher.LastTriggeredAt(); !lt.IsZero() {
			lt = lt.UTC()
			rain.LastTriggerAt = &lt
		}
	}

	return Status{
		Enabled:       c.cfg.Enabled,
		ScheduleSlots: labels,
		NextSlot:      nextLabel,
		NextSlotAt:    nextAt.UTC(),
		SlotRuns:      slotRuns,
		LastRain:      lastRain,
		Totals:        totals,
		RainWatcher:   rain,
		StartedAt:     startedAt,
		RecentErrors:  recentErrs,
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Pipeline returns the underlying ingestion pipeline.
func (c *Coordinator) Pipeline() *Pipeline {
	return c.pipeline
}

// Watcher returns the weather watcher instance.
func (c *Coordinator) Watcher() *Watcher {
	return c.watcher
}

// Scheduler returns the scheduler instance.
func (c *Coordinator) Scheduler() *Scheduler {
	return c.scheduler
}
