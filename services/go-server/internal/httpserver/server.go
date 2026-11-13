package httpserver

import (
	"context"
	"net/http"
	"time"

	_ "github.com/529-studio/projectKV/services/go-server/docs"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title Camera View API
// @version 1.0
// @description Ho Chi Minh City Traffic Camera & Flood Alert API
// @BasePath /

// Health status values surfaced by /health, /healthz and /api/health. The
// overall status is the cv-engine status when a checker is wired, and healthy
// otherwise (nothing downstream is required).
const (
	HealthStatusHealthy   = "healthy"
	HealthStatusDegraded  = "degraded"
	HealthStatusUnhealthy = "unhealthy"

	// healthProbeTimeout bounds a single cv-engine health probe so a hung
	// engine cannot hang the orchestration health check.
	healthProbeTimeout = 3 * time.Second
)

// CVEngineHealth is the point-in-time report for the cv-engine service that
// the health endpoints surface under the "cv_engine" key. It mirrors
// cvclient.CVEngineHealth without importing that package, so httpserver stays
// a leaf package.
type CVEngineHealth struct {
	// Status is one of HealthStatusHealthy, HealthStatusDegraded or
	// HealthStatusUnhealthy.
	Status string `json:"status"`
	// Configured is false when no probe is wired (for example ANALYZER=none).
	// The engine is then not part of the serving path and Status is healthy.
	Configured bool `json:"configured"`
	// Service is the service name reported by the engine's /health endpoint.
	Service string `json:"service,omitempty"`
	// OpenVINOAvailable reports whether the engine can access its runtime.
	OpenVINOAvailable bool `json:"openvino_available"`
	// ModelLoaded reports whether an inference model is loaded.
	ModelLoaded bool `json:"model_loaded"`
	// LatencyMs is the round-trip duration of the probe in milliseconds.
	LatencyMs float64 `json:"latency_ms"`
	// Error carries the failure or degradation reason.
	Error string `json:"error,omitempty"`
	// CheckedAt is when the probe started.
	CheckedAt time.Time `json:"checked_at"`
}

// HealthChecker reports cv-engine health. cmd/server adapts the
// cvclient.CVEngineAnalyzer probe to this interface; tests may supply any stub.
type HealthChecker interface {
	CheckCVEngineHealth(ctx context.Context) CVEngineHealth
}

// HealthCheckerFunc adapts a plain function to HealthChecker.
type HealthCheckerFunc func(ctx context.Context) CVEngineHealth

// CheckCVEngineHealth implements HealthChecker.
func (f HealthCheckerFunc) CheckCVEngineHealth(ctx context.Context) CVEngineHealth {
	return f(ctx)
}

// CVEngineHealthProbe is the shape of cvclient.CVEngineAnalyzer.ProbeHealth:
// a health report plus the probe error, when the engine could not be reached.
type CVEngineHealthProbe func(ctx context.Context) (CVEngineHealth, error)

// NewProbeHealthChecker adapts a CVEngineHealthProbe to a HealthChecker. A
// probe error forces the unhealthy status even if the probe forgot to set it.
func NewProbeHealthChecker(probe CVEngineHealthProbe) HealthChecker {
	if probe == nil {
		return nil
	}
	return HealthCheckerFunc(func(ctx context.Context) CVEngineHealth {
		report, err := probe(ctx)
		report.Configured = true
		if err != nil {
			report.Status = HealthStatusUnhealthy
			if report.Error == "" {
				report.Error = err.Error()
			}
		}
		if report.Status == "" {
			if err != nil {
				report.Status = HealthStatusUnhealthy
			} else {
				report.Status = HealthStatusHealthy
			}
		}
		return report
	})
}

// HealthHandler serves the health aliases with an optional cv-engine probe.
type HealthHandler struct {
	checker HealthChecker
	timeout time.Duration
}

// NewHealthHandler builds a health handler. A nil checker keeps the endpoint
// as a pure liveness probe reporting healthy.
func NewHealthHandler(checker HealthChecker) *HealthHandler {
	return &HealthHandler{checker: checker, timeout: healthProbeTimeout}
}

// Healthz godoc
// @Summary Health check
// @Description Liveness and readiness probe reporting system, schema and cv-engine readiness
// @Tags System
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 503 {object} map[string]any
// @Router /healthz [get]
// @Router /health [get]
// @Router /api/health [get]
func (h *HealthHandler) Healthz(c *gin.Context) {
	cvEngine := CVEngineHealth{
		Status:     HealthStatusHealthy,
		Configured: h != nil && h.checker != nil,
	}

	overall := HealthStatusHealthy
	if h != nil && h.checker != nil {
		timeout := h.timeout
		if timeout <= 0 {
			timeout = healthProbeTimeout
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		cvEngine = h.checker.CheckCVEngineHealth(ctx)
		cvEngine.Configured = true
		if cvEngine.Status == "" {
			cvEngine.Status = HealthStatusUnhealthy
		}
		overall = cvEngine.Status
	}

	code := http.StatusOK
	if overall == HealthStatusUnhealthy {
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, gin.H{
		"status": overall,
		"schema": gin.H{
			"history_ready": true,
		},
		"cv_engine": cvEngine,
	})
}

// defaultHealthHandler backs the package-level Healthz helper, which callers
// that have not wired a cv-engine probe can register directly.
var defaultHealthHandler = NewHealthHandler(nil)

// Healthz serves the health check with no cv-engine probe wired. Prefer
// NewHealthHandler(checker).Healthz when a checker is available.
func Healthz(c *gin.Context) {
	defaultHealthHandler.Healthz(c)
}

// Option configures NewServer.
type Option func(*serverOptions)

type serverOptions struct {
	healthChecker HealthChecker
}

// WithHealthChecker wires a cv-engine health checker into the /health,
// /healthz and /api/health endpoints.
func WithHealthChecker(checker HealthChecker) Option {
	return func(o *serverOptions) {
		o.healthChecker = checker
	}
}

// NewServer builds the standalone health/swagger server.
func NewServer(opts ...Option) *gin.Engine {
	var o serverOptions
	for _, opt := range opts {
		opt(&o)
	}
	health := NewHealthHandler(o.healthChecker)

	r := gin.Default()

	// Health check endpoints
	r.GET("/health", health.Healthz)
	r.HEAD("/health", health.Healthz)
	r.GET("/healthz", health.Healthz)
	r.GET("/api/health", health.Healthz)

	// Swagger documentation UI
	swaggerHandler := ginSwagger.WrapHandler(swaggerFiles.Handler)
	r.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	r.GET("/swagger/*any", func(c *gin.Context) {
		p := c.Param("any")
		if p == "" || p == "/" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		swaggerHandler(c)
	})

	return r
}
