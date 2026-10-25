package harvester

import (
	"sync"
	"time"
)

// maxRecentErrors bounds the recent-errors ring exposed on the status endpoint.
const maxRecentErrors = 10

// RunRecord describes one completed harvest run (scheduled slot, catch-up,
// rain trigger, or manual invocation).
type RunRecord struct {
	Scope      string    `json:"scope"` // scheduled_peak | rain_watcher | manual
	Slot       string    `json:"slot,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Total      int       `json:"total_cameras"`
	Harvested  int       `json:"harvested"`
	Failed     int       `json:"failed"`
	Error      string    `json:"error,omitempty"`
}

// RunTotals aggregates lifetime counters since coordinator start.
type RunTotals struct {
	ScheduledRuns    int `json:"scheduled_runs"`
	RainRuns         int `json:"rain_runs"`
	CamerasHarvested int `json:"cameras_harvested"`
	CamerasFailed    int `json:"cameras_failed"`
}

// RainWatcherStatus exposes the rain-trigger watcher state for observability.
type RainWatcherStatus struct {
	Enabled         bool       `json:"enabled"`
	CooldownMinutes int        `json:"cooldown_minutes"`
	LastTriggerAt   *time.Time `json:"last_trigger_at,omitempty"`
}

// Status is the JSON payload served by GET /api/harvester/status (Issue #299).
type Status struct {
	Enabled       bool                 `json:"enabled"`
	ScheduleSlots []string             `json:"schedule_slots"`
	NextSlot      string               `json:"next_slot,omitempty"`
	NextSlotAt    time.Time            `json:"next_slot_at,omitempty"`
	SlotRuns      map[string]RunRecord `json:"slot_runs,omitempty"`
	LastRain      *RunRecord           `json:"last_rain,omitempty"`
	Totals        RunTotals            `json:"totals"`
	RainWatcher   RainWatcherStatus    `json:"rain_watcher"`
	StartedAt     time.Time            `json:"started_at"`
	RecentErrors  []string             `json:"recent_errors"`
}

// runStats is the thread-safe run bookkeeping behind Status.
type runStats struct {
	mu           sync.Mutex
	startedAt    time.Time
	slotRuns     map[string]RunRecord
	lastRain     *RunRecord
	totals       RunTotals
	recentErrors []string
}

func newRunStats() *runStats {
	return &runStats{
		startedAt: time.Now().UTC(),
		slotRuns:  make(map[string]RunRecord),
	}
}

func (r *runStats) record(rec RunRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch Scope(rec.Scope) {
	case ScopeRain:
		cp := rec
		r.lastRain = &cp
		r.totals.RainRuns++
	case ScopeScheduled:
		r.totals.ScheduledRuns++
		if rec.Slot != "" {
			r.slotRuns[rec.Slot] = rec
		}
	default: // manual
		r.totals.ScheduledRuns++
	}
	r.totals.CamerasHarvested += rec.Harvested
	r.totals.CamerasFailed += rec.Failed

	if rec.Error != "" && len(r.recentErrors) < maxRecentErrors {
		r.recentErrors = append(r.recentErrors, rec.StartedAt.Format(time.RFC3339)+" "+rec.Scope+" "+rec.Slot+": "+rec.Error)
	}
}

func (r *runStats) snapshot() (map[string]RunRecord, *RunRecord, RunTotals, []string, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slots := make(map[string]RunRecord, len(r.slotRuns))
	for k, v := range r.slotRuns {
		slots[k] = v
	}
	var rain *RunRecord
	if r.lastRain != nil {
		cp := *r.lastRain
		rain = &cp
	}
	errs := make([]string, len(r.recentErrors))
	copy(errs, r.recentErrors)
	return slots, rain, r.totals, errs, r.startedAt
}
