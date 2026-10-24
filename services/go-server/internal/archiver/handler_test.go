package archiver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/storage"
	"github.com/gin-gonic/gin"
)

type mockCameraLookup struct{}

func (m *mockCameraLookup) GetCameraTitle(id string) string {
	if id == "cam1" {
		return "Camera 1 - Vo Van Kiet"
	}
	return "Unknown Camera"
}

func setupTestRouter(t *testing.T) (*gin.Engine, storage.Storage) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	tmpDir := t.TempDir()
	store, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	handler := NewHandler(store, &mockCameraLookup{})
	api := router.Group("/api")
	handler.RegisterRoutes(api)

	return router, store
}

func TestHandler_ListDates(t *testing.T) {
	router, store := setupTestRouter(t)
	ctx := context.Background()

	// Seed snapshots for multiple dates
	_ = store.Put(ctx, "2026-10-10/cam1/1791550000_sample.jpg", bytes.NewReader([]byte("sample1")))
	_ = store.Put(ctx, "2026-10-11/cam1/1791560000_sample.jpg", bytes.NewReader([]byte("sample2")))
	_ = store.Put(ctx, "2026-10-09/cam2/1791540000_state_change.jpg", bytes.NewReader([]byte("sample3")))
	// Seed non-date directories and root files that should be ignored
	_ = store.Put(ctx, "feedback/report1.json", bytes.NewReader([]byte("{}")))
	_ = store.Put(ctx, "labels/2026-10-10/label1.json", bytes.NewReader([]byte("{}")))
	_ = store.Put(ctx, "arbitrary_folder/file.txt", bytes.NewReader([]byte("text")))
	_ = store.Put(ctx, "root_file.jpg", bytes.NewReader([]byte("img")))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/archiver/dates", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var dates []string
	if err := json.Unmarshal(w.Body.Bytes(), &dates); err != nil {
		t.Fatalf("failed to unmarshal dates: %v", err)
	}

	if len(dates) != 3 {
		t.Fatalf("expected 3 dates, got %d: %v", len(dates), dates)
	}

	// Must be sorted descending
	if dates[0] != "2026-10-11" || dates[1] != "2026-10-10" || dates[2] != "2026-10-09" {
		t.Errorf("unexpected dates order: %v", dates)
	}
}

func TestHandler_ListSnapshots(t *testing.T) {
	router, store := setupTestRouter(t)
	ctx := context.Background()

	_ = store.Put(ctx, "2026-10-11/cam1/1791560000_sample.jpg", bytes.NewReader([]byte("data1")))
	_ = store.Put(ctx, "2026-10-11/cam1/1791560100_state_change.jpg", bytes.NewReader([]byte("data2")))
	_ = store.Put(ctx, "2026-10-11/cam2/1791560050_sample.jpg", bytes.NewReader([]byte("data3")))

	t.Run("List all for date", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots?date=2026-10-11", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp SnapshotListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if resp.Total != 3 {
			t.Fatalf("expected 3 total snapshots, got %d", resp.Total)
		}
		if len(resp.Snapshots) != 3 {
			t.Fatalf("expected 3 snapshots in page, got %d", len(resp.Snapshots))
		}

		// First item should be the newest timestamp (1791560100)
		if resp.Snapshots[0].Timestamp != 1791560100 {
			t.Errorf("expected newest first, got ts=%d", resp.Snapshots[0].Timestamp)
		}
		if resp.Snapshots[0].CameraName != "Camera 1 - Vo Van Kiet" {
			t.Errorf("expected resolved camera name, got %q", resp.Snapshots[0].CameraName)
		}
		if resp.Snapshots[0].RawURL == "" {
			t.Errorf("expected non-empty RawURL")
		}
	})

	t.Run("Filter by camera_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots?date=2026-10-11&camera_id=cam2", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp SnapshotListResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Total != 1 {
			t.Fatalf("expected 1 snapshot for cam2, got %d", resp.Total)
		}
		if resp.Snapshots[0].CameraID != "cam2" {
			t.Errorf("expected cam2, got %s", resp.Snapshots[0].CameraID)
		}
	})
}

func TestHandler_GetSnapshotRaw(t *testing.T) {
	router, store := setupTestRouter(t)
	ctx := context.Background()

	jpegContent := []byte("JPEG_BINARY_DATA_TEST")
	_ = store.Put(ctx, "2026-10-11/cam1/1791560000_sample.jpg", bytes.NewReader(jpegContent))

	t.Run("Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots/raw?key=2026-10-11/cam1/1791560000_sample.jpg", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("expected Content-Type image/jpeg, got %s", ct)
		}
		if !bytes.Equal(w.Body.Bytes(), jpegContent) {
			t.Errorf("body mismatch: expected %q, got %q", jpegContent, w.Body.Bytes())
		}
	})

	t.Run("Not Found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots/raw?key=2026-10-11/cam1/missing.jpg", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("Path Traversal", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots/raw?key=../../etc/passwd", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for path traversal, got %d", w.Code)
		}
	})

	t.Run("Dual Prefix Fallback - Harvester Frame with proofs/", func(t *testing.T) {
		harvesterContent := []byte("HARVESTER_FRAME_JPEG")
		_ = store.Put(ctx, "proofs/2026-10-11/cam_harvester/1791560000_sample.jpg", bytes.NewReader(harvesterContent))

		// Client queries without proofs/ prefix
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots/raw?key=2026-10-11/cam_harvester/1791560000_sample.jpg", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 via fallback, got %d", w.Code)
		}
		if !bytes.Equal(w.Body.Bytes(), harvesterContent) {
			t.Errorf("body mismatch: expected %q, got %q", harvesterContent, w.Body.Bytes())
		}
	})

	t.Run("Dual Prefix Fallback - Archiver Frame queried with proofs/", func(t *testing.T) {
		archiverContent := []byte("ARCHIVER_FRAME_JPEG")
		_ = store.Put(ctx, "2026-10-11/cam_archiver/1791560000_sample.jpg", bytes.NewReader(archiverContent))

		// Client queries with proofs/ prefix
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/archiver/snapshots/raw?key=proofs/2026-10-11/cam_archiver/1791560000_sample.jpg", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 via reverse fallback, got %d", w.Code)
		}
		if !bytes.Equal(w.Body.Bytes(), archiverContent) {
			t.Errorf("body mismatch: expected %q, got %q", archiverContent, w.Body.Bytes())
		}
	})
}

func TestHandler_SaveAndGetLabels(t *testing.T) {
	router, _ := setupTestRouter(t)

	label := LabelPayload{
		Date:         "2026-10-11",
		CameraID:     "cam1",
		SnapshotKey:  "2026-10-11/cam1/1791560000_sample.jpg",
		Annotator:    "dev_user",
		FloodLevel:   "puddle",
		TrafficLevel: "heavy",
		Weather:      "rain",
		TimeOfDay:    "dusk",
		Notes:        "water pooling at kerb",
		Timestamp:    time.Now().Unix(),
	}

	body, _ := json.Marshal(label)

	// Save Label
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/archiver/labels", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on SaveLabel, got %d: %s", w.Code, w.Body.String())
	}

	// Get Labels for Date
	wGet := httptest.NewRecorder()
	reqGet, _ := http.NewRequest("GET", "/api/archiver/labels?date=2026-10-11", nil)
	router.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 on GetLabels, got %d: %s", wGet.Code, wGet.Body.String())
	}

	var labelsResp LabelListResponse
	if err := json.Unmarshal(wGet.Body.Bytes(), &labelsResp); err != nil {
		t.Fatalf("failed to unmarshal labels response: %v", err)
	}

	if labelsResp.Total != 1 {
		t.Fatalf("expected 1 label, got %d", labelsResp.Total)
	}
	if labelsResp.Labels[0].FloodLevel != "puddle" {
		t.Errorf("expected flood_level puddle, got %s", labelsResp.Labels[0].FloodLevel)
	}
	if labelsResp.Labels[0].TrafficLevel != "heavy" {
		t.Errorf("expected traffic_level heavy, got %s", labelsResp.Labels[0].TrafficLevel)
	}
}

func TestHandler_GetLabels_WithHarvesterGroundTruthManifest(t *testing.T) {
	router, store := setupTestRouter(t)

	// Save GroundTruthManifest as produced by auto_harvester
	manifestJSON := `{
		"frame_id": "cam_auto_1791560000",
		"camera_id": "cam_auto",
		"camera_name": "Test Camera Auto",
		"image_path": "proofs/2026-10-12/cam_auto/1791560000_sample.jpg",
		"annotator": "auto_harvester",
		"annotated_at": "2026-10-12T10:00:00Z",
		"notes": "Auto-harvested via scheduled trigger",
		"labels": {
			"traffic": "CONGESTED",
			"carriageway_direction": "bidirectional",
			"traffic_dir_a": "HEAVY",
			"traffic_dir_b": "MODERATE",
			"is_waiting_red_light": false,
			"queue_reason": "bottleneck",
			"flood": "SHALLOW",
			"waterline_depth": "TIRE_QUARTER_SUBMERGED",
			"flood_submersion_anchor": "tire_base_under_10cm",
			"time_of_day": "day",
			"weather": "rain",
			"quality_tags": ["rain_on_lens"]
		}
	}`

	labelKey := "labels/2026-10-12/cam_auto_auto_harvester_1791560000.json"
	if err := store.Put(context.Background(), labelKey, strings.NewReader(manifestJSON)); err != nil {
		t.Fatalf("failed to put test manifest: %v", err)
	}

	wGet := httptest.NewRecorder()
	reqGet, _ := http.NewRequest("GET", "/api/archiver/labels?date=2026-10-12", nil)
	router.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 on GetLabels, got %d: %s", wGet.Code, wGet.Body.String())
	}

	var labelsResp LabelListResponse
	if err := json.Unmarshal(wGet.Body.Bytes(), &labelsResp); err != nil {
		t.Fatalf("failed to unmarshal labels response: %v", err)
	}

	if labelsResp.Total != 1 {
		t.Fatalf("expected 1 label, got %d", labelsResp.Total)
	}

	l := labelsResp.Labels[0]
	if l.CameraID != "cam_auto" {
		t.Errorf("expected CameraID cam_auto, got %s", l.CameraID)
	}
	if l.SnapshotKey != "2026-10-12/cam_auto/1791560000_sample.jpg" {
		t.Errorf("expected SnapshotKey 2026-10-12/cam_auto/1791560000_sample.jpg, got %s", l.SnapshotKey)
	}
	if l.Annotator != "auto_harvester" {
		t.Errorf("expected Annotator auto_harvester, got %s", l.Annotator)
	}
	if l.TrafficLevel != "congested" {
		t.Errorf("expected TrafficLevel congested, got %s", l.TrafficLevel)
	}
	if l.FloodLevel != "puddle" {
		t.Errorf("expected FloodLevel puddle (mapped from SHALLOW), got %s", l.FloodLevel)
	}
	if l.Weather != "rain" {
		t.Errorf("expected Weather rain, got %s", l.Weather)
	}
}

func TestHandler_InvalidateCache(t *testing.T) {
	router, store := setupTestRouter(t)
	ctx := context.Background()

	// Seed dummy label to populate cache
	_ = store.Put(ctx, "labels/2026-10-23/cam1_reviewer_1729600000.json", bytes.NewReader([]byte(`{"snapshot_key":"snap1","date":"2026-10-23","camera_id":"cam1","annotator":"reviewer"}`)))

	// First call primes cache
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/archiver/labels?date=2026-10-23", nil)
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}

	// Call invalidation endpoint
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/archiver/cache/invalidate", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on cache invalidate, got %d", w2.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}
