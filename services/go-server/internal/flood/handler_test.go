package flood_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

func setupTestRouter(svc *flood.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	handler := flood.NewHandler(svc)
	handler.RegisterRoutes(api)
	return r
}

func TestHandler_VerifyCamera_Success(t *testing.T) {
	analyzer := &mockAnalyzer{
		verdictToReturn: hotset.Verdict{
			FloodSeverity: flood.SeverityWet,
			Confidence:    0.95,
			Source:        "mock",
		},
	}
	weatherSvc := &mockWeather{}
	camReader := newMockCameraReader(camera.Camera{
		ID:          "cam-flood-1",
		DisplayName: "Ung Văn Khiêm",
		SnapshotURL: "https://example.com/snapshot.jpg",
	})
	limiter := flood.NewQuotaLimiter(10, 300)
	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	router := setupTestRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/flood/verify?camera_id=cam-flood-1", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res flood.FloodVerificationResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.CameraID != "cam-flood-1" {
		t.Errorf("expected CameraID cam-flood-1, got %s", res.CameraID)
	}
	if res.FloodSeverity != flood.SeverityWet {
		t.Errorf("expected flood severity WET, got %s", res.FloodSeverity)
	}
}

func TestHandler_VerifyCamera_MissingParam(t *testing.T) {
	svc := flood.NewService(nil, nil, nil, nil, flood.NewQuotaLimiter(10, 300))
	router := setupTestRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/flood/verify", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}

func TestHandler_VerifyCamera_NotFound(t *testing.T) {
	svc := flood.NewService(&mockAnalyzer{}, &mockWeather{}, newMockCameraReader(), nil, flood.NewQuotaLimiter(10, 300))
	router := setupTestRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/flood/verify?camera_id=nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", w.Code)
	}
}

func TestHandler_VerifyCamera_RateLimited(t *testing.T) {
	analyzer := &mockAnalyzer{
		verdictToReturn: hotset.Verdict{FloodSeverity: flood.SeverityDry, Confidence: 0.9},
	}
	camReader := newMockCameraReader(camera.Camera{ID: "cam-1", DisplayName: "Cam 1"})
	limiter := flood.NewQuotaLimiter(1, 300)
	svc := flood.NewService(analyzer, &mockWeather{}, camReader, sampleChokepoints(), limiter)
	router := setupTestRouter(svc)

	// Consume 1 token
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/api/flood/verify?camera_id=cam-1", nil)
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected first call 200, got %d", w1.Code)
	}

	// 2nd call -> 429
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/api/flood/verify?camera_id=cam-1", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second call 429 Too Many Requests, got %d", w2.Code)
	}
}

func TestHandler_GetChokepoints(t *testing.T) {
	svc := flood.NewService(nil, nil, nil, sampleChokepoints(), flood.NewQuotaLimiter(10, 300))
	router := setupTestRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/flood/chokepoints", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp struct {
		Chokepoints []flood.FloodChokepoint `json:"chokepoints"`
		Total       int                     `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode chokepoints: %v", err)
	}
	if resp.Total != 2 || len(resp.Chokepoints) != 2 {
		t.Errorf("expected 2 chokepoints, got %d", resp.Total)
	}
}

func TestHandler_GetQuota(t *testing.T) {
	limiter := flood.NewQuotaLimiter(10, 300)
	svc := flood.NewService(nil, nil, nil, nil, limiter)
	router := setupTestRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/flood/quota", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var status flood.QuotaStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode quota status: %v", err)
	}
	if status.LimitRPM != 10 || status.RemainingRPM != 10 {
		t.Errorf("unexpected status: %+v", status)
	}
}
