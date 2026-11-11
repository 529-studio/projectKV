package camera

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CatalogCacheControl specifies the Cache-Control directive for static camera catalogs.
const CatalogCacheControl = "public, max-age=300, stale-while-revalidate=600"

// checkETagMatch checks if the client's If-None-Match header matches the given etag using weak comparison.
func checkETagMatch(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	rawETag := strings.TrimPrefix(etag, "W/")
	for _, tag := range strings.Split(ifNoneMatch, ",") {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "W/")
		if tag == rawETag {
			return true
		}
	}
	return false
}

// serveJSONWithETag writes a JSON payload with Cache-Control and ETag headers,
// handling conditional requests (If-None-Match -> 304 Not Modified).
func serveJSONWithETag(c *gin.Context, data []byte) {
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(data))
	c.Header("Cache-Control", CatalogCacheControl)
	c.Header("ETag", etag)

	if checkETagMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

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
	rg.GET(RoutePhuong, h.GetPhuongs)
}

// RegisterRoutes is a convenience package-level function to mount camera endpoints.
func RegisterRoutes(rg *gin.RouterGroup, reader Reader) {
	h := NewHandler(reader)
	h.RegisterRoutes(rg)
}

// GetCameras godoc
// @Summary List cameras
// @Description Retrieve full camera catalog with optional zone, corridor, and phuong filters
// @Tags Camera
// @Produce json
// @Param zone query string false "Filter by administrative or geographic zone"
// @Param corridor query string false "Filter by corridor keyword"
// @Param phuong query string false "Filter by post-reform Phường name"
// @Success 200 {array} Camera
// @Router /api/cameras [get]
func (h *Handler) GetCameras(c *gin.Context) {
	zone := c.Query(ParamZone)
	corridor := c.Query(ParamCorridor)
	phuong := c.Query(ParamPhuong)

	var cameras []Camera
	if phuong != "" {
		cameras = h.reader.FilterByPhuong(phuong)
		if zone != "" || corridor != "" {
			var filtered []Camera
			corridorLower := strings.ToLower(strings.TrimSpace(corridor))
			for _, cam := range cameras {
				if zone != "" && !strings.EqualFold(cam.Zone, zone) {
					continue
				}
				if corridorLower != "" {
					name := strings.ToLower(cam.DisplayName)
					title := strings.ToLower(cam.Title)
					if !strings.Contains(name, corridorLower) && !strings.Contains(title, corridorLower) {
						continue
					}
				}
				filtered = append(filtered, cam)
			}
			cameras = filtered
		}
	} else {
		cameras = h.reader.Filter(zone, corridor)
	}

	if cameras == nil {
		cameras = []Camera{}
	}
	data, err := json.Marshal(cameras)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode cameras"})
		return
	}
	serveJSONWithETag(c, data)
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
	zones := h.reader.Zones()
	if zones == nil {
		zones = []string{}
	}
	data, err := json.Marshal(zones)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode zones"})
		return
	}
	serveJSONWithETag(c, data)
}

// GetPhuongs godoc
// @Summary List camera wards (Phường)
// @Description Retrieve sorted list of distinct post-reform Phường structured pairs
// @Tags Camera
// @Produce json
// @Success 200 {array} Phuong
// @Router /api/phuong [get]
func (h *Handler) GetPhuongs(c *gin.Context) {
	phuongs := h.reader.Phuongs()
	if phuongs == nil {
		phuongs = []Phuong{}
	}
	c.JSON(http.StatusOK, phuongs)
}
