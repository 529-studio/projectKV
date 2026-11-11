package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Verdict is the store-layer representation of an AI analysis result for a
// single camera.  It maps 1-to-1 with the camera_verdicts table.
type Verdict struct {
	CameraID      string
	Severity      string
	FloodSeverity string
	Confidence    float64
	Source        string
	ObservedAt    time.Time
	Timestamp     time.Time

	// Physical hazard layer (Issue #401). Pointers preserve the AC13 distinction:
	// nil is "never measured", 0.0 is "measured dry". A pre-migration row reads
	// back nil for both pointers and empty strings for the vocabularies.
	FloodDepthCm         *float64
	FloodHazardLevel     *int
	FloodHazardBasis     string
	FloodDepthConfidence string
	// FloodCalibrationRef is the §2.1 reference ladder string (MOTORCYCLE_110CM,
	// CURB_15CM, CAMERA_OVERRIDE, NONE). It is persisted verbatim so a restart
	// rehydrates the same ladder claim the engine published with the level.
	FloodCalibrationRef string
}

// VerdictRepo provides persistence for camera analysis verdicts using raw SQL
// (no ORM — per docs/GO_BACKEND_ARCHITECTURE.md §1).
type VerdictRepo struct {
	db *sql.DB
}

// NewVerdictRepo constructs a VerdictRepo backed by db.
func NewVerdictRepo(db *sql.DB) *VerdictRepo {
	return &VerdictRepo{db: db}
}

// Save upserts a single verdict into camera_verdicts.  If a row for
// camera_id already exists, severity, confidence, source, observed_at,
// updated_at and the physical hazard fields are updated.
func (r *VerdictRepo) Save(ctx context.Context, v Verdict) error {
	observedAt := v.ObservedAt
	if observedAt.IsZero() {
		observedAt = v.Timestamp
	}
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	updatedAt := v.Timestamp
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	query := fmt.Sprintf(`INSERT INTO %s (camera_id, severity, flood_severity, confidence, source, observed_at, updated_at,
			flood_depth_cm, flood_hazard_level, flood_hazard_basis, flood_depth_confidence, flood_calibration_ref)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(camera_id) DO UPDATE SET
		 	severity                = excluded.severity,
		 	flood_severity          = excluded.flood_severity,
		 	confidence              = excluded.confidence,
		 	source                  = excluded.source,
		 	observed_at             = excluded.observed_at,
		 	updated_at              = excluded.updated_at,
		 	flood_depth_cm          = excluded.flood_depth_cm,
		 	flood_hazard_level      = excluded.flood_hazard_level,
		 	flood_hazard_basis      = excluded.flood_hazard_basis,
		 	flood_depth_confidence  = excluded.flood_depth_confidence,
		 	flood_calibration_ref   = excluded.flood_calibration_ref`, TableCameraVerdicts)

	_, err := r.db.ExecContext(ctx, query,
		v.CameraID,
		v.Severity,
		v.FloodSeverity,
		v.Confidence,
		v.Source,
		observedAt,
		updatedAt,
		nullableFloat(v.FloodDepthCm),
		nullableInt(v.FloodHazardLevel),
		v.FloodHazardBasis,
		v.FloodDepthConfidence,
		v.FloodCalibrationRef,
	)
	if err != nil {
		return fmt.Errorf("save verdict %s: %w", v.CameraID, err)
	}
	return nil
}

// GetLatest returns the most recent verdict for every camera currently in the
// camera_verdicts table, ordered by updated_at descending.
func (r *VerdictRepo) GetLatest(ctx context.Context) ([]Verdict, error) {
	query := fmt.Sprintf(`SELECT camera_id, severity, COALESCE(flood_severity, ''), confidence, COALESCE(source, ''), COALESCE(observed_at, updated_at), updated_at,
			flood_depth_cm, flood_hazard_level, COALESCE(flood_hazard_basis, ''), COALESCE(flood_depth_confidence, ''), COALESCE(flood_calibration_ref, '')
		 FROM %s
		 ORDER BY updated_at DESC`, TableCameraVerdicts)

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query verdicts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Verdict
	for rows.Next() {
		var v Verdict
		var rawObs any
		var depthCm sql.NullFloat64
		var hazardLevel sql.NullInt64
		if err := rows.Scan(
			&v.CameraID,
			&v.Severity,
			&v.FloodSeverity,
			&v.Confidence,
			&v.Source,
			&rawObs,
			&v.Timestamp,
			&depthCm,
			&hazardLevel,
			&v.FloodHazardBasis,
			&v.FloodDepthConfidence,
			&v.FloodCalibrationRef,
		); err != nil {
			return nil, fmt.Errorf("scan verdict: %w", err)
		}
		if depthCm.Valid {
			value := depthCm.Float64
			v.FloodDepthCm = &value
		}
		if hazardLevel.Valid {
			value := int(hazardLevel.Int64)
			v.FloodHazardLevel = &value
		}
		v.ObservedAt = parseTime(rawObs)
		if v.ObservedAt.IsZero() {
			v.ObservedAt = v.Timestamp
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return out, nil
}

// nullableFloat maps a pointer to a driver value so an unmeasured depth is stored
// as SQL NULL rather than 0.0.
func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// nullableInt maps a pointer to a driver value so an unknown level is stored as
// SQL NULL rather than 0.
func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func parseTime(val any) time.Time {
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05.999999999Z07:00",
			"2006-01-02T15:04:05",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
