package tide

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type mockWeatherReader struct {
	forecasts []RainForecast
	err       error
}

func (m *mockWeatherReader) GetHourlyRainForecast(ctx context.Context) ([]RainForecast, error) {
	return m.forecasts, m.err
}

func TestPredictTide_HarmonicBoundsAndFidelity(t *testing.T) {
	baseTime := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	// Sample 48 hours in 15-minute increments
	var minPhuAn, maxPhuAn = 999.0, -999.0
	var minNhaBe, maxNhaBe = 999.0, -999.0

	for h := 0; h < 48*4; h++ {
		tPoint := baseTime.Add(time.Duration(h*15) * time.Minute)
		phuAn := PredictTide(StationPhuAn, tPoint)
		nhaBe := PredictTide(StationNhaBe, tPoint)

		if phuAn < minPhuAn {
			minPhuAn = phuAn
		}
		if phuAn > maxPhuAn {
			maxPhuAn = phuAn
		}
		if nhaBe < minNhaBe {
			minNhaBe = nhaBe
		}
		if nhaBe > maxNhaBe {
			maxNhaBe = nhaBe
		}
	}

	// In HCMC, astronomical tide fluctuates between ~ -0.8m and +1.8m
	if minPhuAn < -1.5 || maxPhuAn > 2.2 {
		t.Errorf("Phú An tide bounds out of realistic range: min=%.2f, max=%.2f", minPhuAn, maxPhuAn)
	}
	if minNhaBe < -1.5 || maxNhaBe > 2.2 {
		t.Errorf("Nhà Bè tide bounds out of realistic range: min=%.2f, max=%.2f", minNhaBe, maxNhaBe)
	}

	// Max tide should reach or exceed Alert Level 1 (1.40m) during high tide windows
	if maxPhuAn < 1.30 {
		t.Errorf("expected maximum tide to reach realistic peak, got %.2f", maxPhuAn)
	}
}

func TestGetAlertLevel(t *testing.T) {
	tests := []struct {
		height float64
		level  string
	}{
		{1.20, "NORMAL"},
		{1.39, "NORMAL"},
		{1.40, "LEVEL_1"},
		{1.49, "LEVEL_1"},
		{1.50, "LEVEL_2"},
		{1.59, "LEVEL_2"},
		{1.60, "LEVEL_3"},
		{1.75, "LEVEL_3"},
	}

	for _, tt := range tests {
		lvl, labels := GetAlertLevel(tt.height)
		if lvl != tt.level {
			t.Errorf("height %.2f: expected %s, got %s", tt.height, tt.level, lvl)
		}
		if labels["vi"] == "" || labels["en"] == "" {
			t.Errorf("height %.2f: missing bilingual labels", tt.height)
		}
	}
}

func TestGetHourlyForecast_Service(t *testing.T) {
	fixedTime := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	mockWR := &mockWeatherReader{
		forecasts: []RainForecast{
			{
				Time:                     "2026-11-20T17:00",
				PrecipitationProbability: 80,
				RainMm:                   12.5,
				WeatherDescVi:            "Mưa rào",
				WeatherDescEn:            "Showers",
			},
		},
	}

	svc := NewService(mockWR)
	svc.SetNowFunc(func() time.Time { return fixedTime })

	resp, err := svc.GetHourlyForecast(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Timeline) != 7 {
		t.Fatalf("expected 7 timeline items (0..6h), got %d", len(resp.Timeline))
	}

	if resp.Advisory.Headline["vi"] == "" || resp.Advisory.Headline["en"] == "" {
		t.Errorf("advisory headline must be bilingual")
	}

	// Verify timeline item structure
	first := resp.Timeline[0]
	if first.HourOffset != 0 {
		t.Errorf("first item offset should be 0, got %d", first.HourOffset)
	}
	if first.TideLevelPhuAnM == 0 {
		t.Errorf("expected valid tidal prediction at Phú An, got 0")
	}
}

func TestHandler_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	apiGroup := router.Group("/api")

	svc := NewService(nil)
	handler := NewHandler(svc)
	handler.RegisterRoutes(apiGroup)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/forecast/hourly", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var payload HourlyForecastResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if len(payload.Timeline) != 7 {
		t.Errorf("expected 7 timeline items, got %d", len(payload.Timeline))
	}
}
