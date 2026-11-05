package forecast_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
)

// openAdversarialDB creates a test SQLite DB with migrations applied
func openAdversarialDB(t *testing.T) (*store.VerdictHistoryRepo, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_adv.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return store.NewVerdictHistoryRepo(db), db
}

type advCatalog struct {
	cameras map[string]forecast.CameraInfo
}

func (c *advCatalog) GetCamera(id string) (forecast.CameraInfo, bool) {
	cam, ok := c.cameras[id]
	return cam, ok
}

type advHistoryAdapter struct {
	repo *store.VerdictHistoryRepo
}

func (a *advHistoryAdapter) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]forecast.Observation, error) {
	return a.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

func (a *advHistoryAdapter) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]forecast.Observation, error) {
	rows, err := a.repo.GetRecentByCameraBefore(ctx, cameraID, before, window, limit)
	if err != nil {
		return nil, err
	}
	obs := make([]forecast.Observation, len(rows))
	for i, r := range rows {
		obs[i] = forecast.Observation{
			CameraID:        r.CameraID,
			PCUTotal:        r.PCUTotal,
			SaturationIndex: r.SaturationIndex,
			Severity:        r.Severity,
			Confidence:      r.Confidence,
			Source:          r.Source,
			ObservedAt:      r.ObservedAt,
		}
	}
	return obs, nil
}

func setupAdversarialRouter(repo *store.VerdictHistoryRepo, cat forecast.CameraCatalog) *gin.Engine {
	adapter := &advHistoryAdapter{repo: repo}
	svc := forecast.NewService(adapter, cat)
	h := forecast.NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
	h.RegisterRootRoutes(r)
	return r
}

// 1. Stress Test: Monotonicity and Confidence Intervals
// Demonstrates Bug 1: Piecewise discontinuity at SI = 0.30 causing speedLower > speedUpper.
func TestAdversarial_SpeedPiecewiseContinuityAndMonotonicity(t *testing.T) {
	// Greenshields model requires speed to be monotonically non-increasing as saturation index increases.
	var prevSpeed float64 = 50.0
	for si := 0.0; si <= 1.50; si += 0.005 {
		speed := forecast.EstimateSpeed(si)
		if speed > prevSpeed {
			t.Errorf("VIOLATION OF MONOTONICITY at SI=%.3f: speed %.2f km/h > previous speed %.2f km/h", si, speed, prevSpeed)
		}
		prevSpeed = speed
	}

	// Boundary check at 0.30
	speedBelow := forecast.EstimateSpeed(0.299)
	speedAt := forecast.EstimateSpeed(0.300)
	if speedAt > speedBelow {
		t.Errorf("DISCONTINUITY AT 0.30: speed below (SI=0.299) is %.2f km/h, but speed at boundary (SI=0.300) jumps to %.2f km/h", speedBelow, speedAt)
	}

	// Confidence intervals across the 0.30 threshold
	siLower, siUpper, speedLower, speedUpper := forecast.ComputeConfidenceIntervals(0.30, 0.01)
	if speedLower > speedUpper {
		t.Errorf("INVERTED CONFIDENCE INTERVAL: speedLower (%.2f) > speedUpper (%.2f) around SI=0.30 (siLower=%.4f, siUpper=%.4f)",
			speedLower, speedUpper, siLower, siUpper)
	}
}

// 2. Stress Test: Extreme Saturation Values & Non-Finite Numbers
func TestAdversarial_ExtremeSaturationValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, db := openAdversarialDB(t)
	cat := &advCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-stress": {ID: "cam-stress", RoadKey: "stress-way"},
		},
	}
	router := setupAdversarialRouter(repo, cat)

	now := time.Now().UTC()

	cases := []struct {
		name         string
		camSuffix    string
		siVal        float64
		expectStatus int
	}{
		{"SI = 0.0", "zero", 0.0, http.StatusOK},
		{"SI = 2.0 (Oversaturated)", "over2", 2.0, http.StatusOK},
		{"SI = 5.0 (Extreme)", "over5", 5.0, http.StatusOK},
		{"SI = 50.0 (Massive)", "over50", 50.0, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			camID := fmt.Sprintf("cam-stress-%s", tc.camSuffix)
			cat.cameras[camID] = forecast.CameraInfo{ID: camID, RoadKey: "stress-way"}

			_, err := db.Exec(`INSERT INTO hotset_verdict_history
				(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				camID, 20.0, tc.siVal, "HEAVY", 0.90, "test",
				now.Add(-1*time.Minute).Format(time.RFC3339Nano),
				now.Format(time.RFC3339Nano),
			)
			if err != nil {
				t.Fatalf("insert failed: %v", err)
			}

			path := fmt.Sprintf("/api/forecast?camera_id=%s", url.QueryEscape(camID))
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.expectStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.expectStatus, w.Code, w.Body.String())
			}

			if tc.expectStatus == http.StatusOK {
				if len(w.Body.Bytes()) == 0 {
					t.Fatalf("FAIL: server returned status 200 with an EMPTY body")
				}
				var resp forecast.Response
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse JSON response: %v, raw body: %q", err, w.Body.String())
				}
				if resp.PredictedSaturationIndex < 0.0 {
					t.Errorf("predicted SI must be >= 0.0, got %v", resp.PredictedSaturationIndex)
				}
				if resp.PredictedSpeedKmh < forecast.MinSpeedKmh || resp.PredictedSpeedKmh > forecast.MaxSpeedKmh {
					t.Errorf("predicted speed out of bounds: %v", resp.PredictedSpeedKmh)
				}
			}
		})
	}
}

// 3. Stress Test: Non-Finite Values (Inf and NaN) Guard
// Demonstrates Bug 2: If +Inf is returned or present, json.Marshal fails in c.JSON.
func TestAdversarial_NonFiniteValueHandling(t *testing.T) {
	baseTime := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		si   float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := []forecast.Observation{
				{
					CameraID:        "cam-nonfinite",
					SaturationIndex: tc.si,
					Severity:        "HEAVY",
					Confidence:      0.90,
					ObservedAt:      baseTime.Add(-1 * time.Minute),
				},
			}
			hist := &mockHistoryReader{
				observations: map[string][]forecast.Observation{
					"cam-nonfinite": obs,
				},
			}
			cat := &mockCameraCatalog{
				cameras: map[string]forecast.CameraInfo{
					"cam-nonfinite": {ID: "cam-nonfinite", RoadKey: "test-road"},
				},
			}
			router := setupTestRouter(hist, cat)

			req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-nonfinite&timestamp=2026-11-05T12:00:00Z", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if len(w.Body.Bytes()) == 0 {
				t.Errorf("CRITICAL DEFECT: Endpoint returned empty body on %s input (json.Marshal failed on non-finite float)", tc.name)
			}
		})
	}
}

// 4. Stress Test: History Gaps and All-NULL History Rows
func TestAdversarial_HistoryGapsAndAllNullRows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, db := openAdversarialDB(t)
	cat := &advCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-all-null": {ID: "cam-all-null", RoadKey: "null-way"},
			"cam-single":   {ID: "cam-single", RoadKey: "single-way"},
			"cam-gaps":     {ID: "cam-gaps", RoadKey: "gap-way"},
			"cam-dense":    {ID: "cam-dense", RoadKey: "dense-way"},
		},
	}
	router := setupAdversarialRouter(repo, cat)

	now := time.Now().UTC()

	// Camera with 5 rows, but ALL rows have NULL saturation_index
	for i := 1; i <= 5; i++ {
		_, err := db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`,
			"cam-all-null", 15.0, "UNKNOWN", 0.0, "test",
			now.Add(-time.Duration(i*2)*time.Minute).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
		if err != nil {
			t.Fatalf("insert null row: %v", err)
		}
	}

	t.Run("all rows NULL saturation index returns 422 insufficient_history", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-all-null", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["error"] != "insufficient_history" {
			t.Errorf("expected insufficient_history error, got %q", body["error"])
		}
	})

	// Camera with exactly 1 row (fresh, 1m ago)
	_, _ = db.Exec(`INSERT INTO hotset_verdict_history
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"cam-single", 20.0, 0.45, "MODERATE", 0.85, "test",
		now.Add(-1*time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)

	t.Run("single observation succeeds with fallback lag interpolation", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-single", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp forecast.Response
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.PredictedSaturationIndex <= 0.0 {
			t.Errorf("expected valid positive predicted SI, got %v", resp.PredictedSaturationIndex)
		}
		if resp.SourceData.SampleCount != 1 {
			t.Errorf("expected sample count 1, got %d", resp.SourceData.SampleCount)
		}
	})

	// Camera with large gaps (observations at -1m and -40m)
	_, _ = db.Exec(`INSERT INTO hotset_verdict_history
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"cam-gaps", 20.0, 0.50, "MODERATE", 0.85, "test",
		now.Add(-1*time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	_, _ = db.Exec(`INSERT INTO hotset_verdict_history
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"cam-gaps", 10.0, 0.20, "CLEAR", 0.85, "test",
		now.Add(-40*time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)

	t.Run("history with large gap survives without crash", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-gaps", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp forecast.Response
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.SourceData.SampleCount != 2 {
			t.Errorf("expected sample count 2, got %d", resp.SourceData.SampleCount)
		}
	})

	// Dense history (50 observations in 45m)
	for i := 0; i < 50; i++ {
		_, _ = db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"cam-dense", 25.0, 0.55, "MODERATE", 0.90, "test",
			now.Add(-time.Duration(i*50)*time.Second).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
	}

	t.Run("dense history is handled efficiently", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-dense", nil)
		w := httptest.NewRecorder()
		start := time.Now()
		router.ServeHTTP(w, req)
		elapsed := time.Since(start)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("query too slow: %v", elapsed)
		}
	})
}

// 5. Stress Test: Invalid Horizons and Parameter Fuzzing
func TestAdversarial_InvalidHorizonsAndFuzzing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, db := openAdversarialDB(t)
	cat := &advCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-fuzz": {ID: "cam-fuzz", RoadKey: "fuzz-way"},
		},
	}
	router := setupAdversarialRouter(repo, cat)

	now := time.Now().UTC()
	_, _ = db.Exec(`INSERT INTO hotset_verdict_history
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"cam-fuzz", 20.0, 0.50, "MODERATE", 0.85, "test",
		now.Add(-1*time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)

	fuzzCases := []struct {
		name       string
		query      string
		wantStatus int
		wantError  string
	}{
		{"horizon=0", "horizon=0", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=-1", "horizon=-1", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=-99999", "horizon=-99999", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=5", "horizon=5", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=15", "horizon=15", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=25", "horizon=25", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=45", "horizon=45", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=60", "horizon=60", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=99999999999999999999", "horizon=99999999999999999999", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=abc", "horizon=abc", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=10 (valid)", "horizon=10", http.StatusOK, ""},
		{"horizon=20 (valid)", "horizon=20", http.StatusOK, ""},
		{"horizon=30 (valid)", "horizon=30", http.StatusOK, ""},
		{"model=invalid_name", "model=super_model", http.StatusBadRequest, "invalid_parameter"},
		{"model=sql_inject'--", "model=ridge_delta'--", http.StatusBadRequest, "invalid_parameter"},
		{"timestamp=bad_date", "timestamp=bad_date", http.StatusBadRequest, "invalid_parameter"},
		{"timestamp=future_1year", fmt.Sprintf("timestamp=%s", now.Add(365*24*time.Hour).Format(time.RFC3339)), http.StatusUnprocessableEntity, "insufficient_history"},
		{"timestamp=epoch_zero", "timestamp=1970-01-01T00:00:00Z", http.StatusUnprocessableEntity, "insufficient_history"},
	}

	for _, tc := range fuzzCases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/forecast?camera_id=cam-fuzz&%s", tc.query)
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("for %s: expected status %d, got %d: %s", tc.name, tc.wantStatus, w.Code, w.Body.String())
			}
			if tc.wantError != "" {
				var body map[string]string
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body["error"] != tc.wantError {
					t.Errorf("for %s: expected error %q, got %q", tc.name, tc.wantError, body["error"])
				}
			}
		})
	}
}

// 6. Stress Test: Concurrency and Race Detector
func TestAdversarial_ConcurrencyStress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo, db := openAdversarialDB(t)
	cat := &advCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-concurrent": {ID: "cam-concurrent", RoadKey: "race-way"},
		},
	}
	router := setupAdversarialRouter(repo, cat)

	now := time.Now().UTC()
	for i := 1; i <= 10; i++ {
		_, _ = db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"cam-concurrent", 20.0, 0.45, "MODERATE", 0.90, "test",
			now.Add(-time.Duration(i*2)*time.Minute).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
	}

	const workers = 30
	const iterationsPerWorker = 10

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()
			for it := 0; it < iterationsPerWorker; it++ {
				if workerID%2 == 0 {
					req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-concurrent&horizon=10", nil)
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						t.Errorf("worker %d iteration %d failed: %d %s", workerID, it, rec.Code, rec.Body.String())
					}
				} else {
					obsTime := now.Add(time.Duration(workerID*iterationsPerWorker+it) * time.Second)
					_ = repo.Record(context.Background(), store.VerdictHistory{
						CameraID:        "cam-concurrent",
						PCUTotal:        22.0,
						SaturationIndex: 0.48,
						Severity:        "MODERATE",
						Confidence:      0.90,
						Source:          "concurrent_test",
						ObservedAt:      obsTime,
					})
				}
			}
		}(w)
	}

	wg.Wait()
}
