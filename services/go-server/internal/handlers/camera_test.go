package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestCameraHandler_HealthCheck_Normal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &models.CameraStore{
		Total:       10,
		OnlineCount: 8,
	}
	handler := NewCameraHandler(store, nil)

	r := gin.New()
	r.GET("/health", handler.HealthCheck)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %v", res["status"])
	}
	if res["cameras_loaded"] != float64(10) {
		t.Fatalf("expected cameras_loaded 10, got %v", res["cameras_loaded"])
	}
}

func TestCameraHandler_HealthCheck_Degraded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &models.CameraStore{
		Total:       10,
		OnlineCount: 8,
	}
	handler := NewCameraHandler(store, nil)
	handler.SetHealthProbe(func() map[string]string {
		return map[string]string{
			"catalog":  "ok",
			"database": "ok",
			"analyzer": "degraded (circuit breaker open: connection refused)",
		}
	})

	r := gin.New()
	r.GET("/health", handler.HealthCheck)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Must NOT return 500 error!
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if res["status"] != "degraded" {
		t.Fatalf("expected status 'degraded', got %v", res["status"])
	}

	components, ok := res["components"].(map[string]any)
	if !ok {
		t.Fatalf("expected components map, got %v", res["components"])
	}
	if components["analyzer"] != "degraded (circuit breaker open: connection refused)" {
		t.Fatalf("unexpected analyzer status: %v", components["analyzer"])
	}
}
