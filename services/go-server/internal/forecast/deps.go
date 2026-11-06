package forecast

import (
	"context"
	"time"
)

// AvailabilityStatus indicates whether a forecast payload is ready for client rendering.
type AvailabilityStatus string

const (
	StatusOk                  AvailabilityStatus = "ok"
	StatusInsufficientHistory AvailabilityStatus = "insufficient_history"
	StatusModelUnvalidated    AvailabilityStatus = "model_unvalidated"
	StatusStale               AvailabilityStatus = "stale"

	AvailabilityOK                  = StatusOk
	AvailabilityInsufficientHistory = StatusInsufficientHistory
	AvailabilityModelUnvalidated    = StatusModelUnvalidated
	AvailabilityStale               = StatusStale
)

// FitSource identifies whether the model weights and metrics originate from real traffic history
// or a synthetic prototype fixture.
type FitSource string

const (
	FitSourceLiveHistory      FitSource = "live_history"
	FitSourceSyntheticFixture FitSource = "synthetic_fixture"
)

// Observation represents one historical traffic observation for a camera.
// Kept as a pure value object within forecast to preserve leaf package boundaries.
type Observation struct {
	CameraID        string
	PCUTotal        float64
	SaturationIndex float64
	Severity        string
	Confidence      float64
	Source          string
	ObservedAt      time.Time
}

// HistoryReader defines the query capability required from the history repository
// per Dependency Inversion Principle (DIP).
type HistoryReader interface {
	GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]Observation, error)
	GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]Observation, error)
}

// CameraInfo holds camera metadata needed by the forecast service.
type CameraInfo struct {
	ID           string
	RoadKey      string
	RoadKeys     []string
	BaseCapacity float64
}

// CameraCatalog defines camera existence validation and metadata retrieval.
type CameraCatalog interface {
	GetCamera(id string) (CameraInfo, bool)
}

// CurrentCondition captures the as-of traffic state at base observation time.
type CurrentCondition struct {
	ObservedAt      string  `json:"observed_at"`
	SaturationIndex float64 `json:"saturation_index"`
	Severity        string  `json:"severity"`
	SpeedKmh        float64 `json:"speed_kmh"`
	Confidence      float64 `json:"confidence"`
}

// CurrentState is an alias for CurrentCondition.
type CurrentState = CurrentCondition

// Interval specifies lower and upper bounds of a 95% confidence interval.
type Interval struct {
	Lower95 float64 `json:"lower_95"`
	Upper95 float64 `json:"upper_95"`
}

// ConfidenceIntervals wraps intervals for both saturation index and speed.
type ConfidenceIntervals struct {
	SaturationIndex Interval `json:"saturation_index"`
	SpeedKmh        Interval `json:"speed_kmh"`
}

// HorizonPrediction represents a forecast for a single forward horizon (e.g. 10, 20, 30 min).
type HorizonPrediction struct {
	HorizonMinutes           int                 `json:"horizon_minutes"`
	TargetTime               string              `json:"target_time"`
	PredictedSaturationIndex float64             `json:"predicted_saturation_index"`
	PredictedSeverity        string              `json:"predicted_severity"`
	PredictedSpeedKmh        float64             `json:"predicted_speed_kmh"`
	Confidence               float64             `json:"confidence"`
	ConfidenceIntervals      ConfidenceIntervals `json:"confidence_intervals"`
}

// FitWindow specifies the calendar time window over which training data was collected.
type FitWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// HorizonMetric records validation accuracy metrics for a specific forecast horizon.
type HorizonMetric struct {
	HorizonMinutes int     `json:"horizon_minutes"`
	MAE            float64 `json:"mae"`
	RMSE           float64 `json:"rmse"`
	SampleCount    int     `json:"sample_count"`
}

// ModelMetadata provides provenance, validation state, and accuracy metrics for the serving model.
type ModelMetadata struct {
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	TargetMode     string          `json:"target_mode"`
	FitSource      string          `json:"fit_source"`
	Validated      bool            `json:"validated"`
	Experimental   bool            `json:"experimental,omitempty"`
	FitRows        int             `json:"fit_rows,omitempty"`
	FitCameras     int             `json:"fit_cameras,omitempty"`
	FitWindow      *FitWindow      `json:"fit_window,omitempty"`
	FitDataSHA256  string          `json:"fit_data_sha256,omitempty"`
	ArtifactSHA256 string          `json:"artifact_sha256,omitempty"`
	SHA256         string          `json:"sha256,omitempty"`
	MAE            float64         `json:"mae"`
	Metrics        []HorizonMetric `json:"metrics,omitempty"`
}

// ModelProvenance is an alias for ModelMetadata.
type ModelProvenance = ModelMetadata

// SourceData summarizes the historical observations feeding the model.
type SourceData struct {
	SampleCount    int     `json:"sample_count"`
	WindowMinutes  int     `json:"window_minutes"`
	LastObservedAt string  `json:"last_observed_at"`
	AgeMinutes     float64 `json:"age_minutes"`
	Stale          bool    `json:"stale"`
}

// MultiHorizonResponse represents the consolidated multi-horizon forecast payload (10, 20, 30 min).
type MultiHorizonResponse struct {
	CameraID     string              `json:"camera_id"`
	RoadKey      string              `json:"road_key"`
	GeneratedAt  string              `json:"generated_at"`
	BaseTime     string              `json:"base_time"`
	Availability AvailabilityStatus  `json:"availability"`
	Experimental bool                `json:"experimental"`
	Current      CurrentCondition    `json:"current"`
	Horizons     []HorizonPrediction `json:"horizons"`
	Model        ModelMetadata       `json:"model"`
	SourceData   SourceData          `json:"source_data"`
}

// ForecastMultiResponse is an alias for MultiHorizonResponse.
type ForecastMultiResponse = MultiHorizonResponse

// ErrorKind classifies forecast error conditions.
type ErrorKind string

const (
	KindInvalidParameter    ErrorKind = "invalid_parameter"
	KindNotFound            ErrorKind = "not_found"
	KindInsufficientHistory ErrorKind = "insufficient_history"
	KindModelUnvalidated    ErrorKind = "model_unvalidated"
	KindServiceUnavailable  ErrorKind = "service_unavailable"
)

// Requirements specifies data criteria required for forecast inference.
type Requirements struct {
	MinObservations       int `json:"min_observations"`
	ObservedSamples       int `json:"observed_samples"`
	RequiredWindowMinutes int `json:"required_window_minutes"`
}

// ErrorResponse defines the uniform error payload structure for the forecast endpoint.
type ErrorResponse struct {
	Error        ErrorKind          `json:"error"`
	Message      string             `json:"message"`
	Availability AvailabilityStatus `json:"availability,omitempty"`
	Requirements *Requirements      `json:"requirements,omitempty"`
	Model        *ModelMetadata     `json:"model,omitempty"`
}
