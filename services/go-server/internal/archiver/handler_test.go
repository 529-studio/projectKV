package archiver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
