package harvester_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/harvester"
	"github.com/gin-gonic/gin"
)

func setupStatusRouter(t *testing.T, coord *harvester.Coordinator, enabled bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	harvester.NewHandler(coord, enabled).RegisterRoutes(api, func(c *gin.Context) { c.Next() })
	return router
}

// TestHandler_Status_ServesSnapshot verifies GET /api/harvester/status returns
// the coordinator observability payload (Issue #299).
func TestHandler_Status_ServesSnapshot(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	coord, err := harvester.New(harvester.Config{
		Enabled:             true,
		ScheduleHours:       "07:30,12:00",
		RainCooldownMinutes: 60,
		Concurrency:         5,
		TimeLocation:        loc,
	}, harvester.Dependencies{
		Weather:       &mockWeatherProvider{},
		Fetcher:       &mockSnapshotFetcher{},
		Predictor:     &mockVisionPredictor{},
		Storage:       newMockStorageUploader(),
		HotsetCameras: []harvester.Camera{{ID: "c1"}},
		RainCameras:   []harvester.Camera{{ID: "c1"}},
	})
	if err != nil {
		t.Fatalf("failed to construct coordinator: %v", err)
	}

	router := setupStatusRouter(t, coord, true)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/harvester/status", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status harvester.Status
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}
	if !status.Enabled || len(status.ScheduleSlots) != 2 || status.NextSlot == "" {
		t.Errorf("unexpected status payload: %+v", status)
	}
	if status.StartedAt.IsZero() {
		t.Error("expected started_at to be set")
	}
}

// TestHandler_Status_NilCoordinatorServes503 verifies the degraded case where
// the harvester subsystem could not initialize (missing storage driver).
func TestHandler_Status_NilCoordinatorServes503(t *testing.T) {
	router := setupStatusRouter(t, nil, false)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/harvester/status", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for nil coordinator, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal error body: %v", err)
	}
	if body["enabled"] != false {
		t.Errorf("expected enabled=false in degraded status, got %v", body["enabled"])
	}
}

// TestHandler_Status_DisabledFlag verifies the enabled flag reflects config
// even when a coordinator instance exists (HARVESTER_ENABLED=false).
func TestHandler_Status_DisabledFlag(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	coord, err := harvester.New(harvester.Config{
		Enabled:       false,
		ScheduleHours: "07:30",
		Concurrency:   5,
		TimeLocation:  loc,
	}, harvester.Dependencies{
		Fetcher:   &mockSnapshotFetcher{},
		Predictor: &mockVisionPredictor{},
		Storage:   newMockStorageUploader(),
	})
	if err != nil {
		t.Fatalf("failed to construct coordinator: %v", err)
	}

	router := setupStatusRouter(t, coord, false)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/harvester/status", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var status harvester.Status
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if status.Enabled {
		t.Error("expected enabled=false in payload when HARVESTER_ENABLED=false")
	}
}
