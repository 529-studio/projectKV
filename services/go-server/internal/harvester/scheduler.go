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

// TimeSlot represents a daily target time for scheduled harvesting.
type TimeSlot struct {
	Hour   int
	Minute int
}

// String formats the slot as HH:MM.
func (ts TimeSlot) String() string {
	return fmt.Sprintf("%02d:%02d", ts.Hour, ts.Minute)
}

// ParseScheduleSlots parses configured hours string into TimeSlots.
// Supports both full "HH:MM" (e.g. "07:30,12:00,17:30") and hour integers (e.g. "7,12,17").
// Integer hours automatically default to calibrated peak commuter minutes (07:30, 12:00, 17:30).
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

// Scheduler triggers periodic harvest runs at specific times of day in ICT (UTC+7).
type Scheduler struct {
	slots        []TimeSlot
	loc          *time.Location
	onTrigger    func(ctx context.Context) error
	checkTicker  time.Duration
	mu           sync.Mutex
	lastRunSlots map[string]string // slot string -> "YYYY-MM-DD"
}

// NewScheduler constructs a new Scheduler.
func NewScheduler(scheduleHours string, loc *time.Location, onTrigger func(ctx context.Context) error) *Scheduler {
	if loc == nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	slots := ParseScheduleSlots(scheduleHours)
	return &Scheduler{
		slots:        slots,
		loc:          loc,
		onTrigger:    onTrigger,
		checkTicker:  30 * time.Second,
		lastRunSlots: make(map[string]string),
	}
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

// Run begins the scheduler loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
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
					if err := s.onTrigger(ctx); err != nil {
						slog.Error("harvester: scheduled harvest execution error", "slot", slot, "err", err)
					}
				}
			}
		}
	}
}
