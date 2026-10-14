package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Manager coordinates notification channels and enforces anti-spam debouncing.
type Manager struct {
	mu        sync.RWMutex
	channels  map[string]Channel
	debouncer *Debouncer
	log       *slog.Logger
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

// NewManager creates a new notification Manager.
func NewManager(debouncer *Debouncer, opts ...ManagerOption) *Manager {
	if debouncer == nil {
		debouncer = NewDebouncer(DefaultDebounceWindow)
	}
	m := &Manager{
		channels:  make(map[string]Channel),
		debouncer: debouncer,
		log:       slog.Default(),
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

// IsFloodSeverity returns true if severity represents actionable flooding.
func IsFloodSeverity(severity string) bool {
	return severity == "FLOODED_SHALLOW" || severity == "FLOODED_DEEP"
}

// Dispatch evaluates a FloodAlert, checks the 30-minute anti-spam debounce window,
// and fans out delivery to all registered channels.
func (m *Manager) Dispatch(ctx context.Context, alert FloodAlert) error {
	// 1. Only dispatch actionable flood conditions (shallow or deep flood)
	if !IsFloodSeverity(alert.FloodSeverity) {
		m.log.Debug("notify: skipping non-flood verdict", "camera_id", alert.CameraID, "severity", alert.FloodSeverity)
		return nil
	}

	// 2. 30-Minute anti-spam sliding debounce check per chokepoint
	if m.debouncer.ShouldSuppress(alert.CameraID, alert.ObservedAt) {
		m.log.Info("notify: flood alert suppressed by 30-minute debounce window", "camera_id", alert.CameraID, "severity", alert.FloodSeverity)
		return nil
	}

	// 3. Collect registered channels
	channels := m.Channels()
	if len(channels) == 0 {
		m.log.Debug("notify: no channels registered, alert not dispatched", "camera_id", alert.CameraID)
		return nil
	}

	m.log.Info("notify: dispatching flood alert to channels", "camera_id", alert.CameraID, "severity", alert.FloodSeverity, "channels_count", len(channels))

	// 4. Concurrent fan-out across channels
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
