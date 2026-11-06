package forecast_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockHistoryReader satisfies forecast.HistoryReader
type mockHistoryReader struct {
	observations map[string][]forecast.Observation
	err          error
}

func (m *mockHistoryReader) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]forecast.Observation, error) {
	return m.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

func (m *mockHistoryReader) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]forecast.Observation, error) {
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

// mockCameraCatalog satisfies forecast.CameraCatalog
type mockCameraCatalog struct {
	cameras map[string]forecast.CameraInfo
}

func (m *mockCameraCatalog) GetCamera(id string) (forecast.CameraInfo, bool) {
	cam, ok := m.cameras[id]
	return cam, ok
}

func setupTestRouter(history *mockHistoryReader, catalog *mockCameraCatalog) *gin.Engine {
	svc := forecast.NewService(history, catalog)
	h := forecast.NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
	h.RegisterRootRoutes(r)
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

func TestHandler_HappyPath(t *testing.T) {
	now := time.Now().UTC()
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-1": seedTestObservations(now),
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-1": {
				ID:           "cam-1",
				RoadKey:      "vo_van_kiet",
				BaseCapacity: 15.0,
			},
		},
	}

	r := setupTestRouter(history, catalog)

	t.Run("GET /api/forecast returns 200 with ridge_delta default", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-1&horizon=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp forecast.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode json response: %v", err)
		}

		if resp.CameraID != "cam-1" {
			t.Errorf("expected camera_id cam-1, got %s", resp.CameraID)
		}
		if resp.RoadKey != "vo_van_kiet" {
			t.Errorf("expected road_key vo_van_kiet, got %s", resp.RoadKey)
		}
		if resp.HorizonMinutes != 10 {
			t.Errorf("expected horizon 10, got %d", resp.HorizonMinutes)
		}
		if resp.Model.Name != "ridge_delta" {
			t.Errorf("expected model ridge_delta, got %s", resp.Model.Name)
		}
		if resp.PredictedSaturationIndex <= 0.0 {
			t.Errorf("expected positive predicted SI, got %v", resp.PredictedSaturationIndex)
		}
		if resp.PredictedSpeedKmh <= 0.0 || resp.PredictedSpeedKmh > 50.0 {
			t.Errorf("predicted speed out of bounds: %v", resp.PredictedSpeedKmh)
		}
		if resp.SourceData.Stale {
			t.Errorf("expected fresh data (not stale), got stale=true")
		}
	})

	t.Run("root alias GET /forecast returns 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/forecast?camera_id=cam-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK from root alias, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("supports segment_id alias and horizon_minutes", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?segment_id=cam-1&horizon_minutes=20", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		var resp forecast.Response
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.HorizonMinutes != 20 {
			t.Errorf("expected horizon 20, got %d", resp.HorizonMinutes)
		}
	})

	t.Run("supports node_id and edge_id aliases", func(t *testing.T) {
		reqNode, _ := http.NewRequest(http.MethodGet, "/api/forecast?node_id=cam-1", nil)
		wNode := httptest.NewRecorder()
		r.ServeHTTP(wNode, reqNode)
		if wNode.Code != http.StatusOK {
			t.Errorf("expected 200 OK with node_id, got %d", wNode.Code)
		}

		reqEdge, _ := http.NewRequest(http.MethodGet, "/api/forecast?edge_id=cam-1", nil)
		wEdge := httptest.NewRecorder()
		r.ServeHTTP(wEdge, reqEdge)
		if wEdge.Code != http.StatusOK {
			t.Errorf("expected 200 OK with edge_id, got %d", wEdge.Code)
		}
	})

	t.Run("supports persistence model", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-1&model=persistence", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		var resp forecast.Response
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Model.Name != "persistence" {
			t.Errorf("expected persistence model, got %s", resp.Model.Name)
		}
		if resp.PredictedSaturationIndex != resp.CurrentSaturationIndex {
			t.Errorf("expected predicted SI == current SI for persistence, got %v vs %v",
				resp.PredictedSaturationIndex, resp.CurrentSaturationIndex)
		}
	})

	t.Run("supports historical_median model", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-1&model=historical_median&horizon=30", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		var resp forecast.Response
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Model.Name != "historical_median" {
			t.Errorf("expected historical_median model, got %s", resp.Model.Name)
		}
		if resp.PredictedSaturationIndex != 0.4416 { // rounded 0.4415535
			t.Errorf("expected global median ~0.4416, got %v", resp.PredictedSaturationIndex)
		}
	})
}

func TestHandler_ValidationErrors(t *testing.T) {
	history := &mockHistoryReader{observations: map[string][]forecast.Observation{}}
	catalog := &mockCameraCatalog{cameras: map[string]forecast.CameraInfo{}}
	r := setupTestRouter(history, catalog)

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing camera_id",
			path:       "/api/forecast",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_parameter",
		},
		{
			name:       "invalid horizon (15 min)",
			path:       "/api/forecast?camera_id=cam-1&horizon=15",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_parameter",
		},
		{
			name:       "invalid horizon negative",
			path:       "/api/forecast?camera_id=cam-1&horizon=-10",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_parameter",
		},
		{
			name:       "invalid timestamp format",
			path:       "/api/forecast?camera_id=cam-1&timestamp=not-a-date",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_parameter",
		},
		{
			name:       "invalid model name",
			path:       "/api/forecast?camera_id=cam-1&model=transformer_v9",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_parameter",
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
			var body map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body["error"] != tc.wantError {
				t.Errorf("expected error %q, got %q", tc.wantError, body["error"])
			}
		})
	}
}

func TestHandler_NotFoundAndInsufficientHistory(t *testing.T) {
	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-empty": {}, // 0 rows
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-empty": {ID: "cam-empty"},
		},
	}
	r := setupTestRouter(history, catalog)

	t.Run("camera not in catalog returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-unregistered", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("camera with no history rows returns 422", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-empty", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("database failure returns 503", func(t *testing.T) {
		failingHistory := &mockHistoryReader{
			err: errors.New("sqlite: database is locked"),
		}
		rFailing := setupTestRouter(failingHistory, catalog)
		req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-empty", nil)
		w := httptest.NewRecorder()
		rFailing.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestHandler_StalenessGuard(t *testing.T) {
	baseTime := time.Date(2026, time.November, 5, 14, 0, 0, 0, time.UTC)
	// Observation is 25 minutes old (> 15m staleness threshold)
	staleObs := []forecast.Observation{
		{
			CameraID:        "cam-stale",
			SaturationIndex: 0.50,
			Severity:        "MODERATE",
			Confidence:      0.80,
			ObservedAt:      baseTime.Add(-25 * time.Minute),
		},
	}

	history := &mockHistoryReader{
		observations: map[string][]forecast.Observation{
			"cam-stale": staleObs,
		},
	}
	catalog := &mockCameraCatalog{
		cameras: map[string]forecast.CameraInfo{
			"cam-stale": {ID: "cam-stale"},
		},
	}
	r := setupTestRouter(history, catalog)

	req, _ := http.NewRequest(http.MethodGet, "/api/forecast?camera_id=cam-stale&timestamp=2026-11-05T14:00:00Z", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp forecast.Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if !resp.SourceData.Stale {
		t.Errorf("expected source_data.stale to be true for 25m old observation")
	}
	// Confidence should be discounted by 50% for stale data
	if resp.Confidence >= 0.80 {
		t.Errorf("expected discounted confidence for stale data, got %v", resp.Confidence)
	}
}
