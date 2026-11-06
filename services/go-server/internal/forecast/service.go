package forecast

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"time"
)

var (
	ErrCameraNotFound      = errors.New("camera not found in catalog")
	ErrInvalidHorizon      = errors.New("horizon must be 10, 20, or 30 minutes")
	ErrInvalidModel        = errors.New("unsupported model name")
	ErrInsufficientHistory = errors.New("insufficient observation history available for camera")
	ErrModelUnvalidated    = errors.New("forecast model was not fitted on live traffic history")
	ErrInvalidCameraID     = errors.New("camera_id is required")
)

// ForecastError wraps an ErrorKind and contextual metadata with an underlying error.
type ForecastError struct {
	Kind         ErrorKind
	Message      string
	Requirements *Requirements
	Model        *ModelMetadata
	Err          error
}

func (e *ForecastError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *ForecastError) Unwrap() error {
	return e.Err
}

// Request defines the input parameters for a short-horizon forecast.
type Request struct {
	CameraID       string
	HorizonMinutes int
	BaseTime       time.Time
	Model          string
}

// Response represents the single-horizon forecast payload (retained for backward compatibility).
type Response struct {
	CameraID                 string              `json:"camera_id"`
	SegmentID                string              `json:"segment_id"`
	RoadKey                  string              `json:"road_key"`
	HorizonMinutes           int                 `json:"horizon_minutes"`
	BaseTime                 string              `json:"base_time"`
	TargetTime               string              `json:"target_time"`
	CurrentSaturationIndex   float64             `json:"current_saturation_index"`
	PredictedSaturationIndex float64             `json:"predicted_saturation_index"`
	PredictedSeverity        string              `json:"predicted_severity"`
	PredictedSpeedKmh        float64             `json:"predicted_speed_kmh"`
	ConfidenceIntervals      ConfidenceIntervals `json:"confidence_intervals"`
	Confidence               float64             `json:"confidence"`
	Model                    ModelMetadata       `json:"model"`
	SourceData               SourceData          `json:"source_data"`
}

// Service coordinates feature extraction, model inference, and output formatting.
type Service struct {
	history        HistoryReader
	catalog        CameraCatalog
	model          ModelMetadata
	allowSynthetic bool
	staleTTL       time.Duration
}

// Option configures Service options.
type Option func(*Service)

// WithAllowSynthetic configures whether synthetic models are permitted to serve predictions.
func WithAllowSynthetic(allow bool) Option {
	return func(s *Service) {
		s.allowSynthetic = allow
	}
}

// WithModelMetadata overrides the default model metadata and provenance.
func WithModelMetadata(meta ModelMetadata) Option {
	return func(s *Service) {
		s.model = meta
	}
}

// WithStaleTTL overrides the observation staleness threshold duration.
func WithStaleTTL(ttl time.Duration) Option {
	return func(s *Service) {
		s.staleTTL = ttl
	}
}

// NewService constructs a new forecast Service.
func NewService(history HistoryReader, catalog CameraCatalog, opts ...Option) *Service {
	envVal := os.Getenv("FORECAST_ALLOW_SYNTHETIC")
	cleanVal := strings.Trim(strings.TrimSpace(envVal), `"'`)
	envAllow := strings.EqualFold(cleanVal, "true") || cleanVal == "1"

	s := &Service{
		history:        history,
		catalog:        catalog,
		model:          DefaultModelMetadata,
		allowSynthetic: envAllow,
		staleTTL:       15 * time.Minute,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetAllowSynthetic provides runtime mutation of synthetic model allowance (used primarily in tests).
func (s *Service) SetAllowSynthetic(allow bool) {
	s.allowSynthetic = allow
}

// SetModelMetadata sets the model metadata (used primarily in tests to inject live history artifacts).
func (s *Service) SetModelMetadata(meta ModelMetadata) {
	s.model = meta
}

// PredictMulti produces a multi-horizon (10, 20, 30 min) congestion forecast for the requested camera.
func (s *Service) PredictMulti(ctx context.Context, req Request) (*MultiHorizonResponse, error) {
	if s == nil || s.history == nil {
		return nil, &ForecastError{
			Kind:    KindServiceUnavailable,
			Message: "forecast repository database is currently unreachable",
			Err:     errors.New("forecast service or history reader is nil"),
		}
	}

	if req.CameraID == "" {
		return nil, &ForecastError{
			Kind:    KindInvalidParameter,
			Message: "camera_id is required",
			Err:     ErrInvalidCameraID,
		}
	}

	// Validate model selector
	if req.Model == "" {
		req.Model = ModelRidgeDelta
	}
	if req.Model != ModelRidgeDelta && req.Model != ModelPersistence && req.Model != ModelHistoricalMedian {
		slog.Warn("unsupported model requested", "camera_id", req.CameraID, "model", req.Model)
		return nil, &ForecastError{
			Kind:    KindInvalidParameter,
			Message: "model must be ridge_delta, persistence, or historical_median",
			Err:     ErrInvalidModel,
		}
	}

	// Validate base time
	if req.BaseTime.IsZero() {
		req.BaseTime = time.Now().UTC()
	} else if req.BaseTime.After(time.Now().UTC().Add(60 * time.Second)) {
		return nil, &ForecastError{
			Kind:    KindInvalidParameter,
			Message: "timestamp cannot be more than 60s in the future",
			Err:     errors.New("timestamp cannot be more than 60s in the future"),
		}
	}

	// Validate camera exists in catalog
	var roadKey string
	if s.catalog != nil {
		cam, found := s.catalog.GetCamera(req.CameraID)
		if !found {
			slog.Warn("camera not found in catalog for forecast", "camera_id", req.CameraID)
			return nil, &ForecastError{
				Kind:    KindNotFound,
				Message: fmt.Sprintf("camera %s not found in catalog", req.CameraID),
				Err:     ErrCameraNotFound,
			}
		}
		roadKey = cam.RoadKey
	}

	// Fail-closed gate: if model is not validated on live history and synthetic models
	// are not explicitly permitted via FORECAST_ALLOW_SYNTHETIC, reject immediately
	// BEFORE issuing any history queries (zero DB read on unvalidated state).
	allowSynthetic := s.allowSynthetic
	if !allowSynthetic {
		envVal := os.Getenv("FORECAST_ALLOW_SYNTHETIC")
		cleanVal := strings.Trim(strings.TrimSpace(envVal), `"'`)
		if strings.EqualFold(cleanVal, "true") || cleanVal == "1" {
			allowSynthetic = true
		}
	}

	if s.model.FitSource != string(FitSourceLiveHistory) && !allowSynthetic {
		modelMeta := s.model
		modelMeta.Name = req.Model
		return nil, &ForecastError{
			Kind:    KindModelUnvalidated,
			Message: ErrModelUnvalidated.Error(),
			Model:   &modelMeta,
			Err:     ErrModelUnvalidated,
		}
	}

	// Query recent history up to 45 minutes prior to baseTime (limit 50 rows)
	observations, err := s.history.GetRecentByCameraBefore(ctx, req.CameraID, req.BaseTime, 45*time.Minute, 50)
	if err != nil {
		slog.Error("failed to query verdict history for forecast", "camera_id", req.CameraID, "error", err)
		return nil, &ForecastError{
			Kind:    KindServiceUnavailable,
			Message: "failed to query observation history: " + err.Error(),
			Err:     err,
		}
	}

	// Sample floor: serving requires at least MinServingSamples = 4 (current + 3 lags)
	const minServingSamples = 4
	if len(observations) < minServingSamples {
		slog.Warn("insufficient observation history for forecast", "camera_id", req.CameraID, "sample_count", len(observations))
		return nil, &ForecastError{
			Kind:    KindInsufficientHistory,
			Message: "fewer than 4 observations in the trailing 45 minutes",
			Requirements: &Requirements{
				MinObservations:       minServingSamples,
				ObservedSamples:       len(observations),
				RequiredWindowMinutes: 45,
			},
			Err: ErrInsufficientHistory,
		}
	}

	latestObs := observations[0]
	lastObsTime := latestObs.ObservedAt
	age := req.BaseTime.Sub(lastObsTime)
	staleThreshold := s.staleTTL
	if staleThreshold <= 0 {
		staleThreshold = 15 * time.Minute
	}
	isStale := age > staleThreshold

	var availability AvailabilityStatus = StatusOk
	if isStale {
		availability = StatusStale
	}

	experimental := (s.model.FitSource != string(FitSourceLiveHistory))

	// Extract features
	features, currentSI, _ := BuildFeatures(observations, req.BaseTime)
	if math.IsNaN(currentSI) || math.IsInf(currentSI, 0) || currentSI < 0.0 {
		currentSI = 0.0
	}
	currentSpeed := EstimateSpeed(currentSI)
	currentSeverity := SaturationToSeverity(currentSI)

	baseConf := latestObs.Confidence
	if math.IsNaN(baseConf) || math.IsInf(baseConf, 0) || baseConf <= 0.0 {
		baseConf = 0.85
	}
	if baseConf > 0.99 {
		baseConf = 0.99
	}

	// Compute predictions for all 3 horizons: 10, 20, 30 min
	horizons := make([]HorizonPrediction, 0, 3)
	for _, hMinutes := range []int{10, 20, 30} {
		var predictedSI float64
		var rmse float64
		params := CalibratedRidgeParams[hMinutes]

		switch req.Model {
		case ModelRidgeDelta:
			predictedSI = PredictRidgeDelta(currentSI, features, params)
			rmse = params.RMSE
		case ModelPersistence:
			predictedSI = currentSI
			switch hMinutes {
			case 10:
				rmse = 0.01110912
			case 20:
				rmse = 0.02220334
			case 30:
				rmse = 0.03316360
			}
		case ModelHistoricalMedian:
			predictedSI = params.GlobalMedian
			switch hMinutes {
			case 10:
				rmse = 0.02766057
			case 20:
				rmse = 0.02774466
			case 30:
				rmse = 0.02757248
			}
		}

		if predictedSI < 0.0 || math.IsNaN(predictedSI) || math.IsInf(predictedSI, 0) {
			predictedSI = 0.0
		}

		predictedSeverity := SaturationToSeverity(predictedSI)
		predictedSpeed := EstimateSpeed(predictedSI)
		siLower, siUpper, speedLower, speedUpper := ComputeConfidenceIntervals(predictedSI, rmse)

		hConf := baseConf
		if isStale {
			hConf *= 0.50
		}
		switch hMinutes {
		case 20:
			hConf *= 0.95
		case 30:
			hConf *= 0.90
		}
		if hConf > 0.99 {
			hConf = 0.99
		}
		if hConf < 0.10 {
			hConf = 0.10
		}

		targetTime := req.BaseTime.Add(time.Duration(hMinutes) * time.Minute)

		horizons = append(horizons, HorizonPrediction{
			HorizonMinutes:           hMinutes,
			TargetTime:               targetTime.UTC().Format(time.RFC3339Nano),
			PredictedSaturationIndex: round(predictedSI, 4),
			PredictedSeverity:        predictedSeverity,
			PredictedSpeedKmh:        round(predictedSpeed, 1),
			Confidence:               round(hConf, 2),
			ConfidenceIntervals: ConfidenceIntervals{
				SaturationIndex: Interval{
					Lower95: round(siLower, 4),
					Upper95: round(siUpper, 4),
				},
				SpeedKmh: Interval{
					Lower95: round(speedLower, 1),
					Upper95: round(speedUpper, 1),
				},
			},
		})
	}

	ageMin := round(age.Minutes(), 1)
	if ageMin < 0.0 {
		ageMin = 0.0
	}

	metaOut := s.model
	metaOut.Name = req.Model
	switch req.Model {
	case ModelRidgeDelta:
		metaOut.TargetMode = "delta"
	case ModelPersistence:
		metaOut.TargetMode = "persistence"
	case ModelHistoricalMedian:
		metaOut.TargetMode = "historical_median"
	}
	metaOut.Experimental = experimental
	metaOut.Validated = (s.model.FitSource == string(FitSourceLiveHistory))

	resp := &MultiHorizonResponse{
		CameraID:     req.CameraID,
		RoadKey:      roadKey,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		BaseTime:     req.BaseTime.UTC().Format(time.RFC3339Nano),
		Availability: availability,
		Experimental: experimental,
		Current: CurrentCondition{
			ObservedAt:      lastObsTime.UTC().Format(time.RFC3339Nano),
			SaturationIndex: round(currentSI, 4),
			Severity:        currentSeverity,
			SpeedKmh:        round(currentSpeed, 1),
			Confidence:      round(baseConf, 2),
		},
		Horizons: horizons,
		Model:    metaOut,
		SourceData: SourceData{
			SampleCount:    len(observations),
			WindowMinutes:  45,
			LastObservedAt: lastObsTime.UTC().Format(time.RFC3339Nano),
			AgeMinutes:     ageMin,
			Stale:          isStale,
		},
	}

	slog.Info("multi-horizon forecast computed",
		"camera_id", req.CameraID,
		"availability", availability,
		"is_stale", isStale,
		"experimental", experimental,
	)

	return resp, nil
}

// PredictMultiHorizon computes multi-horizon predictions for the specified camera at the given time.
func (s *Service) PredictMultiHorizon(ctx context.Context, cameraID string, at time.Time) (*MultiHorizonResponse, error) {
	return s.PredictMulti(ctx, Request{
		CameraID: cameraID,
		BaseTime: at,
		Model:    ModelRidgeDelta,
	})
}

// Predict produces a single-horizon forecast for the requested camera (retained for backward compatibility).
func (s *Service) Predict(ctx context.Context, req Request) (*Response, error) {
	multiResp, err := s.PredictMulti(ctx, req)
	if err != nil {
		return nil, err
	}

	if req.HorizonMinutes == 0 {
		req.HorizonMinutes = 10
	}

	var matched *HorizonPrediction
	for i := range multiResp.Horizons {
		if multiResp.Horizons[i].HorizonMinutes == req.HorizonMinutes {
			matched = &multiResp.Horizons[i]
			break
		}
	}
	if matched == nil && len(multiResp.Horizons) > 0 {
		matched = &multiResp.Horizons[0]
	}

	var predSI, predSpeed, conf float64
	var predSev, targetTime string
	var intervals ConfidenceIntervals
	if matched != nil {
		predSI = matched.PredictedSaturationIndex
		predSpeed = matched.PredictedSpeedKmh
		predSev = matched.PredictedSeverity
		conf = matched.Confidence
		targetTime = matched.TargetTime
		intervals = matched.ConfidenceIntervals
	}

	return &Response{
		CameraID:                 multiResp.CameraID,
		SegmentID:                multiResp.CameraID,
		RoadKey:                  multiResp.RoadKey,
		HorizonMinutes:           req.HorizonMinutes,
		BaseTime:                 multiResp.BaseTime,
		TargetTime:               targetTime,
		CurrentSaturationIndex:   multiResp.Current.SaturationIndex,
		PredictedSaturationIndex: predSI,
		PredictedSeverity:        predSev,
		PredictedSpeedKmh:        predSpeed,
		ConfidenceIntervals:      intervals,
		Confidence:               conf,
		Model:                    multiResp.Model,
		SourceData:               multiResp.SourceData,
	}, nil
}

func round(val float64, decimals int) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0.0
	}
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
