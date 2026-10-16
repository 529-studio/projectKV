package harvester

import (
	"context"
	"io"
	"time"
)

// Scope defines the trigger scope for harvesting.
type Scope string

const (
	ScopeScheduled Scope = "scheduled_peak"
	ScopeRain      Scope = "rain_watcher"
)

// Camera represents the minimal camera details needed by harvester.
type Camera struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	Title       string   `json:"title,omitempty"`
	SnapshotURL string   `json:"snapshotUrl"`
	RoadKeys    []string `json:"road_keys,omitempty"`
}

// Name returns the best available human-readable name for the camera.
func (c Camera) Name() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Title
}

// WeatherInfo represents meteorological status used for rain triggering and label generation.
type WeatherInfo struct {
	RainfallMm  float64
	WeatherCode int
}

// VehicleCounts represents detected vehicle counts broken down by classification.
type VehicleCounts struct {
	Motorcycle int `json:"motorcycle"`
	Car        int `json:"car"`
	Bus        int `json:"bus"`
	Truck      int `json:"truck"`
	Container  int `json:"container"`
}

// Total returns the total vehicle count.
func (v VehicleCounts) Total() int {
	return v.Motorcycle + v.Car + v.Bus + v.Truck + v.Container
}

// PredictionResult represents output from the vision inference engine.
type PredictionResult struct {
	TrafficSeverity  string        `json:"traffic_severity"`
	Confidence       float64       `json:"confidence"`
	VehiclesDetected int           `json:"vehicles_detected"`
	VehicleCounts    VehicleCounts `json:"vehicle_counts,omitempty"`
	SaturationIndex  float64       `json:"saturation_index,omitempty"`
	LatencyMs        float64       `json:"latency_ms,omitempty"`
}

// WeatherProvider defines the consumer interface for retrieving meteorological conditions.
type WeatherProvider interface {
	GetWeather(ctx context.Context) (*WeatherInfo, error)
}

// SnapshotFetcher defines the consumer interface for fetching snapshot bytes.
type SnapshotFetcher interface {
	FetchSnapshot(ctx context.Context, cam Camera) ([]byte, error)
}

// VisionPredictor defines the consumer interface for local inference.
type VisionPredictor interface {
	Predict(ctx context.Context, cam Camera, frame []byte) (*PredictionResult, error)
}

// StorageUploader defines the consumer interface for uploading files to object storage (Cloudflare R2).
type StorageUploader interface {
	Put(ctx context.Context, path string, data io.Reader) error
}

// Config defines the configuration parameters for the harvester subsystem.
type Config struct {
	Enabled             bool
	ScheduleHours       string        // e.g. "07:30,12:00,17:30" or "7,12,17"
	RainCooldownMinutes int           // e.g. 60
	Concurrency         int           // e.g. 15
	WatcherInterval     time.Duration // default: 5m (can be set lower in tests)
	TimeLocation        *time.Location
}

// Dependencies contains the injected implementations for the harvester.
type Dependencies struct {
	Weather       WeatherProvider
	Fetcher       SnapshotFetcher
	Predictor     VisionPredictor
	Storage       StorageUploader
	HotsetCameras []Camera
	RainCameras   []Camera
}
