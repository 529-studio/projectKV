package harvester

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TimeSlot represents a daily schedule time in scheduler-local (ICT) time.
type TimeSlot struct {
	Hour   int
	Minute int
}

// String returns the "HH:MM" slot label.
func (t TimeSlot) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

func ParseScheduleSlots(raw string) []TimeSlot {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	if raw == "" {
		raw = "07:30,12:00,17:30"
	}

	parts := strings.Split(raw, ",")
	slots := make([]TimeSlot, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p == "" {
			continue
		}

		if strings.Contains(p, ":") {
			hm := strings.SplitN(p, ":", 2)
			h, errH := strconv.Atoi(strings.TrimSpace(hm[0]))
			m, errM := strconv.Atoi(strings.TrimSpace(hm[1]))
			if errH == nil && errM == nil && h >= 0 && h < 24 && m >= 0 && m < 60 {
				slots = append(slots, TimeSlot{Hour: h, Minute: m})
			}
		} else {
			h, err := strconv.Atoi(p)
			if err == nil && h >= 0 && h < 24 {
				m := 0
				if h == 7 || h == 17 {
					m = 30 // 07:30 and 17:30 peak commuting onset
				}
				slots = append(slots, TimeSlot{Hour: h, Minute: m})
			}
		}
	}

	if len(slots) == 0 {
		slots = []TimeSlot{
			{Hour: 7, Minute: 30},
			{Hour: 12, Minute: 0},
			{Hour: 17, Minute: 30},
		}
	}

	return slots
}

// TriggerFunc is invoked when a scheduled slot fires; it receives the slot
// label (e.g. "07:30") so the caller can attribute runs to their slot.
type TriggerFunc func(ctx context.Context, slot string) error

// Scheduler triggers periodic harvest runs at specific times of day in ICT (UTC+7).
type Scheduler struct {
	slots        []TimeSlot
	loc          *time.Location
	onTrigger    TriggerFunc
	checkTicker  time.Duration
	lastRunSlots map[string]string
	catchup      time.Duration

	mu sync.Mutex
}

func NewScheduler(scheduleHours string, loc *time.Location, onTrigger TriggerFunc, catchup time.Duration) *Scheduler {
	if loc == nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	if catchup < 0 {
		catchup = 0
	}
	slots := ParseScheduleSlots(scheduleHours)
	return &Scheduler{
		slots:        slots,
		loc:          loc,
		onTrigger:    onTrigger,
		checkTicker:  30 * time.Second,
		lastRunSlots: make(map[string]string),
		catchup:      catchup,
	}
}

// Slots returns a copy of the parsed schedule slots.
func (s *Scheduler) Slots() []TimeSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TimeSlot, len(s.slots))
	copy(out, s.slots)
	return out
}

// NextSlot computes the label and absolute time of the next scheduled slot
// strictly after `now` (in scheduler-local time); today's remaining slots
// first, otherwise tomorrow's earliest slot.
func (s *Scheduler) NextSlot(now time.Time) (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.slots) == 0 {
		return "", time.Time{}
	}
	cur := now.In(s.loc)
	today := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, s.loc)
	var best time.Time
	found := false
	for _, sl := range s.slots {
		at := today.Add(time.Duration(sl.Hour)*time.Hour + time.Duration(sl.Minute)*time.Minute)
		if at.After(cur) && (!found || at.Before(best)) {
			best = at
			found = true
		}
	}
	if !found {
		// Tomorrow's earliest slot.
		for _, sl := range s.slots {
			at := today.Add(24*time.Hour + time.Duration(sl.Hour)*time.Hour + time.Duration(sl.Minute)*time.Minute)
			if !found || at.Before(best) {
				best = at
				found = true
			}
		}
	}
	return best.Format("15:04"), best
}

// ShouldTrigger checks if current ICT time matches any slot that hasn't run today.
func (s *Scheduler) ShouldTrigger(t time.Time) (slot string, shouldRun bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ictTime := t.In(s.loc)
	dateStr := ictTime.Format("2006-01-02")
	curHour := ictTime.Hour()
	curMin := ictTime.Minute()

	for _, slotObj := range s.slots {
		if curHour == slotObj.Hour && curMin == slotObj.Minute {
			slotKey := slotObj.String()
			if s.lastRunSlots[slotKey] != dateStr {
				s.lastRunSlots[slotKey] = dateStr
				return slotKey, true
			}
		}
	}

	return "", false
}

// CatchupSlots returns and marks today's slots whose time already passed
// within the configured catch-up window (Issue #299): a server that boots or
// restarts shortly after a slot still runs it, instead of silently losing the
// day's collection. Slots older than the window are not fired.
func (s *Scheduler) CatchupSlots(now time.Time) []string {
	if s.catchup <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur := now.In(s.loc)
	dateStr := cur.Format("2006-01-02")
	today := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, s.loc)

	var missed []string
	for _, sl := range s.slots {
		at := today.Add(time.Duration(sl.Hour)*time.Hour + time.Duration(sl.Minute)*time.Minute)
		if !at.After(cur) && cur.Sub(at) <= s.catchup {
			slotKey := sl.String()
			if s.lastRunSlots[slotKey] != dateStr {
				s.lastRunSlots[slotKey] = dateStr
				missed = append(missed, slotKey)
			}
		}
	}
	return missed
}

// Run begins the scheduler loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	// Fire any slot missed by a late boot or restart within the catch-up
	// window before entering the steady-state ticker loop.
	for _, slot := range s.CatchupSlots(time.Now()) {
		slog.Info("harvester: catch-up firing missed slot", "slot", slot)
		if s.onTrigger != nil {
			if err := s.onTrigger(ctx, slot); err != nil {
				slog.Error("harvester: catch-up harvest execution error", "slot", slot, "err", err)
			}
		}
	}

	ticker := time.NewTicker(s.checkTicker)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if slot, ok := s.ShouldTrigger(t); ok {
				slog.Info("harvester: daily scheduled harvest triggered", "slot", slot, "time_ict", t.In(s.loc).Format(time.RFC3339))
				if s.onTrigger != nil {
					if err := s.onTrigger(ctx, slot); err != nil {
						slog.Error("harvester: scheduled harvest execution error", "slot", slot, "err", err)
					}
				}
			}
		}
	}
}
