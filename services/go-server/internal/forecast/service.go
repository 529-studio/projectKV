package forecast

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// EnvModelPath points at the on-disk artifact consumed by LoadArtifact.
	EnvModelPath = "FORECAST_MODEL_PATH"

	// EnvReloadIntervalSec controls the background reload cadence in seconds.
	// Unset or empty keeps DefaultReloadInterval, "0" disables reloading.
	EnvReloadIntervalSec = "FORECAST_MODEL_RELOAD_INTERVAL_SEC"

	// EnvReloadSeconds is a backward-compatible alias for EnvReloadIntervalSec.
	EnvReloadSeconds = "FORECAST_MODEL_RELOAD_SECONDS"

	// DefaultReloadInterval is the cadence used when the environment does not specify one.
	DefaultReloadInterval = 60 * time.Second
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
	mu             sync.RWMutex
	history        HistoryReader
	catalog        CameraCatalog
	artifact       *ModelArtifact
	model          ModelMetadata
	allowSynthetic bool
	staleTTL       time.Duration

	// Reload bookkeeping guarded by mu.
	modelPath      string
	lastMtime      time.Time
	lastSize       int64
	reloadInterval time.Duration
	lastStatWarn   time.Time

	// reloadMu serialises the stat -> load -> swap sequence so a ticker tick
	// and a manual call cannot load the same file twice.
	reloadMu sync.Mutex

	// reloaderMu guards reloaderCtx; reloaderCtx makes StartReloader
	// idempotent so duplicate wiring cannot spawn duplicate tickers while
	// cleanly supporting restart immediately upon context cancellation.
	reloaderMu  sync.Mutex
	reloaderCtx context.Context
}

// snapshot is an immutable view of the state needed to serve one prediction.
type snapshot struct {
	artifact       *ModelArtifact
	model          ModelMetadata
	allowSynthetic bool
}

// Option configures Service options.
type Option func(*Service)

// WithAllowSynthetic configures whether synthetic models are permitted to serve predictions.
func WithAllowSynthetic(allow bool) Option {
	return func(s *Service) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.allowSynthetic = allow
	}
}

// WithModelMetadata overrides the default model metadata and provenance.
func WithModelMetadata(meta ModelMetadata) Option {
	return func(s *Service) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.model = meta
	}
}

// WithArtifact configures the model artifact to be used for inference.
func WithArtifact(artifact *ModelArtifact) Option {
	return func(s *Service) {
		if artifact != nil {
			s.mu.Lock()
			s.artifact = artifact
			s.model = artifact.DefaultMetadata()
			s.mu.Unlock()
		}
	}
}

// WithModelPath loads and configures a ModelArtifact from disk.
func WithModelPath(path string) Option {
	return func(s *Service) {
		path = strings.TrimSpace(path)
		s.mu.Lock()
		s.modelPath = path
		s.mu.Unlock()

		if path == "" {
			return
		}

		s.reloadMu.Lock()
		defer s.reloadMu.Unlock()

		fi, statErr := os.Stat(path)

		art, err := LoadArtifact(path)
		if err != nil {
			slog.Error("forecast: failed to load model artifact from path", "path", path, "error", err)
			return
		}

		s.mu.Lock()
		s.artifact = art
		s.model = art.DefaultMetadata()
		if statErr == nil {
			s.lastMtime = fi.ModTime()
			s.lastSize = fi.Size()
		}
		s.mu.Unlock()

		if statErr != nil {
			slog.Warn("forecast: could not stat model artifact after load", "path", path, "error", statErr)
		}
	}
}

// WithReloadInterval overrides the background reload cadence. A value of zero
// or less disables reloading.
func WithReloadInterval(d time.Duration) Option {
	return func(s *Service) {
		if d < 0 {
			d = 0
		}
		s.mu.Lock()
		s.reloadInterval = d
		s.mu.Unlock()
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
		artifact:       DefaultArtifact(),
		model:          DefaultModelMetadata,
		allowSynthetic: envAllow,
		staleTTL:       15 * time.Minute,
		reloadInterval: reloadIntervalFromEnv(),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.mu.RLock()
	needEnvPath := s.modelPath == ""
	s.mu.RUnlock()

	if needEnvPath {
		if p := strings.TrimSpace(os.Getenv(EnvModelPath)); p != "" {
			WithModelPath(p)(s)
		}
	}

	return s
}

// Model returns a copy of the metadata of the artifact currently serving predictions.
// Deep copies pointer and slice members to avoid sharing mutable state across reloader swaps.
func (s *Service) Model() ModelMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.model
	if s.model.FitWindow != nil {
		fw := *s.model.FitWindow
		cp.FitWindow = &fw
	}
	if s.model.Metrics != nil {
		cp.Metrics = make([]HorizonMetric, len(s.model.Metrics))
		copy(cp.Metrics, s.model.Metrics)
	}
	return cp
}

// ModelPath returns the path the service reloads from, or "" when reloading is not configured.
func (s *Service) ModelPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.modelPath
}

// ReloadInterval returns the configured background reload cadence. Zero means reloading is disabled.
func (s *Service) ReloadInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reloadInterval
}

// SetAllowSynthetic provides runtime mutation of synthetic model allowance (used primarily in tests).
func (s *Service) SetAllowSynthetic(allow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowSynthetic = allow
}

// SetModelMetadata sets the model metadata (used primarily in tests to inject live history artifacts).
func (s *Service) SetModelMetadata(meta ModelMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = meta
}

// snapshot copies the state needed to serve one prediction under a read lock.
func (s *Service) snapshot() snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot{
		artifact:       s.artifact,
		model:          s.model,
		allowSynthetic: s.allowSynthetic,
	}
}

// ReloadIfModified reloads the artifact when the file on disk changed since
// the last successful load. It reports whether the serving model was replaced.
//
// A failed load is logged, returns the error, and preserves the artifact
// currently serving traffic: a partially written or corrupt swap degrades to
// the last known good model rather than an outage.
func (s *Service) ReloadIfModified() (bool, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	s.mu.RLock()
	path := s.modelPath
	lastMtime := s.lastMtime
	lastSize := s.lastSize
	s.mu.RUnlock()

	if path == "" {
		return false, nil
	}

	preFi, err := os.Stat(path)
	if err != nil {
		s.mu.Lock()
		now := time.Now()
		// Log warn only on first occurrence or every 5 minutes to avoid log spam (MJ-6)
		if s.lastStatWarn.IsZero() || now.Sub(s.lastStatWarn) > 5*time.Minute {
			slog.Warn("forecast: failed to stat model artifact", "path", path, "error", err)
			s.lastStatWarn = now
		}
		s.mu.Unlock()
		return false, fmt.Errorf("forecast: stat model artifact %q: %w", path, err)
	}

	if preFi.ModTime().Equal(lastMtime) && preFi.Size() == lastSize {
		return false, nil
	}

	art, err := LoadArtifact(path)
	if err != nil {
		slog.Error("forecast: failed to reload model artifact", "path", path, "error", err)
		return false, err
	}

	// Compare pre-load and post-load stat to ensure no concurrent modification during read (MJ-5)
	postFi, err := os.Stat(path)
	if err != nil || !postFi.ModTime().Equal(preFi.ModTime()) || postFi.Size() != preFi.Size() {
		slog.Warn("forecast: model artifact changed during load, retrying next tick", "path", path)
		return false, fmt.Errorf("forecast: concurrent modification during reload, retrying")
	}

	s.mu.Lock()
	s.artifact = art
	s.model = art.DefaultMetadata()
	s.lastMtime = postFi.ModTime()
	s.lastSize = postFi.Size()
	s.lastStatWarn = time.Time{}
	meta := s.model
	s.mu.Unlock()

	slog.Info("forecast: reloaded model artifact",
		"path", path,
		"version", meta.Version,
		"sha256", art.SHA256,
	)

	return true, nil
}

// StartReloader polls the artifact file until ctx is cancelled. It returns
// immediately when the interval is not positive or no model path is
// configured, and is idempotent: only one ticker runs per live context.
func (s *Service) StartReloader(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	s.mu.RLock()
	path := s.modelPath
	s.mu.RUnlock()

	if path == "" {
		return
	}

	s.reloaderMu.Lock()
	if s.reloaderCtx != nil && s.reloaderCtx.Err() == nil {
		s.reloaderMu.Unlock()
		slog.Warn("forecast: model reloader already running", "path", path)
		return
	}
	s.reloaderCtx = ctx
	s.reloaderMu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		slog.Info("forecast: model reloader started", "path", path, "interval", interval.String())

		for {
			select {
			case <-ctx.Done():
				slog.Info("forecast: model reloader stopped", "path", path)
				return
			case <-ticker.C:
				if _, err := s.ReloadIfModified(); err != nil {
					continue
				}
			}
		}
	}()
}

// reloadIntervalFromEnv resolves the background reload cadence. Unset or empty
// keeps DefaultReloadInterval, "0" or a negative value disables reloading, and
// an unparsable value logs a warning and keeps the default.
func reloadIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvReloadIntervalSec))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(EnvReloadSeconds))
	}
	if raw == "" {
		return DefaultReloadInterval
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("forecast: invalid model reload interval, using default",
			"key", EnvReloadIntervalSec,
			"value", raw,
			"default", DefaultReloadInterval.String(),
		)
		return DefaultReloadInterval
	}

	if seconds <= 0 {
		return 0
	}

	return time.Duration(seconds) * time.Second
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

	snap := s.snapshot()

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
	allowSynthetic := snap.allowSynthetic
	if !allowSynthetic {
		envVal := os.Getenv("FORECAST_ALLOW_SYNTHETIC")
		cleanVal := strings.Trim(strings.TrimSpace(envVal), `"'`)
		if strings.EqualFold(cleanVal, "true") || cleanVal == "1" {
			allowSynthetic = true
		}
	}

	if snap.model.FitSource != string(FitSourceLiveHistory) && !allowSynthetic {
		modelMeta := snap.model
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

	experimental := (snap.model.FitSource != string(FitSourceLiveHistory))

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

		// Resolve parameters dynamically from loaded artifact, with fallback to CalibratedRidgeParams
		params := CalibratedRidgeParams[hMinutes]
		if snap.artifact != nil {
			if entry, ok := snap.artifact.Entry(req.Model); ok {
				if p, ok := entry.Params(hMinutes); ok {
					params = p
				}
			}
		}

		switch req.Model {
		case ModelRidgeDelta:
			predictedSI = PredictRidgeDelta(currentSI, features, params)
			rmse = params.RMSE
		case ModelPersistence:
			predictedSI = currentSI
			rmse = params.RMSE
			if rmse == 0 {
				switch hMinutes {
				case 10:
					rmse = 0.01110912
				case 20:
					rmse = 0.02220334
				case 30:
					rmse = 0.03316360
				}
			}
		case ModelHistoricalMedian:
			predictedSI = params.GlobalMedian
			rmse = params.RMSE
			if rmse == 0 {
				switch hMinutes {
				case 10:
					rmse = 0.02766057
				case 20:
					rmse = 0.02774466
				case 30:
					rmse = 0.02757248
				}
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

	metaOut := snap.model
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
	metaOut.Validated = (snap.model.FitSource == string(FitSourceLiveHistory))

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
