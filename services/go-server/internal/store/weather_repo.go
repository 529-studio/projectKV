package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrWeatherNotFound is returned when no weather observation matches the query criteria.
	ErrWeatherNotFound = errors.New("weather observation not found")
)

// WeatherObservation represents one append-only meteorological and radar snapshot.
type WeatherObservation struct {
	ID              int64     `json:"id"`
	PrecipitationMm float64   `json:"precipitation_mm"`
	WeatherCode     int       `json:"weather_code"`
	TemperatureC    float64   `json:"temperature_c"`
	HumidityPct     float64   `json:"humidity_pct"`
	RadarActive     int       `json:"radar_active"`
	RadarFrameAgeS  int       `json:"radar_frame_age_s"`
	Source          string    `json:"source"`
	PayloadHash     string    `json:"payload_hash"`
	ObservedAt      time.Time `json:"observed_at"`
	RecordedAt      time.Time `json:"recorded_at"`
}

// WeatherRepo provides access to persisted weather observations in SQLite.
type WeatherRepo struct {
	db *sql.DB
}

// NewWeatherRepo constructs a WeatherRepo backed by the given SQL database.
func NewWeatherRepo(db *sql.DB) *WeatherRepo {
	return &WeatherRepo{db: db}
}

// Record inserts a new weather observation into the append-only table.
// Repeated writes for the same source and observed_at are ignored idempotently.
func (r *WeatherRepo) Record(ctx context.Context, obs *WeatherObservation) error {
	if r.db == nil {
		return fmt.Errorf("weather repo: database handle is nil")
	}
	if obs == nil {
		return fmt.Errorf("weather repo: observation is nil")
	}

	recordedAt := obs.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	observedAtStr := obs.ObservedAt.UTC().Format(time.RFC3339Nano)
	recordedAtStr := recordedAt.UTC().Format(time.RFC3339Nano)

	query := `
		INSERT INTO weather_observations (
			precipitation_mm, weather_code, temperature_c, humidity_pct,
			radar_active, radar_frame_age_s, source, payload_hash,
			observed_at, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, observed_at) DO NOTHING
	`

	res, err := r.db.ExecContext(ctx, query,
		obs.PrecipitationMm,
		obs.WeatherCode,
		obs.TemperatureC,
		obs.HumidityPct,
		obs.RadarActive,
		obs.RadarFrameAgeS,
		obs.Source,
		obs.PayloadHash,
		observedAtStr,
		recordedAtStr,
	)
	if err != nil {
		return fmt.Errorf("weather repo: insert observation: %w", err)
	}

	id, err := res.LastInsertId()
	if err == nil && id > 0 {
		obs.ID = id
	}
	obs.RecordedAt = recordedAt
	return nil
}

// GetLatest returns the most recently observed weather observation.
func (r *WeatherRepo) GetLatest(ctx context.Context) (*WeatherObservation, error) {
	if r.db == nil {
		return nil, fmt.Errorf("weather repo: database handle is nil")
	}

	query := `
		SELECT id, precipitation_mm, weather_code, temperature_c, humidity_pct,
		       radar_active, radar_frame_age_s, source, payload_hash,
		       observed_at, recorded_at
		FROM weather_observations
		ORDER BY observed_at DESC, id DESC
		LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query)
	obs, err := scanWeatherObservation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWeatherNotFound
		}
		return nil, fmt.Errorf("weather repo: get latest: %w", err)
	}
	return obs, nil
}

// GetAsOf retrieves the closest weather observation at or before target time
// within the specified maxAge window. If no observation falls within [target-maxAge, target],
// it returns ErrWeatherNotFound (no forward fill, L67).
func (r *WeatherRepo) GetAsOf(ctx context.Context, target time.Time, maxAge time.Duration) (*WeatherObservation, error) {
	if r.db == nil {
		return nil, fmt.Errorf("weather repo: database handle is nil")
	}
	if maxAge < 0 {
		return nil, fmt.Errorf("weather repo: maxAge cannot be negative")
	}

	targetUTC := target.UTC()
	minTimeUTC := targetUTC.Add(-maxAge)

	targetStr := targetUTC.Format(time.RFC3339Nano)
	minTimeStr := minTimeUTC.Format(time.RFC3339Nano)

	query := `
		SELECT id, precipitation_mm, weather_code, temperature_c, humidity_pct,
		       radar_active, radar_frame_age_s, source, payload_hash,
		       observed_at, recorded_at
		FROM weather_observations
		WHERE observed_at <= ? AND observed_at >= ?
		ORDER BY observed_at DESC, id DESC
		LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query, targetStr, minTimeStr)
	obs, err := scanWeatherObservation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWeatherNotFound
		}
		return nil, fmt.Errorf("weather repo: get as-of: %w", err)
	}
	return obs, nil
}

// GetHistory retrieves weather observations recorded within the [since, until] interval.
func (r *WeatherRepo) GetHistory(ctx context.Context, since, until time.Time, limit int) ([]WeatherObservation, error) {
	if r.db == nil {
		return nil, fmt.Errorf("weather repo: database handle is nil")
	}
	if limit <= 0 {
		limit = 1000
	}

	sinceStr := since.UTC().Format(time.RFC3339Nano)
	untilStr := until.UTC().Format(time.RFC3339Nano)

	query := `
		SELECT id, precipitation_mm, weather_code, temperature_c, humidity_pct,
		       radar_active, radar_frame_age_s, source, payload_hash,
		       observed_at, recorded_at
		FROM weather_observations
		WHERE observed_at >= ? AND observed_at <= ?
		ORDER BY observed_at ASC, id ASC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, query, sinceStr, untilStr, limit)
	if err != nil {
		return nil, fmt.Errorf("weather repo: query history: %w", err)
	}
	defer rows.Close()

	var result []WeatherObservation
	for rows.Next() {
		obs, err := scanWeatherObservationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("weather repo: scan history row: %w", err)
		}
		result = append(result, *obs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("weather repo: rows err: %w", err)
	}

	return result, nil
}

// Count returns the total number of recorded weather observations.
func (r *WeatherRepo) Count(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("weather repo: database handle is nil")
	}
	var count int64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM weather_observations").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("weather repo: count: %w", err)
	}
	return count, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanWeatherObservation(s rowScanner) (*WeatherObservation, error) {
	var (
		obs           WeatherObservation
		observedAtStr string
		recordedAtStr string
	)

	err := s.Scan(
		&obs.ID,
		&obs.PrecipitationMm,
		&obs.WeatherCode,
		&obs.TemperatureC,
		&obs.HumidityPct,
		&obs.RadarActive,
		&obs.RadarFrameAgeS,
		&obs.Source,
		&obs.PayloadHash,
		&observedAtStr,
		&recordedAtStr,
	)
	if err != nil {
		return nil, err
	}

	obs.ObservedAt, _ = parseFlexibleTime(observedAtStr)
	obs.RecordedAt, _ = parseFlexibleTime(recordedAtStr)

	return &obs, nil
}

func scanWeatherObservationRow(rows *sql.Rows) (*WeatherObservation, error) {
	return scanWeatherObservation(rows)
}

func parseFlexibleTime(val string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", val); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", val); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.DateOnly, val)
}
