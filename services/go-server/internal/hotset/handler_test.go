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
	var resp hotset.FloodStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal flood status: %v", err)
	}
	if len(resp.Verdicts) != 0 {
		t.Errorf("expected empty verdicts, got %d", len(resp.Verdicts))
	}
}

func TestGetFloodStatus_HonestyAndMockFiltering(t *testing.T) {
	now := time.Now().UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{
				CameraID:      "flood_deep_cam",
				Severity:      "CLEAR",
				FloodSeverity: "FLOODED_DEEP",
				Confidence:    0.9,
				Source:        "vlm",
				ObservedAt:    now,
				Timestamp:     now,
			},
			{
				CameraID:      "mock_flood_cam",
				Severity:      "CLEAR",
				FloodSeverity: "FLOODED_SHALLOW",
				Confidence:    0.9,
				Source:        "mock",
				ObservedAt:    now,
				Timestamp:     now,
			},
			{
				CameraID:      "unverified_cam",
				Severity:      "CLEAR",
				FloodSeverity: "",
				Confidence:    0.9,
				Source:        "vlm",
				ObservedAt:    now,
				Timestamp:     now,
			},
		},
	}

	// 1. In production, mock verdicts are excluded and unverified are excluded (Honesty rule)
	hProd := hotset.NewHandlerWithConfig(store, true, "production")
	rProd := newTestRouter(hProd)

	req := httptest.NewRequest(http.MethodGet, "/api/flood-status", nil)
	wProd := httptest.NewRecorder()
	rProd.ServeHTTP(wProd, req)

	var respProd hotset.FloodStatusResponse
	if err := json.Unmarshal(wProd.Body.Bytes(), &respProd); err != nil {
		t.Fatalf("unmarshal flood status: %v", err)
	}
	if len(respProd.Verdicts) != 1 {
		t.Fatalf("expected 1 verdict in prod, got %d", len(respProd.Verdicts))
	}
	if respProd.Verdicts[0].CameraID != "flood_deep_cam" {
		t.Errorf("expected flood_deep_cam, got %s", respProd.Verdicts[0].CameraID)
	}

	// 2. In dev, mock verdicts are allowed
	hDev := hotset.NewHandlerWithConfig(store, true, "dev")
	rDev := newTestRouter(hDev)

	wDev := httptest.NewRecorder()
	rDev.ServeHTTP(wDev, req)

	var respDev hotset.FloodStatusResponse
	if err := json.Unmarshal(wDev.Body.Bytes(), &respDev); err != nil {
		t.Fatalf("unmarshal flood status in dev: %v", err)
	}
	if len(respDev.Verdicts) != 2 {
		t.Fatalf("expected 2 verdicts in dev (mock allowed, unverified excluded), got %d", len(respDev.Verdicts))
	}
}

func TestGetTrafficStatus_MarksStaleWhenOlderThanTTL(t *testing.T) {
	now := time.Now().UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{
				CameraID:   "fresh_cam",
				Severity:   "CLEAR",
				Confidence: 0.9,
				Source:     "vlm",
				ObservedAt: now.Add(-30 * time.Second),
				Timestamp:  now.Add(-30 * time.Second),
				Stale:      false,
			},
			{
				CameraID:   "old_cam",
				Severity:   "HEAVY",
				Confidence: 0.85,
				Source:     "vlm",
				ObservedAt: now.Add(-200 * time.Second),
				Timestamp:  now.Add(-200 * time.Second),
				Stale:      false,
			},
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

	if len(resp.Verdicts) != 2 {
		t.Fatalf("expected 2 verdicts, got %d", len(resp.Verdicts))
	}

	verdictMap := make(map[string]hotset.Verdict)
	for _, v := range resp.Verdicts {
		verdictMap[v.CameraID] = v
	}

	if verdictMap["fresh_cam"].Stale {
		t.Errorf("expected fresh_cam to have stale=false, got true")
	}
	if !verdictMap["old_cam"].Stale {
		t.Errorf("expected old_cam to have stale=true, got false")
	}
	if resp.AppEnv != "production" {
		t.Errorf("expected app_env production, got %s", resp.AppEnv)
	}
}

func TestGetTrafficStatus_CustomStaleTTLAndFrozenPreserved(t *testing.T) {
	now := time.Now().UTC()
	store := &mockStorer{
		verdicts: []hotset.Verdict{
			{
				CameraID:   "frozen_fresh",
				Severity:   "CLEAR",
				Confidence: 0.9,
				Source:     "vlm",
				ObservedAt: now.Add(-10 * time.Second),
				Timestamp:  now.Add(-10 * time.Second),
				Stale:      true, // Already marked stale by frozen-frame detector
			},
			{
				CameraID:   "medium_age",
				Severity:   "MODERATE",
				Confidence: 0.8,
				Source:     "vlm",
				ObservedAt: now.Add(-70 * time.Second),
				Timestamp:  now.Add(-70 * time.Second),
				Stale:      false,
			},
		},
	}
	h := hotset.NewHandlerWithConfig(store, true, "dev")
	h.SetStaleTTL(60 * time.Second) // 1 minute TTL
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

	verdictMap := make(map[string]hotset.Verdict)
	for _, v := range resp.Verdicts {
		verdictMap[v.CameraID] = v
	}

	if !verdictMap["frozen_fresh"].Stale {
		t.Errorf("expected frozen_fresh to preserve stale=true")
	}
	if !verdictMap["medium_age"].Stale {
		t.Errorf("expected medium_age (70s > 60s TTL) to be marked stale=true")
	}
	if resp.AppEnv != "dev" {
		t.Errorf("expected app_env dev, got %s", resp.AppEnv)
	}
}
