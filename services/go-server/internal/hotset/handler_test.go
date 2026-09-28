package hotset_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/gin-gonic/gin"
)

// ── mock VerdictStorer ──────────────────────────────────────────────────────

type mockStorer struct {
	verdicts []hotset.Verdict
}

func (m *mockStorer) GetLatest() []hotset.Verdict {
	return m.verdicts
}

// ── helper ──────────────────────────────────────────────────────────────────

func newTestRouter(h *hotset.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
	return r
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestGetTrafficStatus_ReturnsVerdicts(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{CameraID: "cam1", Severity: "HEAVY", Confidence: 0.92, Source: "vlm", Timestamp: now},
			{CameraID: "cam2", Severity: "CLEAR", Confidence: 0.87, Source: "vlm", Timestamp: now},
		},
	}
	h := hotset.NewHandlerWithConfig(store, true, "production")
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp hotset.TrafficStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.AnalysisEnabled {
		t.Errorf("expected analysis_enabled true, got false")
	}
	if len(resp.Verdicts) != 2 {
		t.Errorf("expected 2 verdicts, got %d", len(resp.Verdicts))
	}
	if resp.Verdicts[0].CameraID != "cam1" || resp.Verdicts[0].Severity != "HEAVY" {
		t.Errorf("unexpected first verdict: %+v", resp.Verdicts[0])
	}
}

func TestGetTrafficStatus_OmitsMockVerdicts_WhenNotDev(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{CameraID: "cam_mock", Severity: "CLEAR", Confidence: 0.99, Source: "mock", Timestamp: now},
			{CameraID: "cam_real", Severity: "MODERATE", Confidence: 0.85, Source: "vlm", Timestamp: now},
		},
	}
	// In production, mock verdicts must be filtered out
	h := hotset.NewHandlerWithConfig(store, true, "production")
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp hotset.TrafficStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Verdicts) != 1 {
		t.Fatalf("expected 1 verdict (mock filtered), got %d", len(resp.Verdicts))
	}
	if resp.Verdicts[0].CameraID != "cam_real" {
		t.Errorf("expected only cam_real, got %s", resp.Verdicts[0].CameraID)
	}
}

func TestGetTrafficStatus_AllowsMockVerdicts_WhenDev(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{CameraID: "cam_mock", Severity: "CLEAR", Confidence: 0.99, Source: "mock", Timestamp: now},
		},
	}
	h := hotset.NewHandlerWithConfig(store, true, "dev")
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp hotset.TrafficStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Verdicts) != 1 {
		t.Fatalf("expected 1 verdict in dev mode, got %d", len(resp.Verdicts))
	}
	if resp.Verdicts[0].Source != "mock" {
		t.Errorf("expected source mock, got %s", resp.Verdicts[0].Source)
	}
}

func TestGetTrafficStatus_EmptyStore_ReturnsEmptyList(t *testing.T) {
	store := &mockStorer{verdicts: nil}
	h := hotset.NewHandlerWithConfig(store, false, "production")
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp hotset.TrafficStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AnalysisEnabled {
		t.Errorf("expected analysis_enabled false")
	}
	if len(resp.Verdicts) != 0 {
		t.Errorf("expected 0 verdicts, got %d", len(resp.Verdicts))
	}
}

func TestGetConfig_ReturnsAnalysisConfig(t *testing.T) {
	store := &mockStorer{}
	h := hotset.NewHandlerWithConfig(store, false, "production")
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var conf map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&conf); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if conf["analysis_enabled"] != false {
		t.Errorf("expected analysis_enabled false, got %v", conf["analysis_enabled"])
	}
	if conf["app_env"] != "production" {
		t.Errorf("expected app_env production, got %v", conf["app_env"])
	}
}

func TestGetFloodStatus_ReturnsEmptyArray(t *testing.T) {
	store := &mockStorer{}
	h := hotset.NewHandler(store)
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/flood-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Errorf("expected empty JSON array [], got %q", body)
	}
}
