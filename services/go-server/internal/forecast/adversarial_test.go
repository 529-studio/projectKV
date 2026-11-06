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
	svc := forecast.NewService(adapter, cat, forecast.WithAllowSynthetic(true))
	h := forecast.NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
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

			// Seed 4 observations to satisfy MinServingSamples = 4
			for lag := 1; lag <= 4; lag++ {
				_, err := db.Exec(`INSERT INTO hotset_verdict_history
					(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					camID, 20.0, tc.siVal, "HEAVY", 0.90, "test",
					now.Add(-time.Duration(lag*5)*time.Minute).Format(time.RFC3339Nano),
					now.Format(time.RFC3339Nano),
				)
				if err != nil {
					t.Fatalf("insert failed: %v", err)
				}
			}

			path := fmt.Sprintf("/api/v1/cameras/%s/forecast", url.PathEscape(camID))
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
				var resp forecast.MultiHorizonResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse JSON response: %v, raw body: %q", err, w.Body.String())
				}
				for _, h := range resp.Horizons {
					if h.PredictedSaturationIndex < 0.0 {
						t.Errorf("predicted SI must be >= 0.0, got %v", h.PredictedSaturationIndex)
					}
					if h.PredictedSpeedKmh < forecast.MinSpeedKmh || h.PredictedSpeedKmh > forecast.MaxSpeedKmh {
						t.Errorf("predicted speed out of bounds: %v", h.PredictedSpeedKmh)
					}
				}
			}
		})
	}
}

// 3. Stress Test: Non-Finite Values (Inf and NaN) Guard
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
				{CameraID: "cam-nonfinite", SaturationIndex: tc.si, Severity: "HEAVY", Confidence: 0.90, ObservedAt: baseTime.Add(-1 * time.Minute)},
				{CameraID: "cam-nonfinite", SaturationIndex: tc.si, Severity: "HEAVY", Confidence: 0.90, ObservedAt: baseTime.Add(-5 * time.Minute)},
				{CameraID: "cam-nonfinite", SaturationIndex: tc.si, Severity: "HEAVY", Confidence: 0.90, ObservedAt: baseTime.Add(-10 * time.Minute)},
				{CameraID: "cam-nonfinite", SaturationIndex: tc.si, Severity: "HEAVY", Confidence: 0.90, ObservedAt: baseTime.Add(-15 * time.Minute)},
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
			router := setupTestRouter(hist, cat, forecast.WithAllowSynthetic(true))

			req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-nonfinite/forecast?timestamp=2026-11-05T12:00:00Z", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if len(w.Body.Bytes()) == 0 {
				t.Errorf("CRITICAL DEFECT: Endpoint returned empty body on %s input (json.Marshal failed on non-finite float)", tc.name)
			}
			if w.Code != http.StatusOK {
				t.Errorf("expected 200 OK on sanitized non-finite input, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// 4. Stress Test: History Gaps and Sample Floor Requirements
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
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-all-null/forecast", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}
		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Error != forecast.KindInsufficientHistory {
			t.Errorf("expected insufficient_history error, got %q", errResp.Error)
		}
	})

	// Camera with exactly 1 row (fresh, 1m ago) -> fails MinServingSamples=4 floor
	_, _ = db.Exec(`INSERT INTO hotset_verdict_history
		(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"cam-single", 20.0, 0.45, "MODERATE", 0.85, "test",
		now.Add(-1*time.Minute).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)

	t.Run("single observation fails serving floor with 422 insufficient_history", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-single/forecast", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}
		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Error != forecast.KindInsufficientHistory {
			t.Errorf("expected insufficient_history error, got %q", errResp.Error)
		}
		if errResp.Requirements == nil || errResp.Requirements.ObservedSamples != 1 {
			t.Errorf("expected requirements.observed_samples = 1")
		}
	})

	// Camera with 4 observations across 45m with large gap (-1m, -2m, -38m, -40m)
	for _, minAgo := range []int{1, 2, 38, 40} {
		_, _ = db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"cam-gaps", 20.0, 0.50, "MODERATE", 0.85, "test",
			now.Add(-time.Duration(minAgo)*time.Minute).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
	}

	t.Run("history with 4 observations and gap survives without crash", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-gaps/forecast", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp forecast.MultiHorizonResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.SourceData.SampleCount != 4 {
			t.Errorf("expected sample count 4, got %d", resp.SourceData.SampleCount)
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
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-dense/forecast", nil)
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

// 5. Stress Test: Parameter Fuzzing and Rejected Query Params
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
	for i := 1; i <= 4; i++ {
		_, _ = db.Exec(`INSERT INTO hotset_verdict_history
			(camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"cam-fuzz", 20.0, 0.50, "MODERATE", 0.85, "test",
			now.Add(-time.Duration(i*2)*time.Minute).Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		)
	}

	fuzzCases := []struct {
		name       string
		query      string
		wantStatus int
		wantError  string
	}{
		// Any horizon query param is rejected with 400 per D6
		{"horizon=0", "horizon=0", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=-1", "horizon=-1", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=10", "horizon=10", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=20", "horizon=20", http.StatusBadRequest, "invalid_parameter"},
		{"horizon=30", "horizon=30", http.StatusBadRequest, "invalid_parameter"},
		{"horizon_minutes=10", "horizon_minutes=10", http.StatusBadRequest, "invalid_parameter"},
		{"model=invalid_name", "model=super_model", http.StatusBadRequest, "invalid_parameter"},
		{"model=sql_inject'--", "model=ridge_delta'--", http.StatusBadRequest, "invalid_parameter"},
		{"timestamp=bad_date", "timestamp=bad_date", http.StatusBadRequest, "invalid_parameter"},
		{"timestamp=future_1year", fmt.Sprintf("timestamp=%s", now.Add(365*24*time.Hour).Format(time.RFC3339)), http.StatusBadRequest, "invalid_parameter"},
		{"timestamp=epoch_zero", "timestamp=1970-01-01T00:00:00Z", http.StatusUnprocessableEntity, "insufficient_history"},
		{"valid default query", "", http.StatusOK, ""},
		{"valid persistence query", "model=persistence", http.StatusOK, ""},
		{"valid historical_median query", "model=historical_median", http.StatusOK, ""},
	}

	for _, tc := range fuzzCases {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/v1/cameras/cam-fuzz/forecast"
			if tc.query != "" {
				path += "?" + tc.query
			}
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("for %s: expected status %d, got %d: %s", tc.name, tc.wantStatus, w.Code, w.Body.String())
			}
			if tc.wantError != "" {
				var body forecast.ErrorResponse
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if string(body.Error) != tc.wantError {
					t.Errorf("for %s: expected error %q, got %q", tc.name, tc.wantError, body.Error)
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
					req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-concurrent/forecast", nil)
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
