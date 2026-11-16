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
	rg.GET("/roads/:road_key/route", h.GetRoute)
	rg.GET("/corridors/overview", h.GetCorridorsOverview)
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

// GetRoute godoc
// @Summary Get arterial corridor route GeoJSON and hazard summary
// @Description Returns pre-computed GeoJSON FeatureCollection containing LineString and camera Points along with cumulative flood hazard, travel delay, and passability verdict.
// @Tags Corridor
// @Produce json
// @Param road_key path string true "Road Key identifier (e.g. vo_van_kiet, mai_chi_tho)"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string "Corridor not found"
// @Router /api/roads/{road_key}/route [get]
func (h *Handler) GetRoute(c *gin.Context) {
	roadKey := c.Param("road_key")
	geoJSON, summary, ok := h.service.GetRoute(roadKey)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "corridor route not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"geojson": geoJSON,
	})
}

// GetCorridorsOverview godoc
// @Summary Get operational overview of all arterial corridors
// @Description Returns list of all arterial commuter corridors with real-time passability verdicts, travel delay in seconds, and peak flood depth.
// @Tags Corridor
// @Produce json
// @Success 200 {array} corridor.CorridorRouteSummary
// @Router /api/corridors/overview [get]
func (h *Handler) GetCorridorsOverview(c *gin.Context) {
	overview := h.service.GetCorridorsOverview()
	c.JSON(http.StatusOK, overview)
}
