package weather

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes live weather forecast and radar tile metadata endpoints.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler with the given weather Service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts weather and radar HTTP endpoints onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/weather", h.GetWeather)
	rg.GET("/weather/radar", h.GetRadar)
}

// GetWeather godoc
// @Summary Get live meteorological weather & rain forecast for Ho Chi Minh City
// @Description Returns current temperature, humidity, WMO weather condition code, hourly rain probability, and precipitation volume from Open-Meteo with in-memory caching.
// @Tags Weather
// @Produce json
// @Success 200 {object} weather.WeatherData
// @Failure 500 {object} map[string]string "Internal server error fetching weather data"
// @Router /api/weather [get]
func (h *Handler) GetWeather(c *gin.Context) {
	data, err := h.service.GetWeather(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "weather service temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, data)
}

// GetRadar godoc
// @Summary Get live RainViewer Doppler radar tile metadata
// @Description Returns the latest radar frame timestamps, host, and tile URL template for rendering live precipitation radar layers in Leaflet.
// @Tags Weather
// @Produce json
// @Success 200 {object} weather.RadarMetadata
// @Failure 500 {object} map[string]string "Internal server error fetching radar metadata"
// @Router /api/weather/radar [get]
func (h *Handler) GetRadar(c *gin.Context) {
	data, err := h.service.GetRadar(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "radar service temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, data)
}
