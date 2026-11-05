package forecast

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"
)

var (
	ErrCameraNotFound      = errors.New("camera not found in catalog")
	ErrInvalidHorizon      = errors.New("horizon must be 10, 20, or 30 minutes")
	ErrInvalidModel        = errors.New("unsupported model name")
	ErrInsufficientHistory = errors.New("insufficient observation history available for camera")
	ErrInvalidCameraID     = errors.New("camera_id is required")
)

// Request defines the input parameters for a short-horizon forecast.
type Request struct {
	CameraID       string
	HorizonMinutes int
	BaseTime       time.Time
	Model          string
}

// ModelMetadata provides provenance and validation stats of the predictor.
type ModelMetadata struct {
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	TargetMode string  `json:"target_mode"`
	MAE        float64 `json:"mae"`
}

// SourceData summarizes the historical observations feeding the model.
type SourceData struct {
	SampleCount    int    `json:"sample_count"`
	LastObservedAt string `json:"last_observed_at"`
	Stale          bool   `json:"stale"`
}

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

// Response represents the complete forecast payload returned by the API.
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
	history HistoryReader
	catalog CameraCatalog
}

// NewService constructs a new forecast Service.
func NewService(history HistoryReader, catalog CameraCatalog) *Service {
	return &Service{
		history: history,
		catalog: catalog,
	}
}

// Predict produces a short-horizon congestion forecast for the requested camera.
func (s *Service) Predict(ctx context.Context, req Request) (*Response, error) {
	if s == nil || s.history == nil {
		return nil, fmt.Errorf("forecast service or history reader is nil")
	}

	if req.CameraID == "" {
		return nil, ErrInvalidCameraID
	}

	// Validate camera exists in catalog
	var roadKey string
	if s.catalog != nil {
		cam, found := s.catalog.GetCamera(req.CameraID)
		if !found {
			slog.Warn("camera not found in catalog for forecast", "camera_id", req.CameraID)
			return nil, ErrCameraNotFound
		}
		roadKey = cam.RoadKey
	}

	// Validate horizon
	if req.HorizonMinutes == 0 {
		req.HorizonMinutes = 10
	}
	if !IsSupportedHorizon(req.HorizonMinutes) {
		slog.Warn("invalid horizon requested", "camera_id", req.CameraID, "horizon", req.HorizonMinutes)
		return nil, ErrInvalidHorizon
	}

	// Validate base time
	if req.BaseTime.IsZero() {
		req.BaseTime = time.Now().UTC()
	}

	// Validate model selector
	if req.Model == "" {
		req.Model = ModelRidgeDelta
	}
	if req.Model != ModelRidgeDelta && req.Model != ModelPersistence && req.Model != ModelHistoricalMedian {
		slog.Warn("unsupported model requested", "camera_id", req.CameraID, "model", req.Model)
		return nil, ErrInvalidModel
	}

	// Query recent history up to 45 minutes prior to baseTime
	observations, err := s.history.GetRecentByCameraBefore(ctx, req.CameraID, req.BaseTime, 45*time.Minute, 50)
	if err != nil {
		slog.Error("failed to query verdict history for forecast", "camera_id", req.CameraID, "error", err)
		return nil, fmt.Errorf("query verdict history: %w", err)
	}

	if len(observations) == 0 {
		slog.Warn("insufficient observation history for forecast", "camera_id", req.CameraID)
		return nil, ErrInsufficientHistory
	}

	latestObs := observations[0]
	lastObsTime := latestObs.ObservedAt
	age := req.BaseTime.Sub(lastObsTime)
	isStale := age > 15*time.Minute

	// Extract features
	features, currentSI, _ := BuildFeatures(observations, req.BaseTime)
	if math.IsNaN(currentSI) || math.IsInf(currentSI, 0) || currentSI < 0.0 {
		currentSI = 0.0
	}

	params := CalibratedRidgeParams[req.HorizonMinutes]

	var predictedSI float64
	var mae, rmse float64
	var targetMode string

	switch req.Model {
	case ModelRidgeDelta:
		predictedSI = PredictRidgeDelta(currentSI, features, params)
		mae = params.MAE
		rmse = params.RMSE
		targetMode = "delta"
	case ModelPersistence:
		predictedSI = currentSI
		// Persistence metrics from docs/reports/forecast_prototype_v1.json
		switch req.HorizonMinutes {
		case 10:
			mae = 0.00917440
			rmse = 0.01110912
		case 20:
			mae = 0.01837405
			rmse = 0.02220334
		case 30:
			mae = 0.02747778
			rmse = 0.03316360
		}
		targetMode = "persistence"
	case ModelHistoricalMedian:
		predictedSI = params.GlobalMedian
		// Historical median metrics from docs/reports/forecast_prototype_v1.json
		switch req.HorizonMinutes {
		case 10:
			mae = 0.02354196
			rmse = 0.02766057
		case 20:
			mae = 0.02359264
			rmse = 0.02774466
		case 30:
			mae = 0.02345770
			rmse = 0.02757248
		}
		targetMode = "historical_median"
	}

	if predictedSI < 0.0 || math.IsNaN(predictedSI) || math.IsInf(predictedSI, 0) {
		predictedSI = 0.0
	}

	predictedSeverity := SaturationToSeverity(predictedSI)
	predictedSpeed := EstimateSpeed(predictedSI)
	siLower, siUpper, speedLower, speedUpper := ComputeConfidenceIntervals(predictedSI, rmse)

	// Calculate overall forecast confidence
	baseConf := latestObs.Confidence
	if math.IsNaN(baseConf) || math.IsInf(baseConf, 0) || baseConf <= 0.0 {
		baseConf = 0.85
	}
	if isStale {
		baseConf *= 0.50
	}
	switch req.HorizonMinutes {
	case 20:
		baseConf *= 0.95
	case 30:
		baseConf *= 0.90
	}
	if baseConf > 0.99 {
		baseConf = 0.99
	}
	if baseConf < 0.10 {
		baseConf = 0.10
	}

	targetTime := req.BaseTime.Add(time.Duration(req.HorizonMinutes) * time.Minute)

	resp := &Response{
		CameraID:                 req.CameraID,
		SegmentID:                req.CameraID,
		RoadKey:                  roadKey,
		HorizonMinutes:           req.HorizonMinutes,
		BaseTime:                 req.BaseTime.UTC().Format(time.RFC3339Nano),
		TargetTime:               targetTime.UTC().Format(time.RFC3339Nano),
		CurrentSaturationIndex:   round(currentSI, 4),
		PredictedSaturationIndex: round(predictedSI, 4),
		PredictedSeverity:        predictedSeverity,
		PredictedSpeedKmh:        round(predictedSpeed, 1),
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
		Confidence: round(baseConf, 2),
		Model: ModelMetadata{
			Name:       req.Model,
			Version:    "1.0.0",
			TargetMode: targetMode,
			MAE:        round(mae, 6),
		},
		SourceData: SourceData{
			SampleCount:    len(observations),
			LastObservedAt: lastObsTime.UTC().Format(time.RFC3339Nano),
			Stale:          isStale,
		},
	}

	slog.Info("forecast computed",
		"camera_id", req.CameraID,
		"horizon", req.HorizonMinutes,
		"predicted_si", resp.PredictedSaturationIndex,
		"predicted_severity", resp.PredictedSeverity,
		"is_stale", isStale,
	)

	return resp, nil
}

func round(val float64, decimals int) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0.0
	}
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
