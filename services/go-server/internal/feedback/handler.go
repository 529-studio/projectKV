package feedback

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler processes crowd-sourced flood feedback submissions.
type Handler struct {
	checker  CameraChecker
	limiter  *RateLimiter
	archiver *Archiver
	loc      *time.Location
}

// NewHandler constructs a feedback Handler with injected dependencies.
func NewHandler(checker CameraChecker, limiter *RateLimiter, archiver *Archiver) *Handler {
	if limiter == nil {
		limiter = NewRateLimiter(DefaultRateLimitCooldown)
	}
	return &Handler{
		checker:  checker,
		limiter:  limiter,
		archiver: archiver,
		loc:      time.FixedZone("ICT", 7*3600),
	}
}

// RegisterRoutes mounts the feedback submission endpoints onto the provided Gin router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/v1/cameras/:id/report-flood", h.ReportFlood)
	rg.POST("/cameras/:id/report-flood", h.ReportFlood)
}

// ReportFlood godoc
// @Summary Report road flooding at camera location
// @Description Accepts crowd-sourced flood observation, enforces 5-minute per-IP rate limit, and asynchronously archives snapshot to Cloudflare R2
// @Tags feedback
// @Produce json
// @Param id path string true "Camera ID"
// @Success 200 {object} ReportResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Router /api/v1/cameras/{id}/report-flood [post]
// @Router /api/cameras/{id}/report-flood [post]
func (h *Handler) ReportFlood(c *gin.Context) {
	cameraID := strings.TrimSpace(c.Param("id"))
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_camera_id",
			Message: "Camera ID is required",
		})
		return
	}

	if h.checker != nil && !h.checker.CameraExists(cameraID) {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "camera_not_found",
			Message: fmt.Sprintf("Camera '%s' not found", cameraID),
		})
		return
	}

	clientIP := c.ClientIP()
	if clientIP == "" {
		clientIP = "unknown"
	}

	allowed, retryAfter := h.limiter.Allow(clientIP, cameraID)
	if !allowed {
		retrySeconds := int(math.Ceil(retryAfter.Seconds()))
		if retrySeconds < 1 {
			retrySeconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(retrySeconds))
		c.JSON(http.StatusTooManyRequests, ErrorResponse{
			Error:             "rate_limited",
			Message:           "Too many reports for this camera. Please try again later.",
			RetryAfterSeconds: retrySeconds,
		})
		return
	}

	ts := time.Now().In(h.loc)
	userAgent := c.GetHeader("User-Agent")

	if h.archiver != nil {
		h.archiver.ArchiveAsync(cameraID, clientIP, userAgent, ts)
	}

	c.JSON(http.StatusOK, ReportResponse{
		Status:    "success",
		Message:   "Ghi nhận phản ánh ngập thành công",
		CameraID:  cameraID,
		Timestamp: ts,
	})
}
