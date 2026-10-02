package weather

import "time"

// WeatherData represents current meteorological metrics and hourly rain probability for HCMC.
type WeatherData struct {
	Location                 string           `json:"location"`
	Latitude                 float64          `json:"latitude"`
	Longitude                float64          `json:"longitude"`
	Temperature              float64          `json:"temperature_c"`
	ApparentTemperature      float64          `json:"apparent_temperature_c"`
	Humidity                 int              `json:"humidity_percent"`
	WeatherCode              int              `json:"weather_code"`
	WeatherDescVi            string           `json:"weather_desc_vi"`
	WeatherDescEn            string           `json:"weather_desc_en"`
	PrecipitationProbability int              `json:"precipitation_probability_percent"`
	RainMm                   float64          `json:"rain_mm"`
	Source                   string           `json:"source"`
	Hourly                   []HourlyForecast `json:"hourly_forecast"`
	UpdatedAt                time.Time        `json:"updated_at"`
}

// HourlyForecast holds hourly precipitation probability and rain volume forecast.
type HourlyForecast struct {
	Time                     string  `json:"time"`
	PrecipitationProbability int     `json:"precipitation_probability_percent"`
	RainMm                   float64 `json:"rain_mm"`
	WeatherCode              int     `json:"weather_code"`
	WeatherDescVi            string  `json:"weather_desc_vi"`
	WeatherDescEn            string  `json:"weather_desc_en"`
}

// RadarFrame represents a single timestamped radar observation frame from RainViewer.
type RadarFrame struct {
	Time int64  `json:"time"`
	Path string `json:"path"`
}

// RadarMetadata represents RainViewer radar tile metadata and latest layer paths.
type RadarMetadata struct {
	Host               string       `json:"host"`
	Generated          int64        `json:"generated"`
	PastFrames         []RadarFrame `json:"past_frames"`
	NowcastFrames      []RadarFrame `json:"nowcast_frames"`
	LatestTileTemplate string       `json:"latest_tile_template"`
	Source             string       `json:"source"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

// WMOToDescription translates standard WMO weather codes (0-99) into Vietnamese and English.
// Vietnamese descriptions strictly follow proper diacritics per AGENTS.md rules.
func WMOToDescription(code int) (vi string, en string) {
	switch code {
	case 0:
		return "Trời quang", "Clear sky"
	case 1:
		return "Trời ít mây", "Mainly clear"
	case 2:
		return "Mây rải rác", "Partly cloudy"
	case 3:
		return "Trời nhiều mây", "Overcast"
	case 45, 48:
		return "Sương mù", "Fog"
	case 51:
		return "Mưa phùn nhẹ", "Light drizzle"
	case 53:
		return "Mưa phùn vừa", "Moderate drizzle"
	case 55:
		return "Mưa phùn dày hạt", "Dense drizzle"
	case 56, 57:
		return "Mưa phùn buốt giá", "Freezing drizzle"
	case 61:
		return "Mưa nhỏ", "Slight rain"
	case 63:
		return "Mưa vừa", "Moderate rain"
	case 65:
		return "Mưa to", "Heavy rain"
	case 66, 67:
		return "Mưa buốt giá", "Freezing rain"
	case 71, 73, 75, 77:
		return "Tuyết rơi", "Snow"
	case 80:
		return "Mưa rào nhẹ", "Slight rain shower"
	case 81:
		return "Mưa rào vừa", "Moderate rain shower"
	case 82:
		return "Mưa rào rất to", "Violent rain shower"
	case 85, 86:
		return "Mưa tuyết", "Snow showers"
	case 95:
		return "Giông sét", "Thunderstorm"
	case 96, 99:
		return "Giông sét kèm mưa đá", "Thunderstorm with hail"
	default:
		return "Không xác định", "Unknown"
	}
}
