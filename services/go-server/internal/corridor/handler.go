package corridor

import (
	"net/http"
	"strconv"
	"strings"

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
	rg.GET("/roads/:road_key/history", h.GetHistory)
	rg.GET("/roads/status-summary", h.GetStatusSummary)
	rg.GET("/roads/:road_key/route", h.GetRoute)
	rg.GET("/corridors/overview", h.GetCorridorsOverview)
	rg.GET("/corridors/:road_key/history", h.GetHistory)
	rg.GET("/corridors/commute-eval", h.EvaluateCommute)
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

// GetStatusSummary godoc
// @Summary Get aggregated status for all corridors
// @Description Returns a map of corridor statuses for all known road keys.
// @Tags Corridor
// @Produce json
// @Success 200 {object} map[string]CorridorStatus
// @Router /api/roads/status-summary [get]
func (h *Handler) GetStatusSummary(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.GetStatusSummary())
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
// @Param vehicle query string false "Vehicle clearance profile (scooter, motorbike, sedan, suv)"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string "Corridor not found"
// @Router /api/roads/{road_key}/route [get]
func (h *Handler) GetRoute(c *gin.Context) {
	roadKey := c.Param("road_key")
	vType := NormalizeVehicleType(c.Query("vehicle"))
	geoJSON, summary, ok := h.service.GetRoute(roadKey, vType)
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
// @Param vehicle query string false "Vehicle clearance profile (scooter, motorbike, sedan, suv)"
// @Success 200 {array} corridor.CorridorRouteSummary
// @Router /api/corridors/overview [get]
func (h *Handler) GetCorridorsOverview(c *gin.Context) {
	vType := NormalizeVehicleType(c.Query("vehicle"))
	overview := h.service.GetCorridorsOverview(vType)
	c.JSON(http.StatusOK, overview)
}

// EvaluateCommute godoc
// @Summary Evaluate multi-corridor commuter journey
// @Description Calculates aggregated passability verdict, trip duration, delay, worst hazard bottleneck, and alternative bypass recommendations for commuter corridors.
// @Tags Corridor
// @Produce json
// @Param corridors query string true "Comma-separated corridor keys (e.g. nguyen_huu_canh,dien_bien_phu)"
// @Param vehicle query string false "Vehicle clearance profile (scooter, motorbike, sedan, suv)"
// @Success 200 {object} corridor.CommuteEvaluationResponse
// @Failure 400 {object} map[string]string "Bad Request"
// @Router /api/corridors/commute-eval [get]
func (h *Handler) EvaluateCommute(c *gin.Context) {
	rawCorridors := c.Query("corridors")
	if strings.TrimSpace(rawCorridors) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corridors query parameter is required (comma-separated corridor keys)"})
		return
	}

	parts := strings.Split(rawCorridors, ",")
	corridorKeys := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			corridorKeys = append(corridorKeys, trimmed)
		}
	}

	if len(corridorKeys) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid corridor keys provided"})
		return
	}

	vType := NormalizeVehicleType(c.Query("vehicle"))
	result := h.service.EvaluateCommute(corridorKeys, vType)
	c.JSON(http.StatusOK, result)
}

// GetHistory godoc
// @Summary Get downsampled corridor flood history and tidal correlation
// @Description Returns hourly historical flood depths (average and peak) and correlated astronomical river tide elevations for a corridor.
// @Tags Corridor
// @Produce json
// @Param road_key path string true "Road Key identifier (e.g. vo_van_kiet, nguyen_huu_canh)"
// @Param hours query int false "Number of hours of history to query (default 24, min 1, max 72)"
// @Success 200 {object} corridor.CorridorHistoryResponse
// @Failure 404 {object} map[string]string "Corridor not found"
// @Router /api/roads/{road_key}/history [get]
func (h *Handler) GetHistory(c *gin.Context) {
	roadKey := c.Param("road_key")
	hours := 24
	if hStr := c.Query("hours"); hStr != "" {
		if val, err := strconv.Atoi(hStr); err == nil {
			hours = val
		}
	}

	history, ok := h.service.GetHistory(roadKey, hours)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "corridor not found"})
		return
	}
	c.JSON(http.StatusOK, history)
}
