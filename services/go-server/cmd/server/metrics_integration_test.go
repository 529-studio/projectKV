package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/metrics"
	"github.com/gin-gonic/gin"
)

func TestMetricsEndpoint_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reg := metrics.NewRegistry()
	reg.RegisterGaugeFunc("warded_corridor_active_count", "Corridor count", func() float64 {
		return 5
	}, nil)
	reg.RegisterGaugeFunc("warded_camera_offline_count", "Offline camera count", func() float64 {
		return 3
	}, nil)
	reg.RegisterGaugeFunc("warded_uptime_seconds", "Uptime", reg.Uptime, nil)
	reg.RegisterGauge("warded_ingest_queue_depth", "Ingest depth", nil).Set(12)
	reg.RegisterGauge("warded_cv_inference_latency_ms", "Inference latency", nil).Set(45.5)
	reg.RegisterGauge("warded_litestream_lag_seconds", "Replication lag", nil).Set(1.2)

	cidrs, err := metrics.ParseCIDRBlocks(nil)
	if err != nil {
		t.Fatalf("parse cidrs: %v", err)
	}

	handler := metrics.NewHandler(reg, metrics.HandlerConfig{
		AdminToken:   "super-secret-token",
		AllowedCIDRs: cidrs,
		AppEnv:       "development",
	})

	cfg := &config.ServerConfig{
		AppEnv:         "development",
		AllowedOrigins: []string{"*"},
		RateLimitRPS:   100,
		RateLimitBurst: 200,
	}

	router := setupRouter(routerConfig{
		cfg:            cfg,
		healthHandler:  httpserver.NewHealthHandler(nil),
		metricsHandler: handler,
		metricsReg:     reg,
	})

	// 1. Hit /healthz to generate request telemetry
	wHealth := httptest.NewRecorder()
	reqHealth, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	router.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 from /healthz, got %d", wHealth.Code)
	}

	// 2. Fetch GET /metrics
	wMetrics := httptest.NewRecorder()
	reqMetrics, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(wMetrics, reqMetrics)

	if wMetrics.Code != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d: %s", wMetrics.Code, wMetrics.Body.String())
	}

	body := wMetrics.Body.String()
	requiredSubstrings := []string{
		"# HELP warded_corridor_active_count Corridor count",
		"# TYPE warded_corridor_active_count gauge",
		"warded_corridor_active_count 5",
		"warded_camera_offline_count 3",
		"warded_ingest_queue_depth 12",
		"warded_cv_inference_latency_ms 45.5",
		"warded_litestream_lag_seconds 1.2",
		"warded_uptime_seconds",
		"warded_http_requests_total",
		"warded_http_request_duration_seconds",
	}

	for _, sub := range requiredSubstrings {
		if !strings.Contains(body, sub) {
			t.Errorf("expected metrics output to contain %q, but body was:\n%s", sub, body)
		}
	}
}

func TestMetricsEndpoint_ProductionAuthAndCIDR(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reg := metrics.NewRegistry()
	cidrs, err := metrics.ParseCIDRBlocks([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("parse cidrs: %v", err)
	}

	handler := metrics.NewHandler(reg, metrics.HandlerConfig{
		AdminToken:   "internal-secret-token",
		AllowedCIDRs: cidrs,
		AppEnv:       "production",
	})

	cfg := &config.ServerConfig{
		AppEnv:         "production",
		AllowedOrigins: []string{"*"},
		RateLimitRPS:   100,
		RateLimitBurst: 200,
	}

	router := setupRouter(routerConfig{
		cfg:            cfg,
		healthHandler:  httpserver.NewHealthHandler(nil),
		metricsHandler: handler,
		metricsReg:     reg,
	})

	// Case 1: Unauthorized external IP, no bearer token -> 403 Forbidden
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	req1.RemoteAddr = "203.0.113.195:12345"
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for external unauthenticated IP, got %d", w1.Code)
	}

	// Case 2: External IP with valid Bearer token -> 200 OK
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	req2.RemoteAddr = "203.0.113.195:12345"
	req2.Header.Set("Authorization", "Bearer internal-secret-token")
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with Bearer token, got %d", w2.Code)
	}

	// Case 3: Whitelisted internal CIDR IP without Bearer token -> 200 OK
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	req3.RemoteAddr = "10.244.1.5:54321"
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with internal CIDR IP, got %d", w3.Code)
	}
}
