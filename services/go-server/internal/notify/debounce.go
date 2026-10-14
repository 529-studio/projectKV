package notify

import (
	"sync"
	"time"
)

// DefaultDebounceWindow is the standard anti-spam quiet period (30 minutes)
// between flood notifications for the same chokepoint / camera.
const DefaultDebounceWindow = 30 * time.Minute

// Debouncer manages thread-safe sliding debounce windows per camera/chokepoint.
type Debouncer struct {
	mu         sync.RWMutex
	window     time.Duration
	lastAlerts map[string]time.Time
}

// NewDebouncer creates a Debouncer with the specified quiet period duration.
func NewDebouncer(window time.Duration) *Debouncer {
	if window <= 0 {
		window = DefaultDebounceWindow
	}
	return &Debouncer{
		window:     window,
		lastAlerts: make(map[string]time.Time),
	}
}

// ShouldSuppress checks whether cameraID has already triggered an alert within the quiet period.
// If NOT suppressed, it records now as the latest alert timestamp and returns false.
// If suppressed, it returns true.
func (d *Debouncer) ShouldSuppress(cameraID string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if now.IsZero() {
		now = time.Now()
	}

	last, exists := d.lastAlerts[cameraID]
	if exists && now.Sub(last) < d.window && !now.Before(last) {
		return true
	}

	d.lastAlerts[cameraID] = now
	return false
}

// LastDispatched returns the timestamp of the last dispatched alert for cameraID.
func (d *Debouncer) LastDispatched(cameraID string) time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastAlerts[cameraID]
}

// Reset clears recorded state for a cameraID, or all cameras if cameraID is empty.
func (d *Debouncer) Reset(cameraID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cameraID == "" {
		d.lastAlerts = make(map[string]time.Time)
		return
	}
	delete(d.lastAlerts, cameraID)
}
