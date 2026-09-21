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

// GetCameras handles GET /cameras with optional zone and corridor filter query parameters.
func (h *Handler) GetCameras(c *gin.Context) {
	zone := c.Query(ParamZone)
	corridor := c.Query(ParamCorridor)

	cameras := h.reader.Filter(zone, corridor)
	c.JSON(http.StatusOK, cameras)
}

// GetCameraByID handles GET /cameras/:id returning camera metadata or 404.
func (h *Handler) GetCameraByID(c *gin.Context) {
	id := c.Param(ParamID)
	if cam, ok := h.reader.ByID(id); ok {
		c.JSON(http.StatusOK, cam)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": ErrCameraNotFound})
}

// GetZones handles GET /zones returning the sorted list of valid zones.
func (h *Handler) GetZones(c *gin.Context) {
	c.JSON(http.StatusOK, h.reader.Zones())
}
