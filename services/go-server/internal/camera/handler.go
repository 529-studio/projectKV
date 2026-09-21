package camera

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler handles HTTP requests for camera catalog and query operations.
// (Single Responsibility Principle: deals strictly with HTTP transport & encoding)
type Handler struct {
	reader Reader
}

// NewHandler constructs a Handler with an injected Reader interface.
// (Dependency Inversion Principle: depends on Reader abstraction, not concrete struct)
func NewHandler(reader Reader) *Handler {
	return &Handler{reader: reader}
}

// RegisterRoutes mounts camera module routes onto the provided gin.RouterGroup.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET(RouteCameras, h.GetCameras)
	rg.GET(RouteCameraDetail, h.GetCameraByID)
	rg.GET(RouteZones, h.GetZones)
}

// RegisterRoutes is a convenience package-level function to mount camera endpoints.
func RegisterRoutes(rg *gin.RouterGroup, reader Reader) {
	h := NewHandler(reader)
	h.RegisterRoutes(rg)
}

// GetCameras godoc
// @Summary List cameras
// @Description Retrieve full camera catalog with optional zone and corridor filters
// @Tags Camera
// @Produce json
// @Param zone query string false "Filter by administrative or geographic zone"
// @Param corridor query string false "Filter by corridor keyword"
// @Success 200 {array} Camera
// @Router /api/cameras [get]
func (h *Handler) GetCameras(c *gin.Context) {
	zone := c.Query(ParamZone)
	corridor := c.Query(ParamCorridor)

	cameras := h.reader.Filter(zone, corridor)
	c.JSON(http.StatusOK, cameras)
}

// GetCameraByID godoc
// @Summary Get camera by ID
// @Description Retrieve single camera metadata by its unique identifier
// @Tags Camera
// @Produce json
// @Param id path string true "Camera ID"
// @Success 200 {object} Camera
// @Failure 404 {object} map[string]string
// @Router /api/cameras/{id} [get]
func (h *Handler) GetCameraByID(c *gin.Context) {
	id := c.Param(ParamID)
	if cam, ok := h.reader.ByID(id); ok {
		c.JSON(http.StatusOK, cam)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": ErrCameraNotFound})
}

// GetZones godoc
// @Summary List camera zones
// @Description Retrieve sorted list of valid camera zone identifiers
// @Tags Camera
// @Produce json
// @Success 200 {array} string
// @Router /api/zones [get]
func (h *Handler) GetZones(c *gin.Context) {
	c.JSON(http.StatusOK, h.reader.Zones())
}
