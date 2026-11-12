package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSwaggerEndpoints(t *testing.T) {
	r := NewServer()

	tests := []struct {
		url          string
		expectedCode int
	}{
		{"/healthz", http.StatusOK},
		{"/swagger", http.StatusMovedPermanently},
		{"/swagger/", http.StatusMovedPermanently},
		{"/swagger/index.html", http.StatusOK},
		{"/swagger/doc.json", http.StatusOK},
	}

	for _, tt := range tests {
		req, _ := http.NewRequest(http.MethodGet, tt.url, nil)
		req.RequestURI = tt.url
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != tt.expectedCode {
			t.Errorf("url %s: expected status %d, got %d", tt.url, tt.expectedCode, w.Code)
		}
	}
}

type healthResponse struct {
	Status   string         `json:"status"`
	Schema   map[string]any `json:"schema"`
	CVEngine CVEngineHealth `json:"cv_engine"`
}

func serveHealth(t *testing.T, handler *HealthHandler, url string) (*httptest.ResponseRecorder, healthResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(url, handler.Healthz)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	router.ServeHTTP(w, req)

	var body healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: unmarshal health body: %v (body=%s)", url, err, w.Body.String())
	}
	return w, body
}

// TestHealthEndpoints_AllAliasesWithoutChecker verifies the three health paths
// stay a pure liveness probe when no cv-engine checker is wired.
func TestHealthEndpoints_AllAliasesWithoutChecker(t *testing.T) {
	handler := NewHealthHandler(nil)
	for _, url := range []string{"/health", "/healthz", "/api/health"} {
		w, body := serveHealth(t, handler, url)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", url, w.Code)
		}
		if body.Status != HealthStatusHealthy {
			t.Errorf("%s: expected overall status healthy, got %q", url, body.Status)
		}
		if body.CVEngine.Status != HealthStatusHealthy || body.CVEngine.Configured {
			t.Errorf("%s: unexpected cv_engine block without checker: %+v", url, body.CVEngine)
		}
		if ready, ok := body.Schema["history_ready"].(bool); !ok || !ready {
			t.Errorf("%s: expected schema.history_ready=true, got %+v", url, body.Schema)
		}
	}
}

// TestHealthEndpoints_CVEngineStatuses verifies the healthy/degraded/unhealthy
// mapping, including the 503 contract for an unhealthy engine.
func TestHealthEndpoints_CVEngineStatuses(t *testing.T) {
	checkedAt := time.Now().UTC()

	tests := []struct {
		name         string
		report       CVEngineHealth
		wantOverall  string
		wantHTTPCode int
	}{
		{
			name: "healthy engine",
			report: CVEngineHealth{
				Status:            HealthStatusHealthy,
				Service:           "cv-engine",
				OpenVINOAvailable: true,
				ModelLoaded:       true,
				LatencyMs:         4.2,
				CheckedAt:         checkedAt,
			},
			wantOverall:  HealthStatusHealthy,
			wantHTTPCode: http.StatusOK,
		},
		{
			name: "degraded standby engine",
			report: CVEngineHealth{
				Status:            HealthStatusDegraded,
				Service:           "cv-engine",
				OpenVINOAvailable: true,
				ModelLoaded:       false,
				Error:             "standby: no model loaded",
				CheckedAt:         checkedAt,
			},
			wantOverall:  HealthStatusDegraded,
			wantHTTPCode: http.StatusOK,
		},
		{
			name: "unhealthy engine",
			report: CVEngineHealth{
				Status:    HealthStatusUnhealthy,
				Service:   "cv-engine",
				Error:     "connection refused",
				CheckedAt: checkedAt,
			},
			wantOverall:  HealthStatusUnhealthy,
			wantHTTPCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.report
			handler := NewHealthHandler(HealthCheckerFunc(func(ctx context.Context) CVEngineHealth {
				return report
			}))

			for _, url := range []string{"/health", "/healthz", "/api/health"} {
				w, body := serveHealth(t, handler, url)
				if w.Code != tt.wantHTTPCode {
					t.Errorf("%s: expected HTTP %d, got %d", url, tt.wantHTTPCode, w.Code)
				}
				if body.Status != tt.wantOverall {
					t.Errorf("%s: expected overall status %q, got %q", url, tt.wantOverall, body.Status)
				}
				if body.CVEngine.Status != tt.wantOverall {
					t.Errorf("%s: expected cv_engine.status %q, got %q", url, tt.wantOverall, body.CVEngine.Status)
				}
				if !body.CVEngine.Configured {
					t.Errorf("%s: expected cv_engine.configured=true when a checker is wired", url)
				}
			}
		})
	}
}

// TestNewProbeHealthChecker_ErrorForcesUnhealthy verifies the probe adapter
// turns a probe error into an unhealthy report even if the probe returned a
// zero-valued status.
func TestNewProbeHealthChecker_ErrorForcesUnhealthy(t *testing.T) {
	checker := NewProbeHealthChecker(func(ctx context.Context) (CVEngineHealth, error) {
		return CVEngineHealth{}, errors.New("dial tcp 127.0.0.1:8001: connect: connection refused")
	})
	if checker == nil {
		t.Fatal("expected non-nil checker")
	}

	report := checker.CheckCVEngineHealth(context.Background())
	if report.Status != HealthStatusUnhealthy {
		t.Fatalf("expected unhealthy, got %q", report.Status)
	}
	if !report.Configured {
		t.Error("expected configured=true")
	}
	if report.Error == "" {
		t.Error("expected error text to be surfaced")
	}

	if NewProbeHealthChecker(nil) != nil {
		t.Error("expected nil probe to produce a nil checker")
	}
}

// TestHealthHandler_ProbeTimeoutBoundsRequest verifies a hung checker cannot
// hang the health endpoint: the request context deadline fires and the handler
// still answers 503.
func TestHealthHandler_ProbeTimeoutBoundsRequest(t *testing.T) {
	handler := NewHealthHandler(HealthCheckerFunc(func(ctx context.Context) CVEngineHealth {
		<-ctx.Done()
		return CVEngineHealth{Status: HealthStatusUnhealthy, Error: ctx.Err().Error()}
	}))
	handler.timeout = 20 * time.Millisecond

	start := time.Now()
	w, body := serveHealth(t, handler, "/health")
	elapsed := time.Since(start)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on probe timeout, got %d", w.Code)
	}
	if body.Status != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy overall status, got %q", body.Status)
	}
	if elapsed > time.Second {
		t.Errorf("health handler did not bound the probe: took %s", elapsed)
	}
}
