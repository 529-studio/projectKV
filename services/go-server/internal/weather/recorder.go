package weather

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"
)

// ObservationRecord holds normalized meteorological and radar state for persistence (ADR-012 D3).
type ObservationRecord struct {
	PrecipitationMm float64
	WeatherCode     int
	TemperatureC    float64
	HumidityPct     float64
	RadarActive     int
	RadarFrameAgeS  int
	Source          string
	PayloadHash     string
	ObservedAt      time.Time
	RecordedAt      time.Time
}

// Persister abstracts the storage layer for weather observations.
// Composition root in cmd/server adapts store.WeatherRepo to this interface.
type Persister interface {
	RecordObservation(ctx context.Context, obs ObservationRecord) error
}

// Sampler orchestrates periodic polling of Open-Meteo and RainViewer,
// computing payload hashes and persisting observations to the feature store.
type Sampler struct {
	service   *Service
	persister Persister
	logger    *slog.Logger
}

// NewSampler constructs a Sampler with the given service and persister.
func NewSampler(service *Service, persister Persister, logger *slog.Logger) *Sampler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sampler{
		service:   service,
		persister: persister,
		logger:    logger,
	}
}

// SampleOnce queries weather and radar, hashes the snapshot, and records the observation.
func (s *Sampler) SampleOnce(ctx context.Context) (*ObservationRecord, error) {
	if s.service == nil {
		return nil, fmt.Errorf("weather sampler: service is nil")
	}
	if s.persister == nil {
		return nil, fmt.Errorf("weather sampler: persister is nil")
	}

	w, err := s.service.GetWeather(ctx)
	if err != nil {
		return nil, fmt.Errorf("weather sampler: get weather: %w", err)
	}
	if w == nil {
		return nil, fmt.Errorf("weather sampler: weather data is nil")
	}

	radarActive := 0
	radarFrameAgeS := 0
	r, err := s.service.GetRadar(ctx)
	if err != nil {
		s.logger.Warn("weather sampler: failed to fetch radar, recording weather without radar", "err", err)
	} else if r != nil && len(r.PastFrames) > 0 {
		latestFrame := r.PastFrames[len(r.PastFrames)-1]
		if latestFrame.Time > 0 {
			radarActive = 1
			ageSec := int(time.Now().Unix() - latestFrame.Time)
			if ageSec >= 0 {
				radarFrameAgeS = ageSec
			}
		}
	}

	observedAt := w.UpdatedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	hashInput := fmt.Sprintf("%.2f:%d:%.2f:%d:%d:%d:%s",
		w.RainMm,
		w.WeatherCode,
		w.Temperature,
		w.Humidity,
		radarActive,
		radarFrameAgeS,
		observedAt.UTC().Format(time.RFC3339),
	)
	hash := sha256.Sum256([]byte(hashInput))
	payloadHash := hex.EncodeToString(hash[:])

	record := ObservationRecord{
		PrecipitationMm: w.RainMm,
		WeatherCode:     w.WeatherCode,
		TemperatureC:    w.Temperature,
		HumidityPct:     float64(w.Humidity),
		RadarActive:     radarActive,
		RadarFrameAgeS:  radarFrameAgeS,
		Source:          "open-meteo+rainviewer",
		PayloadHash:     payloadHash,
		ObservedAt:      observedAt,
		RecordedAt:      time.Now().UTC(),
	}

	if err := s.persister.RecordObservation(ctx, record); err != nil {
		return nil, fmt.Errorf("weather sampler: persist: %w", err)
	}

	return &record, nil
}

// Start runs periodic sampling on the specified interval (default 10 minutes).
// Executes an initial sample immediately, then loops until context cancellation.
func (s *Sampler) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}

	s.logger.Info("weather sampler started", "interval", interval.String())

	if _, err := s.SampleOnce(ctx); err != nil {
		s.logger.Warn("weather sampler initial sample failed", "err", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("weather sampler stopped")
			return
		case <-ticker.C:
			if _, err := s.SampleOnce(ctx); err != nil {
				s.logger.Warn("weather sampler tick failed", "err", err)
			}
		}
	}
}
