package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FloodHistoryRecord represents one immutable time-series flood observation for a camera.
type FloodHistoryRecord struct {
	CameraID             string
	FloodDepthCm         *float64
	FloodHazardLevel     *int
	FloodSeverity        string
	FloodDepthConfidence string
	FloodCalibrationRef  string
	ObservedAt           time.Time
}

// FloodHistoryRepo provides append-only recording and range querying of flood observation history.
type FloodHistoryRepo struct {
	db *sql.DB
}

// NewFloodHistoryRepo constructs a new FloodHistoryRepo backed by db.
func NewFloodHistoryRepo(db *sql.DB) *FloodHistoryRepo {
	return &FloodHistoryRepo{db: db}
}

// Record inserts one flood observation into flood_verdict_history.
// Duplicate (camera_id, observed_at) entries are ignored idempotently.
func (r *FloodHistoryRepo) Record(ctx context.Context, h FloodHistoryRecord) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("flood history repo db is nil")
	}
	if strings.TrimSpace(h.CameraID) == "" {
		return fmt.Errorf("flood history camera id is empty")
	}
	observedAt := h.ObservedAt
	if observedAt.IsZero() {
		return fmt.Errorf("flood history observed_at is zero for camera %s", h.CameraID)
	}

	query := fmt.Sprintf(`INSERT INTO %s
		(camera_id, flood_depth_cm, flood_hazard_level, flood_severity, flood_depth_confidence, flood_calibration_ref, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(camera_id, observed_at) DO NOTHING`, TableFloodVerdictHistory)

	_, err := r.db.ExecContext(ctx, query,
		h.CameraID,
		nullableFloat(h.FloodDepthCm),
		nullableInt(h.FloodHazardLevel),
		h.FloodSeverity,
		h.FloodDepthConfidence,
		h.FloodCalibrationRef,
		observedAt.UTC().Format(TimestampFixedLayout),
		time.Now().UTC().Format(TimestampFixedLayout),
	)
	if err != nil {
		return fmt.Errorf("record flood history %s: %w", h.CameraID, err)
	}
	return nil
}

// GetRecentByCameraIDs retrieves flood observations for a set of cameras within [start, end],
// ordered ascending by observed_at (chronological).
func (r *FloodHistoryRepo) GetRecentByCameraIDs(ctx context.Context, cameraIDs []string, start, end time.Time) ([]FloodHistoryRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("flood history repo db is nil")
	}
	if len(cameraIDs) == 0 {
		return []FloodHistoryRecord{}, nil
	}

	placeholders := make([]string, len(cameraIDs))
	args := make([]interface{}, len(cameraIDs)+2)
	for i, id := range cameraIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	args[len(cameraIDs)] = start.UTC().Format(TimestampFixedLayout)
	args[len(cameraIDs)+1] = end.UTC().Format(TimestampFixedLayout)

	query := fmt.Sprintf(`SELECT camera_id, flood_depth_cm, flood_hazard_level, COALESCE(flood_severity, ''),
			COALESCE(flood_depth_confidence, ''), COALESCE(flood_calibration_ref, ''), observed_at
		FROM %s
		WHERE camera_id IN (%s) AND observed_at >= ? AND observed_at <= ?
		ORDER BY observed_at ASC`, TableFloodVerdictHistory, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query flood history: %w", err)
	}
	defer rows.Close()

	var records []FloodHistoryRecord
	for rows.Next() {
		var rec FloodHistoryRecord
		var rawObserved string
		var depth sql.NullFloat64
		var hazard sql.NullInt64

		if err := rows.Scan(
			&rec.CameraID,
			&depth,
			&hazard,
			&rec.FloodSeverity,
			&rec.FloodDepthConfidence,
			&rec.FloodCalibrationRef,
			&rawObserved,
		); err != nil {
			return nil, fmt.Errorf("scan flood history row: %w", err)
		}

		if depth.Valid {
			v := depth.Float64
			rec.FloodDepthCm = &v
		}
		if hazard.Valid {
			v := int(hazard.Int64)
			rec.FloodHazardLevel = &v
		}

		parsed, err := time.Parse(time.RFC3339Nano, rawObserved)
		if err == nil {
			rec.ObservedAt = parsed.UTC()
		} else if parsed, err := time.Parse("2006-01-02 15:04:05", rawObserved); err == nil {
			rec.ObservedAt = parsed.UTC()
		} else {
			rec.ObservedAt = start.UTC()
		}

		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flood history rows: %w", err)
	}

	return records, nil
}
