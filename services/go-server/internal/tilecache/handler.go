package tilecache

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler serves map tiles from local disk cache or upstream OpenStreetMap.
type Handler struct {
	manager *Manager
}

// NewHandler constructs a Handler with the given tilecache Manager.
func NewHandler(manager *Manager) *Handler {
	return &Handler{
		manager: manager,
	}
}

// RegisterRoutes mounts the tile proxy endpoints onto the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/tiles/:z/:x/:y", h.GetTile)
	rg.HEAD("/tiles/:z/:x/:y", h.GetTile)
}

// GetTile godoc
// @Summary Get map tile (cached proxy)
// @Description Serves OpenStreetMap raster tiles (PNG) cached on local disk with 30-day immutable HTTP cache headers.
// @Tags Map
// @Produce image/png
// @Param z path int true "Zoom level (0-19)"
// @Param x path int true "X coordinate"
// @Param y path string true "Y coordinate (with or without .png extension)"
// @Success 200 {string} binary "PNG image data"
// @Failure 400 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Router /api/tiles/{z}/{x}/{y} [get]
func (h *Handler) GetTile(c *gin.Context) {
	zStr := c.Param("z")
	xStr := c.Param("x")
	yStr := strings.TrimSuffix(c.Param("y"), ".png")

	z, errZ := strconv.Atoi(zStr)
	x, errX := strconv.Atoi(xStr)
	y, errY := strconv.Atoi(yStr)

	if errZ != nil || errX != nil || errY != nil || !ValidateCoords(z, x, y) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tile coordinates"})
		return
	}

	data, isHit, err := h.manager.GetTile(c.Request.Context(), z, x, y)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to get tile: %v", err)})
		return
	}

	cacheStatus := "MISS"
	if isHit {
		cacheStatus = "HIT"
	}

	c.Header("X-Tile-Cache", cacheStatus)
	c.Header("Cache-Control", "public, max-age=2592000, immutable")
	c.Data(http.StatusOK, "image/png", data)
}
