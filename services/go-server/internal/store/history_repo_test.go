package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/store"
)

func openTestHistoryDB(t *testing.T) (*store.VerdictHistoryRepo, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_history.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open sqlite with migrations: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return store.NewVerdictHistoryRepo(db), db
}

func TestVerdictHistoryRepo_RecordPersistsUTCTimestamp(t *testing.T) {
	repo, db := openTestHistoryDB(t)
	ctx := context.Background()

	observed := time.Date(2026, time.November, 5, 7, 30, 15, 123456789, time.FixedZone("ICT", 7*60*60))
	h := store.VerdictHistory{
		CameraID:        "cam-vo-van-kiet",
		PCUTotal:        18.5,
		SaturationIndex: 0.42,
		Severity:        "MODERATE",
		Confidence:      0.91,
		Source:          "onnx",
		ObservedAt:      observed,
	}
	if err := repo.Record(ctx, h); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var (
		gotCamera string
		gotPCU    float64
		gotSI     float64
		gotSev    string
		gotConf   float64
		gotSrc    string
		gotObs    string
		gotRec    string
	)
	row := db.QueryRow(`SELECT camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at
		FROM hotset_verdict_history`)
	if err := row.Scan(&gotCamera, &gotPCU, &gotSI, &gotSev, &gotConf, &gotSrc, &gotObs, &gotRec); err != nil {
		t.Fatalf("scan history row: %v", err)
	}
	if gotCamera != h.CameraID || gotSev != h.Severity || gotSrc != h.Source {
		t.Errorf("unexpected identity fields: camera=%q severity=%q source=%q", gotCamera, gotSev, gotSrc)
	}
	if gotPCU != h.PCUTotal || gotSI != h.SaturationIndex || gotConf != h.Confidence {
		t.Errorf("unexpected numeric fields: pcu=%f si=%f confidence=%f", gotPCU, gotSI, gotConf)
	}
	wantObs := observed.UTC().Format(time.RFC3339Nano)
	if gotObs != wantObs {
		t.Errorf("expected observed_at %q, got %q", wantObs, gotObs)
	}
	if _, err := time.Parse(time.RFC3339Nano, gotRec); err != nil {
		t.Errorf("recorded_at is not RFC3339Nano: %q (%v)", gotRec, err)
	}
}

func TestVerdictHistoryRepo_DuplicateObservationIsIgnored(t *testing.T) {
	repo, db := openTestHistoryDB(t)
	ctx := context.Background()
	observed := time.Date(2026, time.November, 5, 8, 0, 0, 0, time.UTC)

	first := store.VerdictHistory{
		CameraID:        "cam-1",
		PCUTotal:        10,
		SaturationIndex: 0.20,
		Severity:        "CLEAR",
		Confidence:      0.80,
		Source:          "onnx",
		ObservedAt:      observed,
	}
	second := first
	second.PCUTotal = 99
	second.SaturationIndex = 0.99
	second.Severity = "CONGESTED"

	if err := repo.Record(ctx, first); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := repo.Record(ctx, second); err != nil {
		t.Fatalf("second Record: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hotset_verdict_history`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after duplicate record, got %d", count)
	}
	var severity string
	if err := db.QueryRow(`SELECT severity FROM hotset_verdict_history`).Scan(&severity); err != nil {
		t.Fatalf("select severity: %v", err)
	}
	if severity != "CLEAR" {
		t.Errorf("expected first write to win, got severity %q", severity)
	}
}

func TestVerdictHistoryRepo_RejectsInvalidObservation(t *testing.T) {
	repo, _ := openTestHistoryDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		h    store.VerdictHistory
	}{
		{
			name: "empty camera id",
			h: store.VerdictHistory{
				Severity:   "CLEAR",
				ObservedAt: time.Now().UTC(),
			},
		},
		{
			name: "empty severity",
			h: store.VerdictHistory{
				CameraID:   "cam-1",
				ObservedAt: time.Now().UTC(),
			},
		},
		{
			name: "zero observed at",
			h: store.VerdictHistory{
				CameraID: "cam-1",
				Severity: "CLEAR",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.Record(ctx, tc.h); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestVerdictHistoryRepo_GetRecentByCamera(t *testing.T) {
	repo, _ := openTestHistoryDB(t)
	ctx := context.Background()

	baseTime := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

	// Seed 5 records for cam-test-1 (spaced by 5 minutes)
	for i := 5; i >= 1; i-- {
		obs := baseTime.Add(-time.Duration(i*5) * time.Minute)
		h := store.VerdictHistory{
			CameraID:        "cam-test-1",
			PCUTotal:        float64(i * 10),
			SaturationIndex: float64(i) * 0.15,
			Severity:        "MODERATE",
			Confidence:      0.85,
			Source:          "test",
			ObservedAt:      obs,
		}
		if err := repo.Record(ctx, h); err != nil {
			t.Fatalf("seed record %d: %v", i, err)
		}
	}

	// Seed 1 record for cam-test-2
	if err := repo.Record(ctx, store.VerdictHistory{
		CameraID:        "cam-test-2",
		PCUTotal:        50.0,
		SaturationIndex: 0.90,
		Severity:        "CONGESTED",
		Confidence:      0.99,
		Source:          "test",
		ObservedAt:      baseTime.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("seed cam-test-2: %v", err)
	}

	t.Run("queries by camera and window with descending order", func(t *testing.T) {
		// Window 16m should capture -5m, -10m, -15m (3 records)
		rows, err := repo.GetRecentByCameraBefore(ctx, "cam-test-1", baseTime, 16*time.Minute, 10)
		if err != nil {
			t.Fatalf("GetRecentByCameraBefore: %v", err)
		}
		if len(rows) != 3 {
			t.Fatalf("expected 3 records within 16m window, got %d", len(rows))
		}

		// Verify descending order: -5m first, then -10m, then -15m
		wantTimes := []time.Time{
			baseTime.Add(-5 * time.Minute),
			baseTime.Add(-10 * time.Minute),
			baseTime.Add(-15 * time.Minute),
		}
		for idx, row := range rows {
			if !row.ObservedAt.Equal(wantTimes[idx]) {
				t.Errorf("row %d: expected time %v, got %v", idx, wantTimes[idx], row.ObservedAt)
			}
			if row.CameraID != "cam-test-1" {
				t.Errorf("row %d: expected camera cam-test-1, got %s", idx, row.CameraID)
			}
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		rows, err := repo.GetRecentByCameraBefore(ctx, "cam-test-1", baseTime, 30*time.Minute, 2)
		if err != nil {
			t.Fatalf("GetRecentByCameraBefore with limit: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("expected 2 records with limit=2, got %d", len(rows))
		}
		if !rows[0].ObservedAt.Equal(baseTime.Add(-5 * time.Minute)) {
			t.Errorf("expected newest row first, got %v", rows[0].ObservedAt)
		}
	})

	t.Run("window <= 0 retrieves all history up to before", func(t *testing.T) {
		rows, err := repo.GetRecentByCameraBefore(ctx, "cam-test-1", baseTime, 0, 50)
		if err != nil {
			t.Fatalf("GetRecentByCameraBefore window 0: %v", err)
		}
		if len(rows) != 5 {
			t.Fatalf("expected all 5 records, got %d", len(rows))
		}
	})

	t.Run("returns empty slice for unknown camera", func(t *testing.T) {
		rows, err := repo.GetRecentByCameraBefore(ctx, "cam-unknown", baseTime, 1*time.Hour, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("expected 0 rows for unknown camera, got %d", len(rows))
		}
	})

	t.Run("rejects empty camera id", func(t *testing.T) {
		_, err := repo.GetRecentByCameraBefore(ctx, "", baseTime, 1*time.Hour, 10)
		if err == nil {
			t.Fatal("expected error for empty camera id, got nil")
		}
	})

	t.Run("GetRecentByCamera queries relative to now", func(t *testing.T) {
		now := time.Now().UTC()
		err := repo.Record(ctx, store.VerdictHistory{
			CameraID:        "cam-live",
			PCUTotal:        25.0,
			SaturationIndex: 0.55,
			Severity:        "MODERATE",
			Confidence:      0.90,
			Source:          "test",
			ObservedAt:      now.Add(-1 * time.Minute),
		})
		if err != nil {
			t.Fatalf("record cam-live: %v", err)
		}

		rows, err := repo.GetRecentByCamera(ctx, "cam-live", 5*time.Minute, 10)
		if err != nil {
			t.Fatalf("GetRecentByCamera: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 recent row for cam-live, got %d", len(rows))
		}
		if rows[0].CameraID != "cam-live" {
			t.Errorf("expected cam-live, got %s", rows[0].CameraID)
		}
	})

	t.Run("skips NULL saturation index rows", func(t *testing.T) {
		repo, db := openTestHistoryDB(t)
		now := time.Now().UTC()

		// Manually insert row with NULL saturation_index
		_, err := db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`,
			"cam-null", 10.0, "UNKNOWN", 0.0, "test",
			now.Add(-2*time.Minute).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
		if err != nil {
			t.Fatalf("insert null SI row: %v", err)
		}

		// Also insert row with valid saturation index
		err = repo.Record(ctx, store.VerdictHistory{
			CameraID:        "cam-null",
			PCUTotal:        20.0,
			SaturationIndex: 0.40,
			Severity:        "MODERATE",
			Confidence:      0.80,
			Source:          "test",
			ObservedAt:      now.Add(-1 * time.Minute),
		})
		if err != nil {
			t.Fatalf("record valid row: %v", err)
		}

		rows, err := repo.GetRecentByCamera(ctx, "cam-null", 10*time.Minute, 10)
		if err != nil {
			t.Fatalf("GetRecentByCamera: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected exactly 1 row (NULL SI skipped), got %d", len(rows))
		}
		if rows[0].SaturationIndex != 0.40 {
			t.Errorf("expected valid SI 0.40, got %v", rows[0].SaturationIndex)
		}
	})

	t.Run("null rows do not consume query limit", func(t *testing.T) {
		repo, db := openTestHistoryDB(t)
		now := time.Now().UTC()

		// Insert valid older row
		err := repo.Record(ctx, store.VerdictHistory{
			CameraID:        "cam-limit-null",
			PCUTotal:        25.0,
			SaturationIndex: 0.50,
			Severity:        "MODERATE",
			Confidence:      0.85,
			Source:          "test",
			ObservedAt:      now.Add(-5 * time.Minute),
		})
		if err != nil {
			t.Fatalf("record valid row: %v", err)
		}

		// Insert 3 newer rows with NULL saturation_index
		for i := 1; i <= 3; i++ {
			_, err := db.Exec(`INSERT INTO hotset_verdict_history
				(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
				VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`,
				"cam-limit-null", 10.0, "UNKNOWN", 0.0, "test",
				now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339Nano),
				now.Format(time.RFC3339Nano),
			)
			if err != nil {
				t.Fatalf("insert null SI row %d: %v", i, err)
			}
		}

		// With LIMIT 1, SQL-level filter must ensure the valid row is returned
		rows, err := repo.GetRecentByCamera(ctx, "cam-limit-null", 10*time.Minute, 1)
		if err != nil {
			t.Fatalf("GetRecentByCamera: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 valid row returned despite newer NULL rows, got %d", len(rows))
		}
		if rows[0].SaturationIndex != 0.50 {
			t.Errorf("expected SI 0.50, got %v", rows[0].SaturationIndex)
		}
	})
}
