package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	DefaultHcmcLat       = 10.7769
	DefaultHcmcLon       = 106.7009
	DefaultOpenMeteoURL  = "https://api.open-meteo.com/v1/forecast"
	DefaultRainViewerURL = "https://api.rainviewer.com/public/weather-maps.json"
	DefaultWeatherTTL    = 10 * time.Minute
	DefaultRadarTTL      = 5 * time.Minute
	DefaultHTTPTimeout   = 5 * time.Second
)

// Option configures Service options.
type Option func(*Service)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		if client != nil {
			s.httpClient = client
		}
	}
}

// WithOpenMeteoURL overrides the Open-Meteo API endpoint.
func WithOpenMeteoURL(url string) Option {
	return func(s *Service) {
		if url != "" {
			s.openMeteoURL = url
		}
	}
}

// WithRainViewerURL overrides the RainViewer API endpoint.
func WithRainViewerURL(url string) Option {
	return func(s *Service) {
		if url != "" {
			s.rainViewerURL = url
		}
	}
}

// WithWeatherTTL sets the cache duration for weather forecasts.
func WithWeatherTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.weatherTTL = ttl
		}
	}
}

// WithRadarTTL sets the cache duration for radar frames.
func WithRadarTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.radarTTL = ttl
		}
	}
}

// WithCoordinates sets the target geographic coordinates.
func WithCoordinates(lat, lon float64) Option {
	return func(s *Service) {
		s.lat = lat
		s.lon = lon
	}
}

// Service manages weather forecasts and radar tile metadata with in-memory caching.
type Service struct {
	httpClient    *http.Client
	openMeteoURL  string
	rainViewerURL string
	lat           float64
	lon           float64
	weatherTTL    time.Duration
	radarTTL      time.Duration

	mu              sync.RWMutex
	cachedWeather   *WeatherData
	weatherCachedAt time.Time
	cachedRadar     *RadarMetadata
	radarCachedAt   time.Time
}

// NewService constructs a new weather Service with defaults and functional options.
func NewService(opts ...Option) *Service {
	s := &Service{
		httpClient: &http.Client{
			Timeout: DefaultHTTPTimeout,
		},
		openMeteoURL:  DefaultOpenMeteoURL,
		rainViewerURL: DefaultRainViewerURL,
		lat:           DefaultHcmcLat,
		lon:           DefaultHcmcLon,
		weatherTTL:    DefaultWeatherTTL,
		radarTTL:      DefaultRadarTTL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type openMeteoResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Current   struct {
		Time                string  `json:"time"`
		Temperature         float64 `json:"temperature_2m"`
		RelativeHumidity    int     `json:"relative_humidity_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		Precipitation       float64 `json:"precipitation"`
		Rain                float64 `json:"rain"`
		WeatherCode         int     `json:"weather_code"`
	} `json:"current"`
	Hourly struct {
		Time                     []string  `json:"time"`
		PrecipitationProbability []int     `json:"precipitation_probability"`
		Rain                     []float64 `json:"rain"`
		WeatherCode              []int     `json:"weather_code"`
	} `json:"hourly"`
}

type rainViewerResponse struct {
	Version   string `json:"version"`
	Generated int64  `json:"generated"`
	Host      string `json:"host"`
	Radar     struct {
		Past []struct {
			Time int64  `json:"time"`
			Path string `json:"path"`
		} `json:"past"`
		Nowcast []struct {
			Time int64  `json:"time"`
			Path string `json:"path"`
		} `json:"nowcast"`
	} `json:"radar"`
}

// GetWeather retrieves the latest weather forecast for HCMC, utilizing in-memory cache.
// If upstream fails and cached data exists, returns stale cache gracefully per DOD.
func (s *Service) GetWeather(ctx context.Context) (*WeatherData, error) {
	s.mu.RLock()
	if s.cachedWeather != nil && time.Since(s.weatherCachedAt) < s.weatherTTL {
		cached := *s.cachedWeather
		s.mu.RUnlock()
		return &cached, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check cache under write lock
	if s.cachedWeather != nil && time.Since(s.weatherCachedAt) < s.weatherTTL {
		cached := *s.cachedWeather
		return &cached, nil
	}

	reqURL := fmt.Sprintf("%s?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,apparent_temperature,precipitation,rain,weather_code&hourly=precipitation_probability,precipitation,rain,weather_code&timezone=Asia%%2FHo_Chi_Minh&forecast_days=1",
		s.openMeteoURL, s.lat, s.lon)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		if s.cachedWeather != nil {
			cached := *s.cachedWeather
			return &cached, nil
		}
		return nil, fmt.Errorf("weather: create request: %w", err)
	}
	req.Header.Set("User-Agent", "projectKV-go-server/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if s.cachedWeather != nil {
			cached := *s.cachedWeather
			return &cached, nil
		}
		return nil, fmt.Errorf("weather: fetch open-meteo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if s.cachedWeather != nil {
			cached := *s.cachedWeather
			return &cached, nil
		}
		return nil, fmt.Errorf("weather: open-meteo unexpected status: %d", resp.StatusCode)
	}

	var omResp openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&omResp); err != nil {
		if s.cachedWeather != nil {
			cached := *s.cachedWeather
			return &cached, nil
		}
		return nil, fmt.Errorf("weather: decode open-meteo json: %w", err)
	}

	viDesc, enDesc := WMOToDescription(omResp.Current.WeatherCode)

	// Determine precipitation probability: use matching hourly slot or fallback to current
	prob := 0
	currentTimeStr := omResp.Current.Time
	hourlyList := make([]HourlyForecast, 0, len(omResp.Hourly.Time))

	for i, t := range omResp.Hourly.Time {
		hourProb := 0
		if i < len(omResp.Hourly.PrecipitationProbability) {
			hourProb = omResp.Hourly.PrecipitationProbability[i]
		}
		hourRain := 0.0
		if i < len(omResp.Hourly.Rain) {
			hourRain = omResp.Hourly.Rain[i]
		}
		hourCode := 0
		if i < len(omResp.Hourly.WeatherCode) {
			hourCode = omResp.Hourly.WeatherCode[i]
		}
		hVi, hEn := WMOToDescription(hourCode)

		hourlyList = append(hourlyList, HourlyForecast{
			Time:                     t,
			PrecipitationProbability: hourProb,
			RainMm:                   hourRain,
			WeatherCode:              hourCode,
			WeatherDescVi:            hVi,
			WeatherDescEn:            hEn,
		})

		if t == currentTimeStr {
			prob = hourProb
		}
	}

	// If exact current hour not found, take first hourly probability if available
	if prob == 0 && len(hourlyList) > 0 {
		prob = hourlyList[0].PrecipitationProbability
	}

	// Limit hourly forecast to next 12 hours from current hour
	currentIdx := 0
	for i, h := range hourlyList {
		if h.Time == currentTimeStr {
			currentIdx = i
			break
		}
	}
	endIdx := currentIdx + 12
	if endIdx > len(hourlyList) {
		endIdx = len(hourlyList)
	}
	slicedHourly := hourlyList[currentIdx:endIdx]

	weatherData := &WeatherData{
		Location:                 "Ho Chi Minh City",
		Latitude:                 omResp.Latitude,
		Longitude:                omResp.Longitude,
		Temperature:              omResp.Current.Temperature,
		ApparentTemperature:      omResp.Current.ApparentTemperature,
		Humidity:                 omResp.Current.RelativeHumidity,
		WeatherCode:              omResp.Current.WeatherCode,
		WeatherDescVi:            viDesc,
		WeatherDescEn:            enDesc,
		PrecipitationProbability: prob,
		RainMm:                   omResp.Current.Rain,
		Source:                   "open-meteo",
		Hourly:                   slicedHourly,
		UpdatedAt:                time.Now().UTC(),
	}

	s.cachedWeather = weatherData
	s.weatherCachedAt = time.Now()

	cached := *weatherData
	return &cached, nil
}

// GetRadar retrieves the latest RainViewer radar metadata with in-memory caching.
// If upstream fails and cached data exists, returns stale cache gracefully per DOD.
func (s *Service) GetRadar(ctx context.Context) (*RadarMetadata, error) {
	s.mu.RLock()
	if s.cachedRadar != nil && time.Since(s.radarCachedAt) < s.radarTTL {
		cached := *s.cachedRadar
		s.mu.RUnlock()
		return &cached, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check cache under write lock
	if s.cachedRadar != nil && time.Since(s.radarCachedAt) < s.radarTTL {
		cached := *s.cachedRadar
		return &cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.rainViewerURL, nil)
	if err != nil {
		if s.cachedRadar != nil {
			cached := *s.cachedRadar
			return &cached, nil
		}
		return nil, fmt.Errorf("radar: create request: %w", err)
	}
	req.Header.Set("User-Agent", "projectKV-go-server/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if s.cachedRadar != nil {
			cached := *s.cachedRadar
			return &cached, nil
		}
		return nil, fmt.Errorf("radar: fetch rainviewer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if s.cachedRadar != nil {
			cached := *s.cachedRadar
			return &cached, nil
		}
		return nil, fmt.Errorf("radar: rainviewer unexpected status: %d", resp.StatusCode)
	}

	var rvResp rainViewerResponse
	if err := json.NewDecoder(resp.Body).Decode(&rvResp); err != nil {
		if s.cachedRadar != nil {
			cached := *s.cachedRadar
			return &cached, nil
		}
		return nil, fmt.Errorf("radar: decode rainviewer json: %w", err)
	}

	pastFrames := make([]RadarFrame, 0, len(rvResp.Radar.Past))
	for _, p := range rvResp.Radar.Past {
		pastFrames = append(pastFrames, RadarFrame{
			Time: p.Time,
			Path: p.Path,
		})
	}

	nowcastFrames := make([]RadarFrame, 0, len(rvResp.Radar.Nowcast))
	for _, n := range rvResp.Radar.Nowcast {
		nowcastFrames = append(nowcastFrames, RadarFrame{
			Time: n.Time,
			Path: n.Path,
		})
	}

	latestPath := ""
	if len(pastFrames) > 0 {
		latestPath = pastFrames[len(pastFrames)-1].Path
	} else if len(nowcastFrames) > 0 {
		latestPath = nowcastFrames[0].Path
	}

	latestTile := ""
	if latestPath != "" && rvResp.Host != "" {
		latestTile = fmt.Sprintf("%s%s/256/{z}/{x}/{y}/2/1_1.png", rvResp.Host, latestPath)
	}

	radarData := &RadarMetadata{
		Host:               rvResp.Host,
		Generated:          rvResp.Generated,
		PastFrames:         pastFrames,
		NowcastFrames:      nowcastFrames,
		LatestTileTemplate: latestTile,
		Source:             "rainviewer",
		UpdatedAt:          time.Now().UTC(),
	}

	s.cachedRadar = radarData
	s.radarCachedAt = time.Now()

	cached := *radarData
	return &cached, nil
}
