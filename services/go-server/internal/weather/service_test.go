package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sampleOpenMeteoJSON = `{
  "latitude": 10.7769,
  "longitude": 106.7009,
  "current": {
    "time": "2026-10-02T15:00",
    "interval": 900,
    "temperature_2m": 29.5,
    "relative_humidity_2m": 78,
    "apparent_temperature": 34.2,
    "precipitation": 0.5,
    "rain": 0.5,
    "weather_code": 80
  },
  "hourly": {
    "time": ["2026-10-02T15:00", "2026-10-02T16:00", "2026-10-02T17:00"],
    "precipitation_probability": [65, 40, 20],
    "rain": [0.5, 0.2, 0.0],
    "weather_code": [80, 61, 2]
  }
}`

const sampleRainViewerJSON = `{
  "version": "2.0",
  "generated": 1790929522,
  "host": "https://tilecache.rainviewer.com",
  "radar": {
    "past": [
      {"time": 1790928000, "path": "/v2/radar/frame1"},
      {"time": 1790928600, "path": "/v2/radar/frame2"}
    ],
    "nowcast": [
      {"time": 1790929200, "path": "/v2/radar/nowcast1"}
    ]
  }
}`

func TestWMOToDescription(t *testing.T) {
	tests := []struct {
		code       int
		expectedVi string
		expectedEn string
	}{
		{0, "Trời quang", "Clear sky"},
		{1, "Trời ít mây", "Mainly clear"},
		{2, "Mây rải rác", "Partly cloudy"},
		{3, "Trời nhiều mây", "Overcast"},
		{45, "Sương mù", "Fog"},
		{51, "Mưa phùn nhẹ", "Light drizzle"},
		{61, "Mưa nhỏ", "Slight rain"},
		{65, "Mưa to", "Heavy rain"},
		{80, "Mưa rào nhẹ", "Slight rain shower"},
		{95, "Giông sét", "Thunderstorm"},
		{999, "Không xác định", "Unknown"},
	}

	for _, tt := range tests {
		vi, en := WMOToDescription(tt.code)
		if vi != tt.expectedVi || en != tt.expectedEn {
			t.Errorf("WMO %d: expected (%q, %q), got (%q, %q)", tt.code, tt.expectedVi, tt.expectedEn, vi, en)
		}
	}
}

func TestService_GetWeather(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sampleOpenMeteoJSON)
	}))
	defer server.Close()

	svc := NewService(
		WithOpenMeteoURL(server.URL),
		WithWeatherTTL(500*time.Millisecond),
	)

	ctx := context.Background()

	// 1. Initial fetch
	data, err := svc.GetWeather(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.Temperature != 29.5 {
		t.Errorf("expected temp 29.5, got %f", data.Temperature)
	}
	if data.WeatherCode != 80 {
		t.Errorf("expected weather code 80, got %d", data.WeatherCode)
	}
	if data.WeatherDescVi != "Mưa rào nhẹ" {
		t.Errorf("expected desc 'Mưa rào nhẹ', got %q", data.WeatherDescVi)
	}
	if data.PrecipitationProbability != 65 {
		t.Errorf("expected precipitation prob 65, got %d", data.PrecipitationProbability)
	}
	if len(data.Hourly) != 3 {
		t.Errorf("expected 3 hourly items, got %d", len(data.Hourly))
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 upstream call, got %d", callCount.Load())
	}

	// 2. Cached fetch (TTL active)
	cachedData, err := svc.GetWeather(ctx)
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if cachedData.Temperature != 29.5 {
		t.Errorf("expected cached temp 29.5, got %f", cachedData.Temperature)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected callCount to remain 1 due to cache, got %d", callCount.Load())
	}
}

func TestService_GetWeather_FallbackStale(t *testing.T) {
	var shouldFail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, sampleOpenMeteoJSON)
	}))
	defer server.Close()

	svc := NewService(
		WithOpenMeteoURL(server.URL),
		WithWeatherTTL(50*time.Millisecond),
	)

	ctx := context.Background()

	// Initial success
	data, err := svc.GetWeather(ctx)
	if err != nil || data == nil {
		t.Fatalf("failed initial fetch: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Now fail the server
	shouldFail.Store(true)

	// Next fetch should fallback to stale cached data gracefully
	staleData, err := svc.GetWeather(ctx)
	if err != nil {
		t.Fatalf("expected graceful fallback to stale cache, got err: %v", err)
	}
	if staleData == nil || staleData.Temperature != 29.5 {
		t.Errorf("expected stale data with temp 29.5, got %v", staleData)
	}
}

func TestTruncateToHour(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-10-06T14:15", "2026-10-06T14:00"},
		{"2026-10-06T14:00", "2026-10-06T14:00"},
		{"2026-10-06T14:59", "2026-10-06T14:00"},
		{"2026-10-06T00:30", "2026-10-06T00:00"},
		{"2026-10-06T14:45:00", "2026-10-06T14:00"},
		{"2026-10-06T14:30:00+07:00", "2026-10-06T14:00"},
	}

	for _, tt := range tests {
		got := truncateToHour(tt.input)
		if got != tt.expected {
			t.Errorf("truncateToHour(%q): expected %q, got %q", tt.input, tt.expected, got)
		}
	}
}

func TestService_GetWeather_NonZeroMinuteAlignment(t *testing.T) {
	// Simulate realistic Open-Meteo response where current.time is 14:15,
	// and hourly.time is a 48h array starting at midnight (00:00).
	var times []string
	var probs []int
	var rains []float64
	var codes []int

	for day := 6; day <= 7; day++ {
		for hour := 0; hour < 24; hour++ {
			times = append(times, fmt.Sprintf("2026-10-%02dT%02d:00", day, hour))
			probs = append(probs, (hour*3)%100)
			rains = append(rains, float64(hour)*0.1)
			codes = append(codes, 61)
		}
	}

	type omHourly struct {
		Time                     []string  `json:"time"`
		PrecipitationProbability []int     `json:"precipitation_probability"`
		Rain                     []float64 `json:"rain"`
		WeatherCode              []int     `json:"weather_code"`
	}
	type omCurrent struct {
		Time                string  `json:"time"`
		Interval            int     `json:"interval"`
		Temperature2m       float64 `json:"temperature_2m"`
		RelativeHumidity2m  int     `json:"relative_humidity_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		Precipitation       float64 `json:"precipitation"`
		Rain                float64 `json:"rain"`
		WeatherCode         int     `json:"weather_code"`
	}
	type omPayload struct {
		Latitude  float64   `json:"latitude"`
		Longitude float64   `json:"longitude"`
		Current   omCurrent `json:"current"`
		Hourly    omHourly  `json:"hourly"`
	}

	payload := omPayload{
		Latitude:  10.7909,
		Longitude: 106.7109,
		Current: omCurrent{
			Time:                "2026-10-06T14:15",
			Interval:            900,
			Temperature2m:       28.0,
			RelativeHumidity2m:  80,
			ApparentTemperature: 33.0,
			Precipitation:       1.2,
			Rain:                1.2,
			WeatherCode:         95,
		},
		Hourly: omHourly{
			Time:                     times,
			PrecipitationProbability: probs,
			Rain:                     rains,
			WeatherCode:              codes,
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "forecast_days=2") {
			t.Errorf("expected forecast_days=2 in query, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payloadBytes)
	}))
	defer server.Close()

	svc := NewService(
		WithOpenMeteoURL(server.URL),
		WithWeatherTTL(500*time.Millisecond),
	)

	ctx := context.Background()
	data, err := svc.GetWeather(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Current probability should match the 14:00 slot (42%), NOT 00:00 slot (0%)
	if data.PrecipitationProbability != 42 {
		t.Errorf("expected current rain prob 42%% from 14:00 slot, got %d%%", data.PrecipitationProbability)
	}

	// 2. Hourly forecast must start at current hour slot (14:00), NOT midnight (00:00)
	if len(data.Hourly) != 12 {
		t.Fatalf("expected 12 hourly items, got %d", len(data.Hourly))
	}
	if data.Hourly[0].Time != "2026-10-06T14:00" {
		t.Errorf("expected first hourly slot to be '2026-10-06T14:00', got %q", data.Hourly[0].Time)
	}
	if data.Hourly[0].PrecipitationProbability != 42 {
		t.Errorf("expected first hourly prob 42%%, got %d%%", data.Hourly[0].PrecipitationProbability)
	}

	// 3. 12th hour should reach into tomorrow: 14:00 + 11h = 01:00 next day
	if data.Hourly[11].Time != "2026-10-07T01:00" {
		t.Errorf("expected 12th hourly slot to be '2026-10-07T01:00', got %q", data.Hourly[11].Time)
	}
}

func TestService_GetWeather_LateNightMidnightSpan(t *testing.T) {
	// Verify that querying late at night (e.g. 23:45) returns a full 12-hour slice
	// spanning seamlessly from 23:00 today into 10:00 tomorrow morning.
	var times []string
	var probs []int

	for day := 6; day <= 7; day++ {
		for hour := 0; hour < 24; hour++ {
			times = append(times, fmt.Sprintf("2026-10-%02dT%02d:00", day, hour))
			probs = append(probs, hour*2)
		}
	}

	payload := map[string]any{
		"latitude":  10.7909,
		"longitude": 106.7109,
		"current": map[string]any{
			"time":         "2026-10-06T23:45",
			"weather_code": 80,
		},
		"hourly": map[string]any{
			"time":                      times,
			"precipitation_probability": probs,
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payloadBytes)
	}))
	defer server.Close()

	svc := NewService(WithOpenMeteoURL(server.URL))
	data, err := svc.GetWeather(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(data.Hourly) != 12 {
		t.Fatalf("expected 12 hourly items across midnight, got %d", len(data.Hourly))
	}
	if data.Hourly[0].Time != "2026-10-06T23:00" {
		t.Errorf("expected start at 23:00, got %q", data.Hourly[0].Time)
	}
	if data.Hourly[1].Time != "2026-10-07T00:00" {
		t.Errorf("expected 2nd hour at next-day midnight, got %q", data.Hourly[1].Time)
	}
	if data.Hourly[11].Time != "2026-10-07T10:00" {
		t.Errorf("expected 12th hour at 10:00 tomorrow, got %q", data.Hourly[11].Time)
	}
}

func TestService_GetWeather_MissingHourFallback(t *testing.T) {
	// If exact current hour slot is missing from hourly data,
	// it should fallback gracefully to the closest preceding slot.
	payload := map[string]any{
		"latitude":  10.7909,
		"longitude": 106.7109,
		"current": map[string]any{
			"time":         "2026-10-06T14:30",
			"weather_code": 80,
		},
		"hourly": map[string]any{
			"time":                      []string{"2026-10-06T12:00", "2026-10-06T13:00", "2026-10-06T16:00"},
			"precipitation_probability": []int{10, 20, 30},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payloadBytes)
	}))
	defer server.Close()

	svc := NewService(WithOpenMeteoURL(server.URL))
	data, err := svc.GetWeather(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Closest preceding hour slot to 14:00 is 13:00 (prob = 20)
	if data.PrecipitationProbability != 20 {
		t.Errorf("expected fallback prob 20 from 13:00, got %d", data.PrecipitationProbability)
	}
	if data.Hourly[0].Time != "2026-10-06T13:00" {
		t.Errorf("expected fallback start at 13:00, got %q", data.Hourly[0].Time)
	}
}

func TestService_GetRadar(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sampleRainViewerJSON)
	}))
	defer server.Close()

	svc := NewService(
		WithRainViewerURL(server.URL),
		WithRadarTTL(500*time.Millisecond),
	)

	ctx := context.Background()

	// 1. Initial fetch
	radar, err := svc.GetRadar(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if radar.Host != "https://tilecache.rainviewer.com" {
		t.Errorf("expected host 'https://tilecache.rainviewer.com', got %q", radar.Host)
	}
	if len(radar.PastFrames) != 2 {
		t.Errorf("expected 2 past frames, got %d", len(radar.PastFrames))
	}
	expectedTile := "https://tilecache.rainviewer.com/v2/radar/frame2/256/{z}/{x}/{y}/2/1_1.png"
	if radar.LatestTileTemplate != expectedTile {
		t.Errorf("expected tile template %q, got %q", expectedTile, radar.LatestTileTemplate)
	}

	// 2. Cached fetch
	_, _ = svc.GetRadar(ctx)
	if callCount.Load() != 1 {
		t.Errorf("expected radar cache to prevent second call, got %d calls", callCount.Load())
	}
}
