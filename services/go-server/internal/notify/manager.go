package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultCorridorDebounceWindow is the standard anti-spam quiet period (15 minutes)
// between hazard notifications for the same arterial corridor.
const DefaultCorridorDebounceWindow = 15 * time.Minute

// Manager coordinates notification channels and enforces anti-spam debouncing.
type Manager struct {
	mu                sync.RWMutex
	channels          map[string]Channel
	debouncer         *Debouncer
	corridorDebouncer *Debouncer
	stateTracker      *StateTracker
	log               *slog.Logger
}

// ManagerOption configures Manager.
type ManagerOption func(*Manager)

// WithManagerLogger overrides the default logger for Manager.
func WithManagerLogger(log *slog.Logger) ManagerOption {
	return func(m *Manager) {
		if log != nil {
			m.log = log
		}
	}
}

// WithCorridorDebouncer overrides the default corridor debouncer.
func WithCorridorDebouncer(d *Debouncer) ManagerOption {
	return func(m *Manager) {
		if d != nil {
			m.corridorDebouncer = d
		}
	}
}

// WithCorridorDebounceWindow sets the quiet period for corridor debouncing.
func WithCorridorDebounceWindow(w time.Duration) ManagerOption {
	return func(m *Manager) {
		if w > 0 {
			m.corridorDebouncer = NewDebouncer(w)
		}
	}
}

// WithStateTracker overrides the default state tracker for Manager.
func WithStateTracker(st *StateTracker) ManagerOption {
	return func(m *Manager) {
		if st != nil {
			m.stateTracker = st
		}
	}
}

// NewManager creates a new notification Manager.
func NewManager(debouncer *Debouncer, opts ...ManagerOption) *Manager {
	if debouncer == nil {
		debouncer = NewDebouncer(DefaultDebounceWindow)
	}
	m := &Manager{
		channels:          make(map[string]Channel),
		debouncer:         debouncer,
		corridorDebouncer: NewDebouncer(DefaultCorridorDebounceWindow),
		stateTracker:      NewStateTracker(),
		log:               slog.Default(),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Register registers a delivery channel (e.g. TelegramChannel).
func (m *Manager) Register(ch Channel) {
	if ch == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Name()] = ch
	m.log.Info("notify: registered channel", "channel", ch.Name())
}

// GetChannel retrieves a registered channel by name.
func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[name]
	return ch, ok
}

// Channels returns all registered delivery channels.
func (m *Manager) Channels() []Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Channel, 0, len(m.channels))
	for _, ch := range m.channels {
		list = append(list, ch)
	}
	return list
}

// Debouncer returns the underlying Debouncer instance.
func (m *Manager) Debouncer() *Debouncer {
	return m.debouncer
}

// CorridorDebouncer returns the underlying Debouncer instance for arterial corridors.
func (m *Manager) CorridorDebouncer() *Debouncer {
	return m.corridorDebouncer
}

// StateTracker returns the underlying StateTracker instance.
func (m *Manager) StateTracker() *StateTracker {
	return m.stateTracker
}

// IsFloodSeverity returns true if severity represents actionable flooding.
func IsFloodSeverity(severity string) bool {
	return severity == "FLOODED_SHALLOW" || severity == "FLOODED_DEEP"
}

// Dispatch evaluates a FloodAlert, checks state transitions and anti-spam debounce windows,
// and fans out delivery to all registered channels.
func (m *Manager) Dispatch(ctx context.Context, alert FloodAlert) error {
	// 1. Record state transition in StateTracker
	var tr StateTransition
	if m.stateTracker != nil {
		tr = m.stateTracker.RecordCamera(alert.CameraID, alert.FloodSeverity, alert.ObservedAt)
	}

	// 2. Only dispatch actionable flood conditions (shallow or deep flood)
	if !IsFloodSeverity(alert.FloodSeverity) {
		m.log.Debug("notify: skipping non-flood verdict", "camera_id", alert.CameraID, "severity", alert.FloodSeverity)
		return nil
	}

	// 3. Debounce suppression check:
	// Escalations (e.g. CLEAR -> SHALLOW/DEEP or SHALLOW -> DEEP) bypass the debounce window
	// to ensure critical flood hazards are immediately delivered, while refreshing the debounce window.
	if tr.IsEscalation {
		m.debouncer.RecordAlert(alert.CameraID, alert.ObservedAt)
	} else if m.debouncer.ShouldSuppress(alert.CameraID, alert.ObservedAt) {
		m.log.Info("notify: flood alert suppressed by 30-minute debounce window", "camera_id", alert.CameraID, "severity", alert.FloodSeverity)
		return nil
	}

	// 4. Collect registered channels
	channels := m.Channels()
	if len(channels) == 0 {
		m.log.Debug("notify: no channels registered, alert not dispatched", "camera_id", alert.CameraID)
		return nil
	}

	m.log.Info("notify: dispatching flood alert to channels", "camera_id", alert.CameraID, "severity", alert.FloodSeverity, "channels_count", len(channels), "escalation", tr.IsEscalation)

	// 5. Concurrent fan-out across channels
	var wg sync.WaitGroup
	errCh := make(chan error, len(channels))

	for _, ch := range channels {
		wg.Add(1)
		go func(c Channel) {
			defer wg.Done()
			if err := c.Send(ctx, alert); err != nil {
				m.log.Error("notify: channel dispatch failed", "channel", c.Name(), "camera_id", alert.CameraID, "error", err)
				errCh <- fmt.Errorf("channel %s: %w", c.Name(), err)
			}
		}(ch)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// DispatchCorridor evaluates a CorridorAlert, checks state transitions and anti-spam debounce windows,
// and fans out delivery to registered channels that support corridor alerts.
func (m *Manager) DispatchCorridor(ctx context.Context, alert CorridorAlert) error {
	// 1. Record state transition in StateTracker
	var tr StateTransition
	if m.stateTracker != nil {
		tr = m.stateTracker.RecordCorridor(alert.RoadKey, alert.Passability, alert.ObservedAt)
	}

	// 2. Only dispatch actionable hazard conditions (IMPASSABLE, CAUTION, or depth >= 15 cm)
	isHazard := strings.EqualFold(alert.Passability, "IMPASSABLE") ||
		strings.EqualFold(alert.Passability, "CAUTION") ||
		alert.WorstFloodSeverity == "FLOODED_SHALLOW" ||
		alert.WorstFloodSeverity == "FLOODED_DEEP" ||
		(alert.MaxDepthCm != nil && *alert.MaxDepthCm >= 15.0)

	if !isHazard {
		m.log.Debug("notify: skipping non-hazard corridor status", "road_key", alert.RoadKey, "passability", alert.Passability)
		return nil
	}

	// 3. Debounce suppression check:
	// Escalations (e.g. PASSABLE -> CAUTION/IMPASSABLE or CAUTION -> IMPASSABLE) bypass debounce suppression.
	if tr.IsEscalation {
		m.corridorDebouncer.RecordAlert(alert.RoadKey, alert.ObservedAt)
	} else if m.corridorDebouncer.ShouldSuppress(alert.RoadKey, alert.ObservedAt) {
		m.log.Info("notify: corridor alert suppressed by 15-minute debounce window", "road_key", alert.RoadKey, "passability", alert.Passability)
		return nil
	}

	// 4. Collect registered channels
	channels := m.Channels()
	if len(channels) == 0 {
		m.log.Debug("notify: no channels registered, corridor alert not dispatched", "road_key", alert.RoadKey)
		return nil
	}

	m.log.Info("notify: dispatching corridor alert to channels", "road_key", alert.RoadKey, "passability", alert.Passability, "channels_count", len(channels), "escalation", tr.IsEscalation)

	// 4. Concurrent fan-out across channels supporting corridor alerts
	var wg sync.WaitGroup
	errCh := make(chan error, len(channels))

	for _, ch := range channels {
		corridorCh, ok := ch.(interface {
			SendCorridor(ctx context.Context, alert CorridorAlert) error
		})
		if !ok {
			continue
		}
		wg.Add(1)
		go func(c interface {
			SendCorridor(ctx context.Context, alert CorridorAlert) error
		}, name string) {
			defer wg.Done()
			if err := c.SendCorridor(ctx, alert); err != nil {
				m.log.Error("notify: corridor channel dispatch failed", "channel", name, "road_key", alert.RoadKey, "error", err)
				errCh <- fmt.Errorf("channel %s: %w", name, err)
			}
		}(corridorCh, ch.Name())
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
