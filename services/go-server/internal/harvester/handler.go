package harvester

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes the harvester observability endpoint (Issue #299).
// Leaf package: imports only Gin, no internal packages.
type Handler struct {
	coord   *Coordinator
	enabled bool
}

// NewHandler constructs the harvester status handler. A nil coordinator
// (subsystem unavailable: storage driver missing or init failure) is served
// as HTTP 503 rather than panicking.
func NewHandler(coord *Coordinator, enabled bool) *Handler {
	return &Handler{coord: coord, enabled: enabled}
}

// RegisterRoutes mounts the harvester endpoints behind dev authentication.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	group := rg.Group("/harvester", auth)
	group.GET("/status", h.GetStatus)
}

// GetStatus godoc
// @Summary      Harvester subsystem status
// @Description  Observability snapshot of the autonomous data flywheel: schedule slots, next slot, per-slot last runs, rain watcher state, lifetime totals, and recent errors. Protected by dev authentication.
// @Tags         harvester
// @Produce      json
// @Success      200 {object} harvester.Status
// @Failure      503 {object} map[string]string "harvester subsystem unavailable"
// @Router       /harvester/status [get]
func (h *Handler) GetStatus(c *gin.Context) {
	if h.coord == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "harvester subsystem unavailable (storage driver uninitialized or initialization failed)",
			"enabled": false,
		})
		return
	}
	status := h.coord.Status()
	if !h.enabled {
		status.Enabled = false
	}
	c.JSON(http.StatusOK, status)
}
