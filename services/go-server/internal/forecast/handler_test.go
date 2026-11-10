package forecast_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockHistoryReader satisfies forecast.HistoryReader and counts calls for verification.
type mockHistoryReader struct {
	observations map[string][]forecast.Observation
	err          error
	callCount    int
}

func (m *mockHistoryReader) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]forecast.Observation, error) {
	return m.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

func (m *mockHistoryReader) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]forecast.Observation, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	obs, ok := m.observations[cameraID]
	if !ok {
		return []forecast.Observation{}, nil
	}
	var filtered []forecast.Observation
	for _, o := range obs {
		if !o.ObservedAt.After(before) {
			if window > 0 && o.ObservedAt.Before(before.Add(-window)) {
				continue
			}
			filtered = append(filtered, o)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, nil
}

// mockCameraCatalog satisfies forecast.CameraCatalog.
type mockCameraCatalog struct {
	cameras map[string]forecast.CameraInfo
}

func (m *mockCameraCatalog) GetCamera(id string) (forecast.CameraInfo, bool) {
	cam, ok := m.cameras[id]
	return cam, ok
}

func setupTestRouter(history *mockHistoryReader, catalog *mockCameraCatalog, opts ...forecast.Option) *gin.Engine {
	svc := forecast.NewService(history, catalog, opts...)
	h := forecast.NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
	return r
}

func seedTestObservations(baseTime time.Time) []forecast.Observation {
	return []forecast.Observation{
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.45,
			Severity:        "MODERATE",
			Confidence:      0.90,
			ObservedAt:      baseTime.Add(-1 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.42,
			Severity:        "MODERATE",
			Confidence:      0.90,
			ObservedAt:      baseTime.Add(-6 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.40,
			Severity:        "MODERATE",
			Confidence:      0.90,
			ObservedAt:      baseTime.Add(-11 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.38,
			Severity:        "MODERATE",
			Confidence:      0.90,
			ObservedAt:      baseTime.Add(-16 * time.Minute),
		},
	}
}

// TestHandler_FailClosedGate tests that booting with an unvalidated synthetic fixture
// fails closed with HTTP 503 and performs zero database reads.
func TestHandler_FailClosedGate(t *testing.T) {
	_ = os.Unsetenv("FORECAST_ALLOW_SYNTHETIC")

	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-1": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-1": {ID: "cam-1", RoadKey: "vo_van_kiet"},
		},
	}

	syntheticMeta := forecast.ModelMetadata{
		Name:      forecast.ModelRidgeDelta,
		Version:   "2026.11.05-synthetic",
		FitSource: string(forecast.FitSourceSyntheticFixture),
		Validated: false,
		MAE:       0.0044,
	}

	r := setupTestRouter(history, catalog, forecast.WithModelMetadata(syntheticMeta), forecast.WithAllowSynthetic(false))

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-1/forecast", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("expected Cache-Control: no-store, got %q", w.Header().Get("Cache-Control"))
	}

	var errResp forecast.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Error != forecast.KindModelUnvalidated {
		t.Errorf("expected error %q, got %q", forecast.KindModelUnvalidated, errResp.Error)
	}
	if errResp.Availability != forecast.StatusModelUnvalidated {
		t.Errorf("expected availability %q, got %q", forecast.StatusModelUnvalidated, errResp.Availability)
	}
	if errResp.Model == nil {
		t.Fatal("expected model provenance in error response, got nil")
	}
	if errResp.Model.FitSource != "synthetic_fixture" {
		t.Errorf("expected model.fit_source synthetic_fixture, got %q", errResp.Model.FitSource)
	}
	if errResp.Model.Validated {
		t.Errorf("expected model.validated false, got true")
	}
	if errResp.Model.MAE != 0.0044 {
		t.Errorf("expected model.mae 0.0044, got %v", errResp.Model.MAE)
	}
	if errResp.Requirements != nil {
		t.Errorf("requirements should be nil in model_unvalidated response")
	}

	// Zero DB work assertion (V4)
	if history.callCount != 0 {
		t.Errorf("expected 0 history reader calls on fail-closed rejection path, got %d", history.callCount)
	}
}

// TestHandler_OverrideSyntheticHatch tests the local developer override (FORECAST_ALLOW_SYNTHETIC=true).
func TestHandler_OverrideSyntheticHatch(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-1": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-1": {ID: "cam-1", RoadKey: "vo_van_kiet"},
		},
	}

	syntheticMeta := forecast.ModelMetadata{
		Name:         forecast.ModelRidgeDelta,
		Version:      "2026.11.05-synthetic",
		FitSource:    string(forecast.FitSourceSyntheticFixture),
		Validated:    false,
		Experimental: true,
		MAE:          0.0044,
	}

	r := setupTestRouter(history, catalog, forecast.WithModelMetadata(syntheticMeta), forecast.WithAllowSynthetic(true))

	t.Run("GET /api/v1/cameras/:id/forecast returns 200 with multi-horizon payload", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-1/forecast", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp forecast.MultiHorizonResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode multi-horizon response: %v", err)
		}

		if resp.CameraID != "cam-1" {
			t.Errorf("expected camera_id cam-1, got %q", resp.CameraID)
		}
		if resp.RoadKey != "vo_van_kiet" {
			t.Errorf("expected road_key vo_van_kiet, got %q", resp.RoadKey)
		}
		if resp.Availability != forecast.StatusOk {
			t.Errorf("expected availability ok, got %q", resp.Availability)
		}
		if !resp.Experimental {
			t.Errorf("expected experimental true under synthetic override")
		}
		if resp.Model.Validated {
			t.Errorf("expected model.validated false")
		}
		if resp.Model.FitSource != "synthetic_fixture" {
			t.Errorf("expected fit_source synthetic_fixture, got %q", resp.Model.FitSource)
		}

		// Current condition verification
		if resp.Current.SaturationIndex != 0.45 {
			t.Errorf("expected current SI 0.45, got %v", resp.Current.SaturationIndex)
		}
		if resp.Current.Severity != "MODERATE" {
			t.Errorf("expected current severity MODERATE, got %q", resp.Current.Severity)
		}
		if resp.Current.SpeedKmh <= 0.0 || resp.Current.SpeedKmh > 45.0 {
			t.Errorf("current speed out of bounds: %v", resp.Current.SpeedKmh)
		}

		// Exactly 3 horizons: 10, 20, 30 min
		if len(resp.Horizons) != 3 {
			t.Fatalf("expected 3 horizons, got %d", len(resp.Horizons))
		}
		expectedHorizons := []int{10, 20, 30}
		for i, h := range resp.Horizons {
			if h.HorizonMinutes != expectedHorizons[i] {
				t.Errorf("expected horizon[%d] to be %d, got %d", i, expectedHorizons[i], h.HorizonMinutes)
			}
			if h.PredictedSaturationIndex <= 0.0 {
				t.Errorf("predicted SI <= 0 for horizon %d", h.HorizonMinutes)
			}
			if h.PredictedSpeedKmh < forecast.MinSpeedKmh || h.PredictedSpeedKmh > forecast.MaxSpeedKmh {
				t.Errorf("predicted speed %v out of bounds [%v, %v]", h.PredictedSpeedKmh, forecast.MinSpeedKmh, forecast.MaxSpeedKmh)
			}
			// Confidence intervals bracket point estimates
			siL := h.ConfidenceIntervals.SaturationIndex.Lower95
			siU := h.ConfidenceIntervals.SaturationIndex.Upper95
			if siL > h.PredictedSaturationIndex || h.PredictedSaturationIndex > siU {
				t.Errorf("SI interval [%v, %v] does not bracket point estimate %v", siL, siU, h.PredictedSaturationIndex)
			}
			spL := h.ConfidenceIntervals.SpeedKmh.Lower95
			spU := h.ConfidenceIntervals.SpeedKmh.Upper95
			if spL > h.PredictedSpeedKmh || h.PredictedSpeedKmh > spU {
				t.Errorf("Speed interval [%v, %v] does not bracket point estimate %v", spL, spU, h.PredictedSpeedKmh)
			}
		}

		// Source data verification
		if resp.SourceData.SampleCount != 4 {
			t.Errorf("expected sample count 4, got %d", resp.SourceData.SampleCount)
		}
		if resp.SourceData.WindowMinutes != 45 {
			t.Errorf("expected window minutes 45, got %d", resp.SourceData.WindowMinutes)
		}
		if resp.SourceData.Stale {
			t.Errorf("expected fresh data, got stale true")
		}
	})

	t.Run("supports persistence model", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-1/forecast?model=persistence", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp forecast.MultiHorizonResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Model.Name != "persistence" {
			t.Errorf("expected model persistence, got %s", resp.Model.Name)
		}
		for _, h := range resp.Horizons {
			if h.PredictedSaturationIndex != resp.Current.SaturationIndex {
				t.Errorf("persistence prediction %v != current SI %v", h.PredictedSaturationIndex, resp.Current.SaturationIndex)
			}
		}
	})

	t.Run("supports historical_median model", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-1/forecast?model=historical_median", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp forecast.MultiHorizonResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Model.Name != "historical_median" {
			t.Errorf("expected model historical_median, got %s", resp.Model.Name)
		}
		if len(resp.Horizons) != 3 {
			t.Fatalf("expected 3 horizons, got %d", len(resp.Horizons))
		}
		if resp.Horizons[0].PredictedSaturationIndex <= 0.0 {
			t.Errorf("expected positive median prediction")
		}
	})
}

// TestHandler_LiveArtifactServing tests serving with a simulated live_history artifact.
func TestHandler_LiveArtifactServing(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-live": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-live": {ID: "cam-live", RoadKey: "live-street"},
		},
	}

	liveMeta := forecast.ModelMetadata{
		Name:           forecast.ModelRidgeDelta,
		Version:        "2026.11.06-live",
		TargetMode:     "delta",
		FitSource:      string(forecast.FitSourceLiveHistory),
		Validated:      true,
		Experimental:   false,
		FitRows:        8640,
		FitCameras:     57,
		FitDataSHA256:  "live_data_sha256_mock",
		ArtifactSHA256: "live_artifact_sha256_mock",
		MAE:            0.0044,
	}

	r := setupTestRouter(history, catalog, forecast.WithModelMetadata(liveMeta), forecast.WithAllowSynthetic(false))

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-live/forecast", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for validated live model without override, got %d: %s", w.Code, w.Body.String())
	}

	var resp forecast.MultiHorizonResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Availability != forecast.StatusOk {
		t.Errorf("expected availability ok, got %q", resp.Availability)
	}
	if resp.Experimental {
		t.Errorf("expected experimental false for live model")
	}
	if !resp.Model.Validated {
		t.Errorf("expected model.validated true for live model")
	}
	if resp.Model.FitSource != "live_history" {
		t.Errorf("expected fit_source live_history, got %q", resp.Model.FitSource)
	}
}

// TestHandler_DefaultBootLiveHistory verifies that default server boot (without options)
// uses the embedded live history model, returns 200 OK, and validated=true.
func TestHandler_DefaultBootLiveHistory(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-default": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-default": {ID: "cam-default", RoadKey: "vo_van_kiet"},
		},
	}

	r := setupTestRouter(history, catalog)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-default/forecast", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for default boot with embedded live model, got %d: %s", w.Code, w.Body.String())
	}

	var resp forecast.MultiHorizonResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Availability != forecast.StatusOk {
		t.Errorf("expected availability ok, got %q", resp.Availability)
	}
	if resp.Experimental {
		t.Errorf("expected experimental false for default live model")
	}
	if !resp.Model.Validated {
		t.Errorf("expected model.validated true for default live model")
	}
	if resp.Model.FitSource != "live_history" {
		t.Errorf("expected fit_source live_history, got %q", resp.Model.FitSource)
	}
}

// TestHandler_RetiredRoutes ensures old endpoints return 404 Not Found.
func TestHandler_RetiredRoutes(t *testing.T) {
	history := &mockHistoryReader{observations: map[string][]forecast.Observation{}}
	catalog := &mockCameraCatalog{cameras: map[string]forecast.CameraInfo{}}
	r := setupTestRouter(history, catalog)

	retiredPaths := []string{
		"/forecast",
		"/forecast?camera_id=cam-1",
		"/api/forecast",
		"/api/forecast?camera_id=cam-1",
		"/api/cameras/cam-1/forecast",
	}

	for _, path := range retiredPaths {
		t.Run("retired path "+path, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("expected 404 Not Found for retired route %s, got %d", path, w.Code)
			}
		})
	}
}

// TestHandler_ForbiddenHorizonParam ensures that supplying horizon/horizon_minutes returns 400.
func TestHandler_ForbiddenHorizonParam(t *testing.T) {
	history := &mockHistoryReader{observations: map[string][]forecast.Observation{}}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-1": {ID: "cam-1"},
		},
	}
	r := setupTestRouter(history, catalog, forecast.WithAllowSynthetic(true))

	cases := []struct {
		name  string
		query string
	}{
		{"horizon=10", "?horizon=10"},
		{"horizon=20", "?horizon=20"},
		{"horizon=30", "?horizon=30"},
		{"horizon_minutes=10", "?horizon_minutes=10"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-1/forecast"+tc.query, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for forbidden horizon param, got %d: %s", w.Code, w.Body.String())
			}

			var errResp forecast.ErrorResponse
			_ = json.Unmarshal(w.Body.Bytes(), &errResp)
			if errResp.Error != forecast.KindInvalidParameter {
				t.Errorf("expected error invalid_parameter, got %q", errResp.Error)
			}
		})
	}
}

// TestHandler_ValidationErrors verifies parameter validation (model, timestamp).
func TestHandler_ValidationErrors(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-1": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-1": {ID: "cam-1"},
		},
	}
	r := setupTestRouter(history, catalog, forecast.WithAllowSynthetic(true))

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantError  forecast.ErrorKind
	}{
		{
			name:       "invalid timestamp format",
			path:       "/api/v1/cameras/cam-1/forecast?timestamp=not-a-date",
			wantStatus: http.StatusBadRequest,
			wantError:  forecast.KindInvalidParameter,
		},
		{
			name:       "future timestamp > 60s",
			path:       "/api/v1/cameras/cam-1/forecast?timestamp=" + now.Add(2*time.Hour).Format(time.RFC3339),
			wantStatus: http.StatusBadRequest,
			wantError:  forecast.KindInvalidParameter,
		},
		{
			name:       "invalid model name",
			path:       "/api/v1/cameras/cam-1/forecast?model=transformer_v9",
			wantStatus: http.StatusBadRequest,
			wantError:  forecast.KindInvalidParameter,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
			var body forecast.ErrorResponse
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body.Error != tc.wantError {
				t.Errorf("expected error %q, got %q", tc.wantError, body.Error)
			}
		})
	}
}

// TestHandler_NotFoundAndInsufficientHistory tests 404, 422, and 503 errors.
func TestHandler_NotFoundAndInsufficientHistory(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-empty": {}, // 0 rows
			"cam-two": { // 2 rows (< 4 floor)
				{CameraID: "cam-two", SaturationIndex: 0.40, ObservedAt: now.Add(-1 * time.Minute)},
				{CameraID: "cam-two", SaturationIndex: 0.38, ObservedAt: now.Add(-5 * time.Minute)},
			},
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-empty": {ID: "cam-empty"},
			"cam-two":   {ID: "cam-two"},
		},
	}
	r := setupTestRouter(history, catalog, forecast.WithAllowSynthetic(true))

	t.Run("camera not in catalog returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-unregistered/forecast", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Error != forecast.KindNotFound {
			t.Errorf("expected error not_found, got %q", errResp.Error)
		}
	})

	t.Run("camera with fewer than 4 observations returns 422 with requirements", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-two/forecast", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}

		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Error != forecast.KindInsufficientHistory {
			t.Errorf("expected error %q, got %q", forecast.KindInsufficientHistory, errResp.Error)
		}
		if errResp.Availability != forecast.StatusInsufficientHistory {
			t.Errorf("expected availability %q, got %q", forecast.StatusInsufficientHistory, errResp.Availability)
		}
		if errResp.Requirements == nil {
			t.Fatal("expected requirements in 422 response, got nil")
		}
		if errResp.Requirements.MinObservations != 4 {
			t.Errorf("expected min_observations 4, got %d", errResp.Requirements.MinObservations)
		}
		if errResp.Requirements.ObservedSamples != 2 {
			t.Errorf("expected observed_samples 2, got %d", errResp.Requirements.ObservedSamples)
		}
		if errResp.Requirements.RequiredWindowMinutes != 45 {
			t.Errorf("expected required_window_minutes 45, got %d", errResp.Requirements.RequiredWindowMinutes)
		}
	})

	t.Run("camera with zero observations returns 422 with observed_samples=0", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-empty/forecast", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}

		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Requirements.ObservedSamples != 0 {
			t.Errorf("expected observed_samples 0, got %d", errResp.Requirements.ObservedSamples)
		}
	})

	t.Run("database failure returns 503", func(t *testing.T) {
		failingHistory := &mockHistoryReader{
			err: errors.New("sqlite: database is locked"),
		}
		rFailing := setupTestRouter(failingHistory, catalog, forecast.WithAllowSynthetic(true))
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-empty/forecast", nil)
		w := httptest.NewRecorder()
		rFailing.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got %d: %s", w.Code, w.Body.String())
		}
		var errResp forecast.ErrorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &errResp)
		if errResp.Error != forecast.KindServiceUnavailable {
			t.Errorf("expected error service_unavailable, got %q", errResp.Error)
		}
	})
}

// TestHandler_StalenessGuard verifies availability=stale and discounted confidence
// when trailing observation age exceeds 15 minutes.
func TestHandler_StalenessGuard(t *testing.T) {
	baseTime := time.Date(2026, time.November, 5, 14, 0, 0, 0, time.UTC)
	// 4 observations, but the newest is 20 minutes old (> 15m staleness threshold)
	staleObs := []forecast.Observation{
		{CameraID: "cam-stale", SaturationIndex: 0.50, Severity: "MODERATE", Confidence: 0.80, ObservedAt: baseTime.Add(-20 * time.Minute)},
		{CameraID: "cam-stale", SaturationIndex: 0.48, Severity: "MODERATE", Confidence: 0.80, ObservedAt: baseTime.Add(-25 * time.Minute)},
		{CameraID: "cam-stale", SaturationIndex: 0.45, Severity: "MODERATE", Confidence: 0.80, ObservedAt: baseTime.Add(-30 * time.Minute)},
		{CameraID: "cam-stale", SaturationIndex: 0.42, Severity: "MODERATE", Confidence: 0.80, ObservedAt: baseTime.Add(-35 * time.Minute)},
	}

	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-stale": staleObs,
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-stale": {ID: "cam-stale", RoadKey: "stale-road"},
		},
	}
	r := setupTestRouter(history, catalog, forecast.WithAllowSynthetic(true))

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/cameras/cam-stale/forecast?timestamp=2026-11-05T14:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp forecast.MultiHorizonResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Availability != forecast.StatusStale {
		t.Errorf("expected availability stale, got %q", resp.Availability)
	}
	if !resp.SourceData.Stale {
		t.Errorf("expected source_data.stale to be true for 20m old observation")
	}
	if resp.SourceData.AgeMinutes < 20.0 {
		t.Errorf("expected age_minutes >= 20.0, got %v", resp.SourceData.AgeMinutes)
	}

	// In staleness state, horizon confidence is discounted by 50%
	for _, h := range resp.Horizons {
		if h.Confidence > 0.50 {
			t.Errorf("expected discounted confidence <= 0.50, got %v for horizon %d", h.Confidence, h.HorizonMinutes)
		}
	}
}
