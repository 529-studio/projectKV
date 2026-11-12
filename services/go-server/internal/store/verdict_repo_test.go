package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
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
			camera_id      TEXT PRIMARY KEY,
			severity       TEXT NOT NULL,
			flood_severity TEXT NOT NULL DEFAULT '',
			confidence     REAL NOT NULL,
			source         TEXT NOT NULL DEFAULT '',
			observed_at    TIMESTAMP,
			updated_at     TIMESTAMP NOT NULL,
			-- Migration 0011 (Issue #401). Mirrors the production schema so the
			-- repository's hazard-column reads and writes are exercised, not skipped.
			flood_depth_cm          REAL,
			flood_hazard_level      INTEGER,
			flood_hazard_basis      TEXT NOT NULL DEFAULT '',
			flood_depth_confidence  TEXT NOT NULL DEFAULT '',
			flood_calibration_ref   TEXT NOT NULL DEFAULT ''
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

	now := time.Now().UTC().Truncate(time.Second)
	v := store.Verdict{
		CameraID:      "cam1",
		Severity:      "HEAVY",
		FloodSeverity: "FLOODED_SHALLOW",
		Confidence:    0.91,
		Source:        "vlm",
		ObservedAt:    now,
		Timestamp:     now,
	}
	if err := repo.Save(ctx, v); err != nil {
		t.Fatalf("Save: %v", err)
	}

	verdicts, err := repo.GetLatest(context.Background())
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
	if got.FloodSeverity != v.FloodSeverity {
		t.Errorf("FloodSeverity mismatch: want %q, got %q", v.FloodSeverity, got.FloodSeverity)
	}
	if got.Confidence != v.Confidence {
		t.Errorf("Confidence mismatch: want %f, got %f", v.Confidence, got.Confidence)
	}
	if got.Source != v.Source {
		t.Errorf("Source mismatch: want %q, got %q", v.Source, got.Source)
	}
	if !got.ObservedAt.Equal(v.ObservedAt) {
		t.Errorf("ObservedAt mismatch: want %v, got %v", v.ObservedAt, got.ObservedAt)
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

	verdicts, err := repo.GetLatest(context.Background())
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

	verdicts, err := repo.GetLatest(context.Background())
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

	got, err := repo.GetLatest(context.Background())
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

	got, err := repo.GetLatest(context.Background())
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

func TestVerdictRepo_ConcurrentSaves(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent_test.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	repo := store.NewVerdictRepo(db)
	workers := 20
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		camID := fmt.Sprintf("cam_%d", i)
		go func(id string) {
			defer wg.Done()
			v := store.Verdict{
				CameraID:   id,
				Severity:   "CONGESTED",
				Confidence: 0.95,
				Source:     "mock",
				Timestamp:  time.Now().UTC(),
			}
			if err := repo.Save(context.Background(), v); err != nil {
				errCh <- err
			}
		}(camID)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent save failed: %v", err)
	}

	latest, err := repo.GetLatest(context.Background())
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(latest) != workers {
		t.Errorf("expected %d saved verdicts, got %d", workers, len(latest))
	}
}

// ── Physical hazard layer tests (Issue #401, spec §5.4/§8.1) ─────────────────

// TestVerdictRepo_HazardRoundTrip covers the AC13 distinction that motivates the
// pointer fields: nil must survive as nil ("never measured"), a measured 0.0 must
// survive as 0.0 ("dry, verified"), and levels 1-3 must round-trip verbatim.
func TestVerdictRepo_HazardRoundTrip(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewVerdictRepo(db)
	ctx := context.Background()

	levelOne, levelTwo, levelThree := 1, 2, 3
	zero, shallow, deep := 0.0, 14.5, 24.2
	want := map[string]store.Verdict{
		"dry-measured": {
			CameraID:             "dry-measured",
			Severity:             "CLEAR",
			Confidence:           0.9,
			Timestamp:            time.Now().UTC().Truncate(time.Second),
			FloodDepthCm:         &zero,
			FloodHazardLevel:     &levelOne,
			FloodHazardBasis:     "DEPTH_MEASURED",
			FloodDepthConfidence: "MEASURED",
			FloodCalibrationRef:  "MOTORCYCLE_110CM",
		},
		"shallow-measured": {
			CameraID:             "shallow-measured",
			Severity:             "CLEAR",
			Confidence:           0.8,
			Timestamp:            time.Now().UTC().Truncate(time.Second),
			FloodDepthCm:         &shallow,
			FloodHazardLevel:     &levelTwo,
			FloodHazardBasis:     "DEPTH_MEASURED",
			FloodDepthConfidence: "MEASURED",
			FloodCalibrationRef:  "CURB_15CM",
		},
		"deep-inferred": {
			CameraID:             "deep-inferred",
			Severity:             "MODERATE",
			Confidence:           0.78,
			Timestamp:            time.Now().UTC().Truncate(time.Second),
			FloodDepthCm:         &deep,
			FloodHazardLevel:     &levelThree,
			FloodHazardBasis:     "AVOIDANCE_INFERRED",
			FloodDepthConfidence: "INFERRED",
			FloodCalibrationRef:  "CAMERA_OVERRIDE",
		},
		"unmeasured": {
			CameraID:            "unmeasured",
			Severity:            "CLEAR",
			Confidence:          0.9,
			Timestamp:           time.Now().UTC().Truncate(time.Second),
			FloodCalibrationRef: "NONE",
		},
	}

	for _, v := range want {
		if err := repo.Save(ctx, v); err != nil {
			t.Fatalf("Save %s: %v", v.CameraID, err)
		}
	}

	got, err := repo.GetLatest(ctx)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	byID := make(map[string]store.Verdict, len(got))
	for _, v := range got {
		byID[v.CameraID] = v
	}

	for id, expected := range want {
		actual, ok := byID[id]
		if !ok {
			t.Fatalf("verdict %q missing after round-trip", id)
		}
		if (expected.FloodDepthCm == nil) != (actual.FloodDepthCm == nil) {
			t.Fatalf("%s: FloodDepthCm nil-ness = %v, want %v", id, actual.FloodDepthCm == nil, expected.FloodDepthCm == nil)
		}
		if expected.FloodDepthCm != nil && *actual.FloodDepthCm != *expected.FloodDepthCm {
			t.Errorf("%s: FloodDepthCm = %v, want %v", id, *actual.FloodDepthCm, *expected.FloodDepthCm)
		}
		if (expected.FloodHazardLevel == nil) != (actual.FloodHazardLevel == nil) {
			t.Fatalf("%s: FloodHazardLevel nil-ness = %v, want %v", id, actual.FloodHazardLevel == nil, expected.FloodHazardLevel == nil)
		}
		if expected.FloodHazardLevel != nil && *actual.FloodHazardLevel != *expected.FloodHazardLevel {
			t.Errorf("%s: FloodHazardLevel = %d, want %d", id, *actual.FloodHazardLevel, *expected.FloodHazardLevel)
		}
		if actual.FloodHazardBasis != expected.FloodHazardBasis {
			t.Errorf("%s: FloodHazardBasis = %q, want %q", id, actual.FloodHazardBasis, expected.FloodHazardBasis)
		}
		if actual.FloodDepthConfidence != expected.FloodDepthConfidence {
			t.Errorf("%s: FloodDepthConfidence = %q, want %q", id, actual.FloodDepthConfidence, expected.FloodDepthConfidence)
		}
		if actual.FloodCalibrationRef != expected.FloodCalibrationRef {
			t.Errorf("%s: FloodCalibrationRef = %q, want %q", id, actual.FloodCalibrationRef, expected.FloodCalibrationRef)
		}
	}
}

// TestVerdictRepo_CalibrationRefSurvivesRestart pins M5: the reference ladder string
// must survive a process restart, because the boot-time hydration rebuilds the API
// verdict from SQLite. A column that is written but never read back would drop the
// claim (`MOTORCYCLE_110CM`, `CURB_15CM`, `CAMERA_OVERRIDE`, `NONE`) on every restart.
func TestVerdictRepo_CalibrationRefSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	depth, level := 14.5, 3
	if err := store.NewVerdictRepo(db).Save(context.Background(), store.Verdict{
		CameraID:             "cam_dbp_01",
		Severity:             "CLEAR",
		FloodSeverity:        "FLOODED_SHALLOW",
		Confidence:           0.78,
		Source:               "onnx",
		ObservedAt:           time.Now().UTC().Truncate(time.Second),
		Timestamp:            time.Now().UTC().Truncate(time.Second),
		FloodDepthCm:         &depth,
		FloodHazardLevel:     &level,
		FloodHazardBasis:     "DEPTH_MEASURED",
		FloodDepthConfidence: "MEASURED",
		FloodCalibrationRef:  "CURB_15CM",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("reopen NewDB: %v", err)
	}
	defer reopened.Close() //nolint:errcheck

	got, err := store.NewVerdictRepo(reopened).GetLatest(context.Background())
	if err != nil {
		t.Fatalf("GetLatest after restart: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row after restart, got %d", len(got))
	}
	if got[0].FloodCalibrationRef != "CURB_15CM" {
		t.Errorf("FloodCalibrationRef = %q after restart, want CURB_15CM", got[0].FloodCalibrationRef)
	}
	if got[0].FloodDepthCm == nil || *got[0].FloodDepthCm != depth {
		t.Errorf("FloodDepthCm = %v after restart, want %v", got[0].FloodDepthCm, depth)
	}
}

// TestVerdictRepo_Migration0011OnLegacyDatabase pins E20/AC14: applying 0011 to a
// database that only has the pre-#401 schema keeps the old row readable, with NULL
// hazard fields and no fabricated level. It also proves the migration is repeatable
// by construction (schema_migrations records the version).
func TestVerdictRepo_Migration0011OnLegacyDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_0011.db")

	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE camera_verdicts (
			camera_id      TEXT PRIMARY KEY,
			severity       TEXT NOT NULL,
			flood_severity TEXT NOT NULL DEFAULT '',
			confidence     REAL NOT NULL,
			source         TEXT NOT NULL DEFAULT '',
			observed_at    TIMESTAMP,
			updated_at     TIMESTAMP NOT NULL
		);
		CREATE TABLE schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	for version := 1; version <= 10; version++ {
		if _, err := legacy.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			t.Fatalf("mark migration %d applied: %v", version, err)
		}
	}
	observedAt := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	if _, err := legacy.Exec(
		`INSERT INTO camera_verdicts (camera_id, severity, flood_severity, confidence, source, observed_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"legacy-cam", "HEAVY", "FLOODED_SHALLOW", 0.82, "onnx", observedAt, observedAt,
	); err != nil {
		t.Fatalf("insert legacy verdict: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB (applies 0011): %v", err)
	}
	defer db.Close() //nolint:errcheck

	repo := store.NewVerdictRepo(db)
	verdicts, err := repo.GetLatest(context.Background())
	if err != nil {
		t.Fatalf("GetLatest after migration: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("expected the legacy row to survive, got %d rows", len(verdicts))
	}
	got := verdicts[0]
	if got.CameraID != "legacy-cam" || got.FloodSeverity != "FLOODED_SHALLOW" {
		t.Errorf("legacy fields changed: %+v", got)
	}
	if got.FloodDepthCm != nil {
		t.Errorf("FloodDepthCm = %v, want nil: no value may be backfilled (E20)", *got.FloodDepthCm)
	}
	if got.FloodHazardLevel != nil {
		t.Errorf("FloodHazardLevel = %d, want nil: no fabricated level (E20)", *got.FloodHazardLevel)
	}
	if got.FloodHazardBasis != "" || got.FloodDepthConfidence != "" || got.FloodCalibrationRef != "" {
		t.Errorf("hazard vocabularies = (%q, %q, %q), want empty", got.FloodHazardBasis, got.FloodDepthConfidence, got.FloodCalibrationRef)
	}
}
