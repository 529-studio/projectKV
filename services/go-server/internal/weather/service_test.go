package weather

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
