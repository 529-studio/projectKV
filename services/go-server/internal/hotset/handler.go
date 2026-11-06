package hotset

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// VerdictStorer is defined here (consumer package) so hotset does NOT need to
// import internal/store directly. The concrete implementation (store.VerdictRepo
// via an adapter) satisfies this interface and is wired in cmd/*/main.go.
// (Dependency Inversion & Interface Segregation Principles)
type VerdictStorer interface {
	// GetLatest returns the most recent Verdict for every camera currently
	// tracked in the store (all rows in camera_verdicts).
	GetLatest() []Verdict
}

// HistoryCoverageProvider defines consumer access to time-series data coverage (ADR-012 D2).
type HistoryCoverageProvider interface {
	GetCoverage(ctx context.Context, cameraID string, window time.Duration, now time.Time) (*CoverageReport, error)
}

// ObservationBounds holds the boundary timestamps for an observation window.
type ObservationBounds struct {
	Earliest *time.Time `json:"earliest,omitempty"`
	Latest   *time.Time `json:"latest,omitempty"`
}

// CoverageReport represents the continuous data accumulation and bin coverage statistics (ADR-012 D2).
type CoverageReport struct {
	CameraID             string            `json:"camera_id,omitempty"`
	Window               string            `json:"window"`
	BinIntervalMinutes   int               `json:"bin_interval_minutes"`
	ExpectedBins         int               `json:"expected_bins"`
	ObservedBins         int               `json:"observed_bins"`
	CoverageRatio        float64           `json:"coverage_ratio"`
	TotalObservations    int               `json:"total_observations"`
	ObservationBounds    ObservationBounds `json:"observation_bounds"`
	LongestGapSeconds    int64             `json:"longest_gap_seconds"`
	HorizonEligibility   map[string]bool   `json:"horizon_eligibility"`
	HistoryCoverageHours float64           `json:"history_coverage_hours"`
	Status               string            `json:"status"`
	CamerasCount         int               `json:"cameras_count,omitempty"`
}

// TrafficStatusResponse represents the response payload for GET /api/traffic-status.
type TrafficStatusResponse struct {
	AnalysisEnabled bool      `json:"analysis_enabled"`
	AppEnv          string    `json:"app_env,omitempty"`
	Verdicts        []Verdict `json:"verdicts"`
}

// FloodStatusResponse represents the response payload for GET /api/flood-status.
type FloodStatusResponse struct {
	AnalysisEnabled bool      `json:"analysis_enabled"`
	AppEnv          string    `json:"app_env,omitempty"`
	Verdicts        []Verdict `json:"verdicts"`
}

// HotsetStatusResponse represents the response payload for GET /api/v1/cameras/hotset/status.
type HotsetStatusResponse struct {
	Total     int       `json:"total"`
	UpdatedAt time.Time `json:"updated_at"`
	Cameras   []Verdict `json:"cameras"`
}

// Handler exposes the verdict query endpoints defined in §4 of
// docs/GO_BACKEND_ARCHITECTURE.md.
type Handler struct {
	storer          VerdictStorer
	coverage        HistoryCoverageProvider
	analysisEnabled bool
	appEnv          string
	staleTTL        time.Duration
}

// NewHandler constructs a Handler with an injected VerdictStorer (default analysis disabled).
func NewHandler(storer VerdictStorer) *Handler {
	return NewHandlerWithConfig(storer, false, "production")
}

// NewHandlerWithConfig constructs a Handler with explicit analysis state and environment.
func NewHandlerWithConfig(storer VerdictStorer, analysisEnabled bool, appEnv string) *Handler {
	return &Handler{
		storer:          storer,
		analysisEnabled: analysisEnabled,
		appEnv:          appEnv,
		staleTTL:        180 * time.Second,
	}
}

// SetCoverageProvider attaches a HistoryCoverageProvider to the Handler.
func (h *Handler) SetCoverageProvider(cp HistoryCoverageProvider) {
	h.coverage = cp
}

// SetStaleTTL sets the duration after which a verdict is marked stale.
func (h *Handler) SetStaleTTL(d time.Duration) {
	if d > 0 {
		h.staleTTL = d
	}
}

// RegisterRoutes mounts traffic and flood status endpoints onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET(RouteTrafficStatus, h.GetTrafficStatus)
	rg.GET(RouteFloodStatus, h.GetFloodStatus)
	rg.GET("/config", h.GetConfig)
	rg.GET(RouteHotsetStatus, h.GetHotsetStatus)
	rg.GET("/v1"+RouteHotsetStatus, h.GetHotsetStatus)
	rg.GET("/v1/hotset/history/coverage", h.GetHistoryCoverage)
	rg.GET("/hotset/history/coverage", h.GetHistoryCoverage)
}

// GetTrafficStatus godoc
// @Summary Get latest traffic congestion verdicts
// @Description Returns the most recent AI-analyzed congestion verdict for cameras in the Hot Set along with analysis_enabled status. When APP_ENV is not dev, verdicts with source=mock are strictly omitted.
// @Tags Traffic
// @Produce json
// @Success 200 {object} hotset.TrafficStatusResponse
// @Failure 500 {object} map[string]string
// @Router /api/traffic-status [get]
func (h *Handler) GetTrafficStatus(c *gin.Context) {
	allVerdicts := h.storer.GetLatest()
	if allVerdicts == nil {
		allVerdicts = []Verdict{}
	}

	staleThreshold := h.staleTTL
	if staleThreshold <= 0 {
		staleThreshold = 180 * time.Second
	}

	filtered := make([]Verdict, 0, len(allVerdicts))
	for _, v := range allVerdicts {
		// Provenance safety rule: filter out source=mock when appEnv is not "dev"
		if strings.ToLower(v.Source) == "mock" && strings.ToLower(h.appEnv) != "dev" {
			continue
		}

		if v.ObservedAt.IsZero() && !v.Timestamp.IsZero() {
			v.ObservedAt = v.Timestamp
		}

		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}
		if !v.Stale && !obs.IsZero() && time.Since(obs) > staleThreshold {
			v.Stale = true
		}

		filtered = append(filtered, v)
	}

	c.Header("X-Analysis-Enabled", fmt.Sprintf("%t", h.analysisEnabled))
	c.JSON(http.StatusOK, TrafficStatusResponse{
		AnalysisEnabled: h.analysisEnabled,
		AppEnv:          h.appEnv,
		Verdicts:        filtered,
	})
}

// GetConfig godoc
// @Summary Get system analysis configuration
// @Description Returns lightweight runtime configuration including whether AI analysis is enabled.
// @Tags System
// @Produce json
// @Success 200 {object} map[string]any
// @Router /api/config [get]
func (h *Handler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"analysis_enabled": h.analysisEnabled,
		"app_env":          h.appEnv,
	})
}

// GetFloodStatus godoc
// @Summary Get latest flood-detection verdicts
// @Description Returns the most recent AI-analyzed flood and rain verdicts for cameras in the Hot Set along with analysis_enabled status. When APP_ENV is not dev, verdicts with source=mock are strictly omitted. Unverified cameras are omitted per honesty rules.
// @Tags Flood
// @Produce json
// @Success 200 {object} hotset.FloodStatusResponse
// @Failure 500 {object} map[string]string
// @Router /api/flood-status [get]
func (h *Handler) GetFloodStatus(c *gin.Context) {
	allVerdicts := h.storer.GetLatest()
	if allVerdicts == nil {
		allVerdicts = []Verdict{}
	}

	staleThreshold := h.staleTTL
	if staleThreshold <= 0 {
		staleThreshold = 180 * time.Second
	}

	filtered := make([]Verdict, 0, len(allVerdicts))
	for _, v := range allVerdicts {
		// Provenance safety rule: filter out source=mock when appEnv is not "dev"
		if strings.ToLower(v.Source) == "mock" && strings.ToLower(h.appEnv) != "dev" {
			continue
		}

		// Normalize/determine flood severity
		floodSev := v.FloodSeverity
		if floodSev == "" {
			switch strings.ToUpper(strings.TrimSpace(v.Severity)) {
			case SeverityDry, SeverityWet, SeverityFloodedShallow, SeverityFloodedDeep:
				floodSev = strings.ToUpper(strings.TrimSpace(v.Severity))
			}
		}

		// Honesty rule: Unverified cameras (empty or unknown flood severity) are omitted.
		if floodSev == "" || floodSev == SeverityUnknown {
			continue
		}
		v.FloodSeverity = floodSev

		if v.ObservedAt.IsZero() && !v.Timestamp.IsZero() {
			v.ObservedAt = v.Timestamp
		}

		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}
		if !v.Stale && !obs.IsZero() && time.Since(obs) > staleThreshold {
			v.Stale = true
		}

		filtered = append(filtered, v)
	}

	c.Header("X-Analysis-Enabled", fmt.Sprintf("%t", h.analysisEnabled))
	c.JSON(http.StatusOK, FloodStatusResponse{
		AnalysisEnabled: h.analysisEnabled,
		AppEnv:          h.appEnv,
		Verdicts:        filtered,
	})
}

// GetHotsetStatus godoc
// @Summary Get latest 50 Hot Set traffic status and Saturation Index
// @Description Returns cached traffic severity, saturation index, and PCU counts for the 50 Hot Set priority arterial cameras with sub-5ms latency.
// @Tags Cameras, Traffic
// @Produce json
// @Success 200 {object} hotset.HotsetStatusResponse
// @Failure 500 {object} map[string]string
// @Router /api/v1/cameras/hotset/status [get]
func (h *Handler) GetHotsetStatus(c *gin.Context) {
	allVerdicts := h.storer.GetLatest()
	if allVerdicts == nil {
		allVerdicts = []Verdict{}
	}

	staleThreshold := h.staleTTL
	if staleThreshold <= 0 {
		staleThreshold = 180 * time.Second
	}

	filtered := make([]Verdict, 0, len(allVerdicts))
	for _, v := range allVerdicts {
		// Provenance safety rule: filter out source=mock when appEnv is not "dev"
		if strings.ToLower(v.Source) == "mock" && strings.ToLower(h.appEnv) != "dev" {
			continue
		}

		if v.ObservedAt.IsZero() && !v.Timestamp.IsZero() {
			v.ObservedAt = v.Timestamp
		}

		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}
		if !v.Stale && !obs.IsZero() && time.Since(obs) > staleThreshold {
			v.Stale = true
		}

		filtered = append(filtered, v)
	}

	c.Header("X-Analysis-Enabled", fmt.Sprintf("%t", h.analysisEnabled))
	c.JSON(http.StatusOK, HotsetStatusResponse{
		Total:     len(filtered),
		UpdatedAt: time.Now(),
		Cameras:   filtered,
	})
}

// GetHistoryCoverage godoc
// @Summary      Get Hot Set time-series data accumulation and bin coverage
// @Description  Reports observation bounds, 10m bin counts, coverage ratio, longest gap, and per-horizon prediction eligibility across a trailing window (default 72h).
// @Tags         Traffic
// @Produce      json
// @Param        camera_id query string false "Camera ID (empty for aggregate across all cameras)"
// @Param        window query string false "Time window duration (e.g. 72h, 24h, 60m; default 72h)"
// @Success      200 {object} hotset.CoverageReport
// @Failure      400 {object} map[string]string "invalid window format"
// @Failure      503 {object} map[string]string "coverage provider unavailable"
// @Router       /api/v1/hotset/history/coverage [get]
func (h *Handler) GetHistoryCoverage(c *gin.Context) {
	if h.coverage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "history coverage provider unavailable",
		})
		return
	}

	camID := strings.TrimSpace(c.Query("camera_id"))
	windowStr := strings.TrimSpace(c.Query("window"))
	window := 72 * time.Hour
	if windowStr != "" {
		w, err := time.ParseDuration(windowStr)
		if err != nil || w <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("invalid window %q: must be a positive duration string (e.g. 72h, 24h, 60m)", windowStr),
			})
			return
		}
		window = w
	}

	rep, err := h.coverage.GetCoverage(c.Request.Context(), camID, window, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to get history coverage: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, rep)
}
