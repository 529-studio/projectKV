package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type mockPersister struct {
	mu      sync.Mutex
	records []ObservationRecord
	err     error
}

func (m *mockPersister) RecordObservation(ctx context.Context, obs ObservationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.records = append(m.records, obs)
	return nil
}

func (m *mockPersister) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func TestSampler_SampleOnce_Success(t *testing.T) {
	// Mock Open-Meteo
	omServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openMeteoResponse{
			Latitude:  10.7769,
			Longitude: 106.7009,
		}
		resp.Current.Time = "2026-11-06T10:00"
		resp.Current.Temperature = 29.5
		resp.Current.RelativeHumidity = 78
		resp.Current.Rain = 1.5
		resp.Current.WeatherCode = 61
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer omServer.Close()

	// Mock RainViewer
	rvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rainViewerResponse{
			Host:      "https://tilecache.rainviewer.com",
			Generated: time.Now().Unix(),
		}
		resp.Radar.Past = []struct {
			Time int64  `json:"time"`
			Path string `json:"path"`
		}{
			{Time: time.Now().Unix() - 180, Path: "/v2/radar/123/256/{z}/{x}/{y}/2/1_1.png"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer rvServer.Close()

	service := NewService(
		WithOpenMeteoURL(omServer.URL),
		WithRainViewerURL(rvServer.URL),
	)

	persister := &mockPersister{}
	sampler := NewSampler(service, persister, nil)

	ctx := context.Background()
	record, err := sampler.SampleOnce(ctx)
	if err != nil {
		t.Fatalf("SampleOnce failed: %v", err)
	}

	if record.PrecipitationMm != 1.5 {
		t.Errorf("expected RainMm 1.5, got %f", record.PrecipitationMm)
	}
	if record.WeatherCode != 61 {
		t.Errorf("expected WeatherCode 61, got %d", record.WeatherCode)
	}
	if record.TemperatureC != 29.5 {
		t.Errorf("expected Temp 29.5, got %f", record.TemperatureC)
	}
	if record.HumidityPct != 78.0 {
		t.Errorf("expected Humidity 78.0, got %f", record.HumidityPct)
	}
	if record.RadarActive != 1 {
		t.Errorf("expected RadarActive 1, got %d", record.RadarActive)
	}
	if record.RadarFrameAgeS < 0 {
		t.Errorf("unexpected negative RadarFrameAgeS: %d", record.RadarFrameAgeS)
	}
	if record.PayloadHash == "" {
		t.Errorf("expected non-empty PayloadHash")
	}

	if persister.count() != 1 {
		t.Errorf("expected 1 record in persister, got %d", persister.count())
	}
}

func TestSampler_Start_Ticker(t *testing.T) {
	omServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openMeteoResponse{}
		resp.Current.Temperature = 25.0
		resp.Current.WeatherCode = 0
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer omServer.Close()

	rvServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rainViewerResponse{})
	}))
	defer rvServer.Close()

	service := NewService(
		WithOpenMeteoURL(omServer.URL),
		WithRainViewerURL(rvServer.URL),
		WithWeatherTTL(10*time.Millisecond),
	)

	persister := &mockPersister{}
	sampler := NewSampler(service, persister, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run with short interval for testing
	go sampler.Start(ctx, 30*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	cancel()

	time.Sleep(20 * time.Millisecond)
	if persister.count() < 2 {
		t.Errorf("expected at least 2 samples, got %d", persister.count())
	}
}
