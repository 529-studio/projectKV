package corridor

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes corridor status and road inventory endpoints.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler with the given Service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts corridor HTTP endpoints onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/roads", h.ListRoads)
	rg.GET("/roads/:road_key/status", h.GetStatus)
}

// GetStatus godoc
// @Summary Get corridor traffic status
// @Description Returns the aggregated traffic status for a named road corridor identified by road_key, including worst severity, coverage percentage, oldest observation age, and per-camera verdicts. Enforces ADR-004 §D4 honesty rules.
// @Tags Corridor
// @Produce json
// @Param road_key path string true "Road Key identifier (e.g. vo_van_kiet, quoc_lo_1)"
// @Success 200 {object} corridor.CorridorStatus
// @Failure 404 {object} map[string]string "Corridor not found"
// @Router /api/roads/{road_key}/status [get]
func (h *Handler) GetStatus(c *gin.Context) {
	roadKey := c.Param("road_key")
	status, ok := h.service.GetStatus(roadKey)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "corridor not found"})
		return
	}
	c.JSON(http.StatusOK, status)
}

// ListRoads godoc
// @Summary List all known road keys
// @Description Returns list of all known road keys with associated camera counts, sorted by camera density descending.
// @Tags Corridor
// @Produce json
// @Success 200 {object} corridor.RoadsResponse
// @Router /api/roads [get]
func (h *Handler) ListRoads(c *gin.Context) {
	roads := h.service.ListRoads()
	c.JSON(http.StatusOK, roads)
}
