package hotset

import (
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

// TrafficStatusResponse represents the response payload for GET /api/traffic-status.
type TrafficStatusResponse struct {
	AnalysisEnabled bool      `json:"analysis_enabled"`
	AppEnv          string    `json:"app_env,omitempty"`
	Verdicts        []Verdict `json:"verdicts"`
}

// Handler exposes the verdict query endpoints defined in §4 of
// docs/GO_BACKEND_ARCHITECTURE.md.
type Handler struct {
	storer          VerdictStorer
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
// @Description Placeholder — flood detection shares the same verdict store; this endpoint is out of scope for issue #8 and returns an empty list.
// @Tags Traffic
// @Produce json
// @Success 200 {array} hotset.Verdict
// @Failure 500 {object} map[string]string
// @Router /api/flood-status [get]
func (h *Handler) GetFloodStatus(c *gin.Context) {
	// Out of scope (issue #8) — flood severity values come from a separate
	// Analyzer and will be wired in a later issue.
	c.JSON(http.StatusOK, []Verdict{})
}
