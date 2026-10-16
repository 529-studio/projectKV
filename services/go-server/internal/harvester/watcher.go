package harvester

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Watcher polls meteorological telemetry and triggers rain harvesting when thresholds are met.
type Watcher struct {
	weather         WeatherProvider
	cooldown        time.Duration
	pollInterval    time.Duration
	onTrigger       func(ctx context.Context, weather *WeatherInfo) error
	mu              sync.Mutex
	lastTriggeredAt time.Time
}

// NewWatcher constructs a new meteorological Watcher.
func NewWatcher(weather WeatherProvider, cooldownMinutes int, pollInterval time.Duration, onTrigger func(ctx context.Context, weather *WeatherInfo) error) *Watcher {
	if cooldownMinutes <= 0 {
		cooldownMinutes = 60
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Minute
	}
	return &Watcher{
		weather:      weather,
		cooldown:     time.Duration(cooldownMinutes) * time.Minute,
		pollInterval: pollInterval,
		onTrigger:    onTrigger,
	}
}

// LastTriggeredAt returns the timestamp of the most recent rain trigger.
func (w *Watcher) LastTriggeredAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastTriggeredAt
}

// CheckTrigger fetches current weather and evaluates rain conditions and sliding cooldown.
func (w *Watcher) CheckTrigger(ctx context.Context) (triggered bool, info *WeatherInfo, err error) {
	if w.weather == nil {
		return false, nil, fmt.Errorf("weather provider is nil")
	}

	info, err = w.weather.GetWeather(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("fetch weather: %w", err)
	}

	if info == nil {
		return false, nil, nil
	}

	isRain := info.RainfallMm >= 0.10 || IsRainWeatherCode(info.WeatherCode)
	if !isRain {
		return false, info, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	if !w.lastTriggeredAt.IsZero() && now.Sub(w.lastTriggeredAt) < w.cooldown {
		slog.Debug("harvester: rain detected but sliding window cooldown active",
			"elapsed_minutes", now.Sub(w.lastTriggeredAt).Minutes(),
			"cooldown_minutes", w.cooldown.Minutes())
		return false, info, nil
	}

	w.lastTriggeredAt = now
	return true, info, nil
}

// Run starts the 5-minute polling loop. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			triggered, info, err := w.CheckTrigger(ctx)
			if err != nil {
				slog.Warn("harvester: weather check failed", "err", err)
				continue
			}

			if triggered {
				slog.Info("harvester: meteorological rain harvest triggered",
					"rainfall_mm", info.RainfallMm,
					"weather_code", info.WeatherCode)
				if w.onTrigger != nil {
					if err := w.onTrigger(ctx, info); err != nil {
						slog.Error("harvester: rain harvest execution error", "err", err)
					}
				}
			}
		}
	}
}
