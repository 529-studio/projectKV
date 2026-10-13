package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CapacityRecord represents a learned peak vehicle capacity record for a single camera.
type CapacityRecord struct {
	CameraID   string
	LearnedMax float64
	UpdatedAt  time.Time
}

// CapacityRepo provides persistent storage for learned camera capacities
// using hand-written SQL (no ORM — per docs/GO_BACKEND_ARCHITECTURE.md §1).
type CapacityRepo struct {
	db *sql.DB
}

// NewCapacityRepo constructs a CapacityRepo backed by db.
func NewCapacityRepo(db *sql.DB) *CapacityRepo {
	return &CapacityRepo{db: db}
}

// Save upserts the learned peak capacity for cameraID into camera_capacities.
// If cameraID exists, learned_max and updated_at are updated.
func (r *CapacityRepo) Save(ctx context.Context, cameraID string, learnedMax float64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("capacity repo db is nil")
	}

	query := fmt.Sprintf(`INSERT INTO %s (camera_id, learned_max, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(camera_id) DO UPDATE SET
			learned_max = excluded.learned_max,
			updated_at  = CURRENT_TIMESTAMP`, TableCameraCapacities)

	_, err := r.db.ExecContext(ctx, query, cameraID, learnedMax)
	if err != nil {
		return fmt.Errorf("save camera capacity %s: %w", cameraID, err)
	}
	return nil
}

// GetAll returns a map of all learned camera capacities keyed by camera_id.
func (r *CapacityRepo) GetAll(ctx context.Context) (map[string]float64, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("capacity repo db is nil")
	}

	query := fmt.Sprintf(`SELECT camera_id, learned_max FROM %s`, TableCameraCapacities)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all camera capacities: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	capacities := make(map[string]float64)
	for rows.Next() {
		var (
			camID      string
			learnedMax float64
		)
		if err := rows.Scan(&camID, &learnedMax); err != nil {
			return nil, fmt.Errorf("scan camera capacity: %w", err)
		}
		capacities[camID] = learnedMax
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("camera capacities iteration: %w", err)
	}
	return capacities, nil
}

// Get returns the learned peak capacity for a specific cameraID, or sql.ErrNoRows if not found.
func (r *CapacityRepo) Get(ctx context.Context, cameraID string) (float64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("capacity repo db is nil")
	}

	query := fmt.Sprintf(`SELECT learned_max FROM %s WHERE camera_id = ?`, TableCameraCapacities)
	var learnedMax float64
	err := r.db.QueryRowContext(ctx, query, cameraID).Scan(&learnedMax)
	if err != nil {
		return 0, fmt.Errorf("get camera capacity %s: %w", cameraID, err)
	}
	return learnedMax, nil
}
