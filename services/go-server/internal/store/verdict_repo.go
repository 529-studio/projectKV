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
	CameraID   string
	Severity   string
	Confidence float64
	Timestamp  time.Time
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
// camera_id already exists, severity, confidence and updated_at are updated.
func (r *VerdictRepo) Save(ctx context.Context, v Verdict) error {
	query := fmt.Sprintf(`INSERT INTO %s (camera_id, severity, confidence, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(camera_id) DO UPDATE SET
		 	severity   = excluded.severity,
		 	confidence = excluded.confidence,
		 	updated_at = excluded.updated_at`, TableCameraVerdicts)

	_, err := r.db.ExecContext(ctx, query, v.CameraID, v.Severity, v.Confidence, v.Timestamp)
	if err != nil {
		return fmt.Errorf("save verdict %s: %w", v.CameraID, err)
	}
	return nil
}

// GetLatest returns the most recent verdict for every camera currently in the
// camera_verdicts table, ordered by updated_at descending.
func (r *VerdictRepo) GetLatest() ([]Verdict, error) {
	query := fmt.Sprintf(`SELECT camera_id, severity, confidence, updated_at
		 FROM %s
		 ORDER BY updated_at DESC`, TableCameraVerdicts)

	rows, err := r.db.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query verdicts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Verdict
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.CameraID, &v.Severity, &v.Confidence, &v.Timestamp); err != nil {
			return nil, fmt.Errorf("scan verdict: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return out, nil
}
