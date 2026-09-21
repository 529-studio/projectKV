package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/store"
	_ "modernc.org/sqlite"
)

// openTestDB creates a fresh in-memory SQLite DB and runs the schema DDL
// needed for VerdictRepo tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS camera_verdicts (
			camera_id   TEXT PRIMARY KEY,
			severity    TEXT NOT NULL,
			confidence  REAL NOT NULL,
			updated_at  TIMESTAMP NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ── Save tests ───────────────────────────────────────────────────────────────

func TestVerdictRepo_Save_InsertsRow(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)
	ctx := context.Background()

	v := store.Verdict{
		CameraID:   "cam1",
		Severity:   "HEAVY",
		Confidence: 0.91,
		Timestamp:  time.Now().UTC().Truncate(time.Second),
	}
	if err := repo.Save(ctx, v); err != nil {
		t.Fatalf("Save: %v", err)
	}

	verdicts, err := repo.GetLatest()
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("expected 1 verdict, got %d", len(verdicts))
	}
	got := verdicts[0]
	if got.CameraID != v.CameraID {
		t.Errorf("CameraID mismatch: want %q, got %q", v.CameraID, got.CameraID)
	}
	if got.Severity != v.Severity {
		t.Errorf("Severity mismatch: want %q, got %q", v.Severity, got.Severity)
	}
	if got.Confidence != v.Confidence {
		t.Errorf("Confidence mismatch: want %f, got %f", v.Confidence, got.Confidence)
	}
}

func TestVerdictRepo_Save_UpsertUpdatesExisting(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)
	ctx := context.Background()

	v1 := store.Verdict{CameraID: "camX", Severity: "CLEAR", Confidence: 0.5, Timestamp: time.Now().UTC()}
	if err := repo.Save(ctx, v1); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	v2 := store.Verdict{CameraID: "camX", Severity: "CONGESTED", Confidence: 0.99, Timestamp: time.Now().UTC()}
	if err := repo.Save(ctx, v2); err != nil {
		t.Fatalf("second Save (upsert): %v", err)
	}

	verdicts, err := repo.GetLatest()
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("upsert must keep only 1 row, got %d", len(verdicts))
	}
	if verdicts[0].Severity != "CONGESTED" {
		t.Errorf("severity must be updated to CONGESTED, got %q", verdicts[0].Severity)
	}
	if verdicts[0].Confidence != 0.99 {
		t.Errorf("confidence must be updated to 0.99, got %f", verdicts[0].Confidence)
	}
}

// ── GetLatest tests ──────────────────────────────────────────────────────────

func TestVerdictRepo_GetLatest_EmptyDB_ReturnsNil(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)

	verdicts, err := repo.GetLatest()
	if err != nil {
		t.Fatalf("GetLatest on empty DB: %v", err)
	}
	if len(verdicts) != 0 {
		t.Errorf("expected 0 verdicts from empty DB, got %d", len(verdicts))
	}
}

func TestVerdictRepo_GetLatest_MultipleCameras(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)
	ctx := context.Background()

	cameras := []store.Verdict{
		{CameraID: "a", Severity: "CLEAR", Confidence: 0.8, Timestamp: time.Now().UTC()},
		{CameraID: "b", Severity: "HEAVY", Confidence: 0.7, Timestamp: time.Now().UTC()},
		{CameraID: "c", Severity: "CONGESTED", Confidence: 0.95, Timestamp: time.Now().UTC()},
	}
	for _, v := range cameras {
		if err := repo.Save(ctx, v); err != nil {
			t.Fatalf("Save %s: %v", v.CameraID, err)
		}
	}

	got, err := repo.GetLatest()
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 verdicts, got %d", len(got))
	}
}

func TestVerdictRepo_GetLatest_OrderedByUpdatedAtDesc(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)
	ctx := context.Background()

	t0 := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	t1 := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second)
	t2 := time.Now().UTC().Truncate(time.Second)

	_ = repo.Save(ctx, store.Verdict{CameraID: "old", Severity: "CLEAR", Confidence: 0.5, Timestamp: t0})
	_ = repo.Save(ctx, store.Verdict{CameraID: "mid", Severity: "HEAVY", Confidence: 0.7, Timestamp: t1})
	_ = repo.Save(ctx, store.Verdict{CameraID: "new", Severity: "CONGESTED", Confidence: 0.9, Timestamp: t2})

	got, err := repo.GetLatest()
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// Most recent first
	if got[0].CameraID != "new" {
		t.Errorf("expected newest first, got %q", got[0].CameraID)
	}
	if got[2].CameraID != "old" {
		t.Errorf("expected oldest last, got %q", got[2].CameraID)
	}
}
