package forecast

import (
	"errors"
	"net/http"
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

// RegisterRoutes registers the canonical forecast endpoint onto the given RouterGroup (e.g. /api).
// Registers GET /v1/cameras/:id/forecast, mounting to /api/v1/cameras/:id/forecast.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/v1/cameras/:id/forecast", h.GetForecast)
}

// GetForecast godoc
// @Summary Short-horizon congestion forecast
// @Description Returns short-horizon congestion forecasts (10, 20, 30 min) for a camera segment using Ridge delta regression or baseline models.
// @Tags Forecast, Traffic
// @Produce json
// @Param id path string true "Camera ID"
// @Param timestamp query string false "Base timestamp in RFC3339 format (default current UTC)"
// @Param model query string false "Model selector (ridge_delta, persistence, historical_median; default ridge_delta)"
// @Success 200 {object} forecast.MultiHorizonResponse
// @Failure 400 {object} forecast.ErrorResponse
// @Failure 404 {object} forecast.ErrorResponse
// @Failure 422 {object} forecast.ErrorResponse
// @Failure 503 {object} forecast.ErrorResponse
// @Router /api/v1/cameras/{id}/forecast [get]
func (h *Handler) GetForecast(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	cameraID := c.Param("id")
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   KindInvalidParameter,
			Message: "camera id path parameter is required",
		})
		return
	}

	// Reject retired horizon query parameters per contract D6
	if c.Query("horizon") != "" || c.Query("horizon_minutes") != "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   KindInvalidParameter,
			Message: "horizon query parameter is retired; endpoint returns all horizons [10, 20, 30]",
		})
		return
	}

	// Base timestamp parameter
	var baseTime time.Time
	tsStr := c.Query("timestamp")
	if tsStr != "" {
		parsedTime, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			parsedTime, err = time.Parse(time.RFC3339, tsStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, ErrorResponse{
					Error:   KindInvalidParameter,
					Message: "invalid timestamp format, expected RFC3339",
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
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   KindInvalidParameter,
			Message: "model must be ridge_delta, persistence, or historical_median",
		})
		return
	}

	req := Request{
		CameraID: cameraID,
		BaseTime: baseTime,
		Model:    model,
	}

	resp, err := h.service.PredictMulti(c.Request.Context(), req)
	if err != nil {
		var fe *ForecastError
		if errors.As(err, &fe) {
			switch fe.Kind {
			case KindInvalidParameter:
				c.JSON(http.StatusBadRequest, ErrorResponse{
					Error:   fe.Kind,
					Message: fe.Message,
				})
			case KindNotFound:
				c.JSON(http.StatusNotFound, ErrorResponse{
					Error:   fe.Kind,
					Message: fe.Message,
				})
			case KindInsufficientHistory:
				c.JSON(http.StatusUnprocessableEntity, ErrorResponse{
					Error:        fe.Kind,
					Message:      fe.Message,
					Availability: StatusInsufficientHistory,
					Requirements: fe.Requirements,
				})
			case KindModelUnvalidated:
				c.JSON(http.StatusServiceUnavailable, ErrorResponse{
					Error:        fe.Kind,
					Message:      fe.Message,
					Availability: StatusModelUnvalidated,
					Model:        fe.Model,
				})
			case KindServiceUnavailable:
				c.JSON(http.StatusServiceUnavailable, ErrorResponse{
					Error:   fe.Kind,
					Message: fe.Message,
				})
			default:
				c.JSON(http.StatusInternalServerError, ErrorResponse{
					Error:   "internal_error",
					Message: fe.Message,
				})
			}
			return
		}

		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "internal_error",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}
