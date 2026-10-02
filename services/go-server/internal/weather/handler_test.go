package weather

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHandler_Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/public/weather-maps.json" {
			fmt.Fprint(w, sampleRainViewerJSON)
			return
		}
		fmt.Fprint(w, sampleOpenMeteoJSON)
	}))
	defer server.Close()

	svc := NewService(
		WithOpenMeteoURL(server.URL),
		WithRainViewerURL(server.URL+"/public/weather-maps.json"),
		WithWeatherTTL(1*time.Minute),
		WithRadarTTL(1*time.Minute),
	)

	handler := NewHandler(svc)

	r := gin.New()
	apiGroup := r.Group("/api")
	handler.RegisterRoutes(apiGroup)

	// 1. Test GET /api/weather
	t.Run("GET /api/weather", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp WeatherData
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal weather response: %v", err)
		}

		if resp.Temperature != 29.5 {
			t.Errorf("expected temp 29.5, got %f", resp.Temperature)
		}
		if resp.WeatherDescVi != "Mưa rào nhẹ" {
			t.Errorf("expected 'Mưa rào nhẹ', got %q", resp.WeatherDescVi)
		}
		if resp.PrecipitationProbability != 65 {
			t.Errorf("expected prob 65, got %d", resp.PrecipitationProbability)
		}
	})

	// 2. Test GET /api/weather/radar
	t.Run("GET /api/weather/radar", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/weather/radar", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp RadarMetadata
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal radar response: %v", err)
		}

		if resp.Host != "https://tilecache.rainviewer.com" {
			t.Errorf("expected host, got %q", resp.Host)
		}
		if resp.LatestTileTemplate == "" {
			t.Errorf("expected non-empty LatestTileTemplate")
		}
	})

	// 3. Test Error Case (500)
	t.Run("GET /api/weather error handling", func(t *testing.T) {
		errorSvc := NewService(
			WithOpenMeteoURL("http://127.0.0.1:0/invalid"),
			WithWeatherTTL(1*time.Minute),
		)
		errHandler := NewHandler(errorSvc)
		errR := gin.New()
		errHandler.RegisterRoutes(errR.Group("/api"))

		req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
		w := httptest.NewRecorder()
		errR.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 on fetch failure with no cache, got %d", w.Code)
		}
	})
}
