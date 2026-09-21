package store

import (
	"context"
	"database/sql"
	"time"
)

type Verdict struct {
	CameraID   string
	Severity   string
	Confidence float64
	Timestamp  time.Time
}

type VerdictRepo struct {
	db *sql.DB
}

func NewVerdictRepo(db *sql.DB) *VerdictRepo {
	return &VerdictRepo{db: db}
}

func (r *VerdictRepo) Save(ctx context.Context, v Verdict) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO camera_verdicts (camera_id, severity, confidence, updated_at) 
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(camera_id) DO UPDATE SET 
			severity=excluded.severity, 
			confidence=excluded.confidence, 
			updated_at=excluded.updated_at`,
		v.CameraID, v.Severity, v.Confidence, v.Timestamp,
	)
	return err
}
