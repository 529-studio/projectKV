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
// camera_id already exists, severity, confidence, source, observed_at and updated_at are updated.
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

	query := fmt.Sprintf(`INSERT INTO %s (camera_id, severity, flood_severity, confidence, source, observed_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(camera_id) DO UPDATE SET
		 	severity       = excluded.severity,
		 	flood_severity = excluded.flood_severity,
		 	confidence     = excluded.confidence,
		 	source         = excluded.source,
		 	observed_at    = excluded.observed_at,
		 	updated_at     = excluded.updated_at`, TableCameraVerdicts)

	_, err := r.db.ExecContext(ctx, query, v.CameraID, v.Severity, v.FloodSeverity, v.Confidence, v.Source, observedAt, updatedAt)
	if err != nil {
		return fmt.Errorf("save verdict %s: %w", v.CameraID, err)
	}
	return nil
}

// GetLatest returns the most recent verdict for every camera currently in the
// camera_verdicts table, ordered by updated_at descending.
func (r *VerdictRepo) GetLatest(ctx context.Context) ([]Verdict, error) {
	query := fmt.Sprintf(`SELECT camera_id, severity, COALESCE(flood_severity, ''), confidence, COALESCE(source, ''), COALESCE(observed_at, updated_at), updated_at
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
		if err := rows.Scan(&v.CameraID, &v.Severity, &v.FloodSeverity, &v.Confidence, &v.Source, &rawObs, &v.Timestamp); err != nil {
			return nil, fmt.Errorf("scan verdict: %w", err)
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
