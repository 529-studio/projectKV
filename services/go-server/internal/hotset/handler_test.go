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
			{CameraID: "cam1", Severity: "HEAVY", Confidence: 0.92, Timestamp: now},
			{CameraID: "cam2", Severity: "CLEAR", Confidence: 0.87, Timestamp: now},
		},
	}
	h := hotset.NewHandler(store)
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got []hotset.Verdict
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 verdicts, got %d", len(got))
	}
	if got[0].CameraID != "cam1" || got[0].Severity != "HEAVY" {
		t.Errorf("unexpected first verdict: %+v", got[0])
	}
}

func TestGetTrafficStatus_EmptyStore_ReturnsEmptyArray(t *testing.T) {
	store := &mockStorer{verdicts: nil}
	h := hotset.NewHandler(store)
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Must be [] not null
	body := w.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Errorf("expected empty JSON array [], got %q", body)
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

func TestGetTrafficStatus_ContentTypeJSON(t *testing.T) {
	store := &mockStorer{verdicts: []hotset.Verdict{}}
	h := hotset.NewHandler(store)
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic-status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header must be set")
	}
}
