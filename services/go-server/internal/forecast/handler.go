package forecast

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler serves HTTP requests for the forecast module.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler backed by the provided forecast Service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers the forecast endpoints onto the given RouterGroup (e.g. /api).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/forecast", h.GetForecast)
}

// RegisterRootRoutes registers alias routes directly onto the root Gin engine (e.g. GET /forecast).
func (h *Handler) RegisterRootRoutes(r *gin.Engine) {
	r.GET("/forecast", h.GetForecast)
}

// GetForecast godoc
// @Summary Short-horizon congestion forecast
// @Description Returns short-horizon congestion forecasts (10, 20, 30 min) for a camera segment using Ridge delta regression or baseline models.
// @Tags Forecast, Traffic
// @Produce json
// @Param camera_id query string false "Camera ID (or segment_id, node_id, edge_id)"
// @Param segment_id query string false "Alias for camera_id"
// @Param node_id query string false "Topology node alias"
// @Param edge_id query string false "Topology edge alias"
// @Param horizon query int false "Forecast horizon minutes (10, 20, 30; default 10)"
// @Param horizon_minutes query int false "Alias for horizon"
// @Param timestamp query string false "Base timestamp in RFC3339 format (default current UTC)"
// @Param model query string false "Model selector (ridge_delta, persistence, historical_median; default ridge_delta)"
// @Success 200 {object} forecast.Response
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/forecast [get]
func (h *Handler) GetForecast(c *gin.Context) {
	// Priority order: camera_id > segment_id > node_id > edge_id
	cameraID := c.Query("camera_id")
	if cameraID == "" {
		cameraID = c.Query("segment_id")
	}
	if cameraID == "" {
		cameraID = c.Query("node_id")
	}
	if cameraID == "" {
		cameraID = c.Query("edge_id")
	}

	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_parameter",
			"message": "camera_id is required",
		})
		return
	}

	// Horizon parameter (with horizon_minutes alias)
	horizonStr := c.Query("horizon")
	if horizonStr == "" {
		horizonStr = c.Query("horizon_minutes")
	}

	horizon := 10
	if horizonStr != "" {
		parsed, err := strconv.Atoi(horizonStr)
		if err != nil || !IsSupportedHorizon(parsed) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_parameter",
				"message": "horizon must be 10, 20, or 30 minutes",
			})
			return
		}
		horizon = parsed
	}

	// Base timestamp parameter
	var baseTime time.Time
	tsStr := c.Query("timestamp")
	if tsStr != "" {
		parsedTime, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			parsedTime, err = time.Parse(time.RFC3339, tsStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "invalid_parameter",
					"message": "invalid timestamp format, expected RFC3339",
				})
				return
			}
		}
		baseTime = parsedTime.UTC()
	} else {
		baseTime = time.Now().UTC()
	}

	// Model parameter
	model := c.DefaultQuery("model", ModelRidgeDelta)
	if model != ModelRidgeDelta && model != ModelPersistence && model != ModelHistoricalMedian {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_parameter",
			"message": "model must be ridge_delta, persistence, or historical_median",
		})
		return
	}

	req := Request{
		CameraID:       cameraID,
		HorizonMinutes: horizon,
		BaseTime:       baseTime,
		Model:          model,
	}

	resp, err := h.service.Predict(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCameraID):
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_parameter",
				"message": "camera_id is required",
			})
		case errors.Is(err, ErrInvalidHorizon):
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_parameter",
				"message": "horizon must be 10, 20, or 30 minutes",
			})
		case errors.Is(err, ErrInvalidModel):
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_parameter",
				"message": "model must be ridge_delta, persistence, or historical_median",
			})
		case errors.Is(err, ErrCameraNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": fmt.Sprintf("camera %s not found in catalog", cameraID),
			})
		case errors.Is(err, ErrInsufficientHistory):
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "insufficient_history",
				"message": "no observation history available for camera",
			})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "service_unavailable",
				"message": "forecast repository database is currently unreachable",
			})
		}
		return
	}

	c.JSON(http.StatusOK, resp)
}
