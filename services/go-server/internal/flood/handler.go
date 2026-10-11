package flood

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler handles HTTP requests for the flood verification subsystem.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler with an injected Service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts flood endpoints onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	floodGroup := rg.Group("/flood")
	{
		floodGroup.POST("/verify", h.VerifyCamera)
		floodGroup.GET("/chokepoints", h.GetChokepoints)
		floodGroup.GET("/quota", h.GetQuota)
	}
}

// VerifyCamera godoc
// @Summary      Verify flood status for a camera
// @Description  Triggers on-demand VLM multimodal flood verification for a camera, strictly guarded by 10 RPM and 300 RPD quotas
// @Tags         flood
// @Accept       json
// @Produce      json
// @Param        camera_id query string true "Camera ID to verify"
// @Success      200 {object} FloodVerificationResult
// @Failure      400 {object} map[string]string "camera_id is required"
// @Failure      404 {object} map[string]string "camera not found"
// @Failure      429 {object} map[string]any "rate limit or daily quota exceeded"
// @Failure      503 {object} map[string]string "upstream VLM or snapshot failure"
// @Router       /flood/verify [post]
func (h *Handler) VerifyCamera(c *gin.Context) {
	cameraID := c.Query("camera_id")
	if cameraID == "" {
		cameraID = c.Param("id")
	}
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera_id is required"})
		return
	}

	result, err := h.service.VerifyCamera(c.Request.Context(), cameraID)
	if err != nil {
		if errors.Is(err, ErrCameraNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found in catalog"})
			return
		}
		if errors.Is(err, ErrRateLimited) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded (10 RPM)",
				"quota": h.service.QuotaStatus(),
			})
			return
		}
		if errors.Is(err, ErrDailyQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "daily quota exceeded (300 RPD)",
				"quota": h.service.QuotaStatus(),
			})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetChokepoints godoc
// @Summary      List authentic municipal flood chokepoints
// @Description  Returns curated HCMC flood chokepoints and their latest verified statuses
// @Tags         flood
// @Produce      json
// @Success      200 {object} map[string]any
// @Router       /flood/chokepoints [get]
func (h *Handler) GetChokepoints(c *gin.Context) {
	cps := h.service.Chokepoints()
	latest := h.service.LatestVerdicts()

	type chokepointItem struct {
		FloodChokepoint
		LatestResult *FloodVerificationResult `json:"latest_result,omitempty"`
	}

	items := make([]chokepointItem, len(cps))
	for i, cp := range cps {
		items[i] = chokepointItem{
			FloodChokepoint: cp,
		}
		if res, ok := latest[cp.CameraID]; ok {
			items[i].LatestResult = &res
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"chokepoints": items,
		"total":       len(items),
	})
}

// GetQuota godoc
// @Summary      Get flood verification quota status
// @Description  Returns sliding-window RPM (10 max) and daily RPD (300 max) quota utilization
// @Tags         flood
// @Produce      json
// @Success      200 {object} QuotaStatus
// @Router       /flood/quota [get]
func (h *Handler) GetQuota(c *gin.Context) {
	status := h.service.QuotaStatus()
	c.JSON(http.StatusOK, status)
}
