package tide

import "context"

// RainForecast represents an hourly rain forecast item provided by a weather reader.
type RainForecast struct {
	Time                     string  `json:"time"`
	PrecipitationProbability int     `json:"precipitation_probability_percent"`
	RainMm                   float64 `json:"rain_mm"`
	WeatherCode              int     `json:"weather_code"`
	WeatherDescVi            string  `json:"weather_desc_vi"`
	WeatherDescEn            string  `json:"weather_desc_en"`
}

// WeatherReader provides hourly precipitation forecasts for Ho Chi Minh City.
// Consumers define interfaces; producers implement them (Modulith Rule 1).
type WeatherReader interface {
	GetHourlyRainForecast(ctx context.Context) ([]RainForecast, error)
}
