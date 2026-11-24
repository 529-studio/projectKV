package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/store"
)

func openTestFloodHistoryDB(t *testing.T) (*store.FloodHistoryRepo, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_flood_history.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open sqlite with migrations: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return store.NewFloodHistoryRepo(db), db
}

func TestFloodHistoryRepo_RecordAndQuery(t *testing.T) {
	repo, _ := openTestFloodHistoryDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Minute)
	t1 := now.Add(-2 * time.Hour)
	t2 := now.Add(-1 * time.Hour)
	t3 := now

	depth1 := 14.5
	hazard1 := 1
	depth2 := 28.0
	hazard2 := 2

	recs := []store.FloodHistoryRecord{
		{
			CameraID:             "cam-1",
			FloodDepthCm:         &depth1,
			FloodHazardLevel:     &hazard1,
			FloodSeverity:        "FLOOD_CAUTION",
			FloodDepthConfidence: "MEASURED",
			FloodCalibrationRef:  "CURB_15CM",
			ObservedAt:           t1,
		},
		{
			CameraID:             "cam-2",
			FloodDepthCm:         &depth2,
			FloodHazardLevel:     &hazard2,
			FloodSeverity:        "FLOOD_IMPASSABLE",
			FloodDepthConfidence: "MEASURED",
			FloodCalibrationRef:  "MOTORCYCLE_110CM",
			ObservedAt:           t2,
		},
		{
			CameraID:             "cam-3",
			FloodDepthCm:         nil, // unmeasured
			FloodHazardLevel:     nil,
			FloodSeverity:        "",
			FloodDepthConfidence: "UNMEASURED",
			FloodCalibrationRef:  "NONE",
			ObservedAt:           t3,
		},
	}

	for _, r := range recs {
		if err := repo.Record(ctx, r); err != nil {
			t.Fatalf("Record failed for %s: %v", r.CameraID, err)
		}
	}

	// Query for cam-1 and cam-2 in [t1, t2]
	results, err := repo.GetRecentByCameraIDs(ctx, []string{"cam-1", "cam-2"}, t1.Add(-1*time.Minute), t2.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("GetRecentByCameraIDs failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 records, got %d", len(results))
	}

	if results[0].CameraID != "cam-1" || *results[0].FloodDepthCm != 14.5 {
		t.Errorf("unexpected record 0: %+v", results[0])
	}
	if results[1].CameraID != "cam-2" || *results[1].FloodDepthCm != 28.0 {
		t.Errorf("unexpected record 1: %+v", results[1])
	}
}

func TestFloodHistoryRepo_IdempotentOnConflict(t *testing.T) {
	repo, db := openTestFloodHistoryDB(t)
	ctx := context.Background()

	observed := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	depth := 10.0
	r := store.FloodHistoryRecord{
		CameraID:     "cam-dup",
		FloodDepthCm: &depth,
		ObservedAt:   observed,
	}

	if err := repo.Record(ctx, r); err != nil {
		t.Fatalf("first Record failed: %v", err)
	}

	// Re-record identical observation
	if err := repo.Record(ctx, r); err != nil {
		t.Fatalf("second duplicate Record failed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM flood_verdict_history WHERE camera_id = 'cam-dup'").Scan(&count); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1 after duplicate record, got %d", count)
	}
}

func TestFloodHistoryRepo_EmptyCameraIDs(t *testing.T) {
	repo, _ := openTestFloodHistoryDB(t)
	ctx := context.Background()

	results, err := repo.GetRecentByCameraIDs(ctx, []string{}, time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Fatalf("unexpected error for empty cameraIDs: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
