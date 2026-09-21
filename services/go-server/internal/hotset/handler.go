package hotset

import (
	"net/http"

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

// Handler exposes the verdict query endpoints defined in §4 of
// docs/GO_BACKEND_ARCHITECTURE.md.
type Handler struct {
	storer VerdictStorer
}

// NewHandler constructs a Handler with an injected VerdictStorer.
func NewHandler(storer VerdictStorer) *Handler {
	return &Handler{storer: storer}
}

// RegisterRoutes mounts traffic and flood status endpoints onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET(RouteTrafficStatus, h.GetTrafficStatus)
	rg.GET(RouteFloodStatus, h.GetFloodStatus)
}

// GetTrafficStatus godoc
// @Summary Get latest traffic congestion verdicts
// @Description Returns the most recent AI-analyzed congestion verdict for every camera in the Hot Set (all cameras with a stored verdict).
// @Tags Traffic
// @Produce json
// @Success 200 {array} hotset.Verdict
// @Failure 500 {object} map[string]string
// @Router /api/traffic-status [get]
func (h *Handler) GetTrafficStatus(c *gin.Context) {
	verdicts := h.storer.GetLatest()
	if verdicts == nil {
		verdicts = []Verdict{} // return [] not null in JSON
	}
	c.JSON(http.StatusOK, verdicts)
}

// GetFloodStatus godoc
// @Summary Get latest flood-detection verdicts
// @Description Placeholder — flood detection shares the same verdict store; this endpoint is out of scope for issue #8 and returns an empty list.
// @Tags Traffic
// @Produce json
// @Success 200 {array} hotset.Verdict
// @Router /api/flood-status [get]
func (h *Handler) GetFloodStatus(c *gin.Context) {
	// Out of scope (issue #8) — flood severity values come from a separate
	// Analyzer and will be wired in a later issue.
	c.JSON(http.StatusOK, []Verdict{})
}
