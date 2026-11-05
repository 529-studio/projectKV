package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// VerdictHistory is one append-only Hot Set observation for a camera. It maps
// 1-to-1 with a row in hotset_verdict_history. The store layer deliberately
// carries plain values instead of importing hotset, preserving the modulith
// dependency direction (internal/store is a leaf package).
type VerdictHistory struct {
	CameraID        string
	PCUTotal        float64
	SaturationIndex float64
	Severity        string
	Confidence      float64
	Source          string
	ObservedAt      time.Time
}

// VerdictHistoryRepo appends Hot Set verdicts to hotset_verdict_history using
// hand-written SQL (no ORM, per docs/GO_BACKEND_ARCHITECTURE.md).
type VerdictHistoryRepo struct {
	db *sql.DB
}

// NewVerdictHistoryRepo constructs a VerdictHistoryRepo backed by db.
func NewVerdictHistoryRepo(db *sql.DB) *VerdictHistoryRepo {
	return &VerdictHistoryRepo{db: db}
}

// TimestampFixedLayout is a fixed-width RFC3339 layout with 9-digit fractional seconds.
// Fixed width guarantees that SQLite's ASCII lexical sorting (`ORDER BY observed_at`)
// is strictly equivalent to chronological sorting, preventing variable-width RFC3339Nano anomalies.
const TimestampFixedLayout = "2006-01-02T15:04:05.000000000Z"

// Record inserts one observation. Duplicate (camera_id, observed_at) pairs are
// ignored so a restart, a retried fan-out, or a pre-seed cannot inflate the
// time series. Timestamps are bound as explicit RFC3339Nano UTC strings: the
// modernc.org/sqlite driver otherwise persists time.Time through its Go
// String() representation, which includes a monotonic-clock suffix and is not
// portable to the Python feature extractor or to SQLite date functions.
func (r *VerdictHistoryRepo) Record(ctx context.Context, h VerdictHistory) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("verdict history repo db is nil")
	}
	if h.CameraID == "" {
		return fmt.Errorf("verdict history camera id is empty")
	}
	if h.Severity == "" {
		return fmt.Errorf("verdict history severity is empty for camera %s", h.CameraID)
	}
	observedAt := h.ObservedAt
	if observedAt.IsZero() {
		return fmt.Errorf("verdict history observed_at is zero for camera %s", h.CameraID)
	}

	query := fmt.Sprintf(`INSERT INTO %s
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(camera_id, observed_at) DO NOTHING`, TableHotsetVerdictHistory)

	_, err := r.db.ExecContext(ctx, query,
		h.CameraID,
		h.PCUTotal,
		h.SaturationIndex,
		h.Severity,
		h.Confidence,
		h.Source,
		observedAt.UTC().Format(TimestampFixedLayout),
		time.Now().UTC().Format(TimestampFixedLayout),
	)
	if err != nil {
		return fmt.Errorf("record verdict history %s: %w", h.CameraID, err)
	}
	return nil
}

// GetRecentByCamera retrieves recent observations for cameraID within window prior to now,
// ordered descending by observed_at (newest first). If window <= 0, no lower time cutoff is applied.
// If limit <= 0, limit defaults to 2000. If limit > 10000, limit is capped at 10000.
func (r *VerdictHistoryRepo) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]VerdictHistory, error) {
	return r.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

// GetRecentByCameraBefore retrieves recent observations for cameraID observed at or before the given
// timestamp, within window prior to before. If window <= 0, no lower time cutoff is applied.
// If before is zero, time.Now().UTC() is used.
// Results are returned in reverse chronological order (newest first).
func (r *VerdictHistoryRepo) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]VerdictHistory, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("verdict history repo db is nil")
	}
	if cameraID == "" {
		return nil, fmt.Errorf("verdict history camera id is empty")
	}
	if before.IsZero() {
		before = time.Now().UTC()
	}
	if limit <= 0 {
		limit = 2000
	} else if limit > 10000 {
		limit = 10000
	}

	var query string
	var args []any

	if window > 0 {
		since := before.Add(-window)
		query = fmt.Sprintf(`SELECT camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at
			FROM %s
			WHERE camera_id = ? AND observed_at <= ? AND observed_at >= ? AND saturation_index IS NOT NULL
			ORDER BY observed_at DESC
			LIMIT ?`, TableHotsetVerdictHistory)
		args = []any{
			cameraID,
			before.UTC().Format(TimestampFixedLayout),
			since.UTC().Format(TimestampFixedLayout),
			limit,
		}
	} else {
		query = fmt.Sprintf(`SELECT camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at
			FROM %s
			WHERE camera_id = ? AND observed_at <= ? AND saturation_index IS NOT NULL
			ORDER BY observed_at DESC
			LIMIT ?`, TableHotsetVerdictHistory)
		args = []any{
			cameraID,
			before.UTC().Format(TimestampFixedLayout),
			limit,
		}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query verdict history for %s: %w", cameraID, err)
	}
	defer rows.Close()

	var results []VerdictHistory
	for rows.Next() {
		var (
			camID  string
			pcu    sql.NullFloat64
			si     sql.NullFloat64
			sev    string
			conf   float64
			src    string
			obsStr string
		)
		if err := rows.Scan(&camID, &pcu, &si, &sev, &conf, &src, &obsStr); err != nil {
			return nil, fmt.Errorf("scan verdict history row: %w", err)
		}

		// A missing or non-finite saturation index must never be interpreted as 0.0 (CLEAR)
		if !si.Valid || math.IsNaN(si.Float64) || math.IsInf(si.Float64, 0) {
			continue
		}

		obsTime, err := time.Parse(time.RFC3339Nano, obsStr)
		if err != nil {
			obsTime, err = time.Parse(time.RFC3339, obsStr)
			if err != nil {
				return nil, fmt.Errorf("parse observed_at timestamp %q: %w", obsStr, err)
			}
		}

		results = append(results, VerdictHistory{
			CameraID:        camID,
			PCUTotal:        pcu.Float64,
			SaturationIndex: si.Float64,
			Severity:        sev,
			Confidence:      conf,
			Source:          src,
			ObservedAt:      obsTime.UTC(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verdict history rows: %w", err)
	}

	return results, nil
}
