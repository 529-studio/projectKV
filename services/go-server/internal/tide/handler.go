package tide

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler serves HTTP endpoints for tidal predictions and hourly commuter risk forecasts.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler with the given Service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts the hourly forecast endpoint onto rg (e.g. /api).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/forecast/hourly", h.GetHourlyForecast)
}

// GetHourlyForecast godoc
// @Summary Combined hourly tidal surge and convective rain forecast
// @Description Returns 6-hour timeline combining astronomical tide predictions (Phú An and Nhà Bè stations) with rain volume and probability, highlighting affected low-lying roads and commuter advisories.
// @Tags Forecast, Tide
// @Produce json
// @Success 200 {object} tide.HourlyForecastResponse
// @Failure 500 {object} map[string]string "Internal error generating forecast"
// @Router /api/forecast/hourly [get]
func (h *Handler) GetHourlyForecast(c *gin.Context) {
	resp, err := h.service.GetHourlyForecast(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate hourly forecast"})
		return
	}
	c.JSON(http.StatusOK, resp)
}
