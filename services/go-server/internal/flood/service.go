package flood

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// Service coordinates dual-trigger flood monitoring and VLM verifications.
type Service struct {
	analyzer       hotset.Analyzer
	weatherSvc     WeatherProvider
	camReader      camera.Reader
	limiter        *QuotaLimiter
	chokepoints    []FloodChokepoint
	log            *slog.Logger

	mu             sync.RWMutex
	latestVerdicts map[string]FloodVerificationResult
}

// Option configures Service.
type Option func(*Service)

// WithLogger overrides the default logger.
func WithLogger(log *slog.Logger) Option {
	return func(s *Service) {
		if log != nil {
			s.log = log
		}
	}
}

// NewService instantiates a new flood Service.
func NewService(
	analyzer hotset.Analyzer,
	weatherSvc WeatherProvider,
	camReader camera.Reader,
	chokepoints []FloodChokepoint,
	limiter *QuotaLimiter,
	opts ...Option,
) *Service {
	s := &Service{
		analyzer:       analyzer,
		weatherSvc:     weatherSvc,
		camReader:      camReader,
		limiter:        limiter,
		chokepoints:    chokepoints,
		log:            slog.Default(),
		latestVerdicts: make(map[string]FloodVerificationResult),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// LoadChokepoints reads the curated JSON fixture from path with fallback paths.
func LoadChokepoints(path string) ([]FloodChokepoint, error) {
	resolved := resolveFixturePath(path)
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read flood chokepoints fixture (%s): %w", resolved, err)
	}

	var catalog ChokepointsCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("unmarshal flood chokepoints (%s): %w", resolved, err)
	}

	return catalog.Chokepoints, nil
}

func resolveFixturePath(rel string) string {
	candidates := []string{
		filepath.Clean(rel),
		filepath.Join("../..", rel),
		filepath.Join("../../..", rel),
		filepath.Join("../../../..", rel),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return rel
}

// Chokepoints returns the curated list of flood hotspots.
func (s *Service) Chokepoints() []FloodChokepoint {
	return s.chokepoints
}

// QuotaStatus returns current limiter status snapshot.
func (s *Service) QuotaStatus() QuotaStatus {
	return s.limiter.Status()
}

// LatestVerdicts returns a snapshot of cached latest verification verdicts.
func (s *Service) LatestVerdicts() map[string]FloodVerificationResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copyMap := make(map[string]FloodVerificationResult, len(s.latestVerdicts))
	for k, v := range s.latestVerdicts {
		copyMap[k] = v
	}
	return copyMap
}

// VerifyCamera executes an on-demand flood verification for a single camera (Trigger 2).
func (s *Service) VerifyCamera(ctx context.Context, cameraID string) (*FloodVerificationResult, error) {
	// 1. Check camera exists in catalog
	cam, ok := s.camReader.ByID(cameraID)
	if !ok || cam == nil {
		return nil, ErrCameraNotFound
	}

	// 2. Acquire quota token
	allowed, remMin, remDay := s.limiter.TryAcquire()
	if !allowed {
		if remMin <= 0 {
			return nil, ErrRateLimited
		}
		return nil, ErrDailyQuotaExceeded
	}

	// 3. Prepare hotset.Camera
	hotsetCam := hotset.Camera{
		ID:          cam.ID,
		Name:        cam.DisplayName,
		SnapshotURL: cam.SnapshotURL,
		Zone:        cam.Zone,
	}

	// 4. Run analysis
	verdict, err := s.analyzer.Analyze(ctx, hotsetCam)
	if err != nil {
		s.log.Error("flood: analyzer failed on camera", "camera_id", cameraID, "error", err)
		return nil, fmt.Errorf("analyzer failure: %w", err)
	}

	// 5. Build result
	floodSeverity := verdict.FloodSeverity
	if floodSeverity == "" {
		floodSeverity = SeverityDry
	}

	res := FloodVerificationResult{
		CameraID:             cam.ID,
		CameraName:           cam.DisplayName,
		FloodSeverity:        floodSeverity,
		Confidence:           verdict.Confidence,
		Source:               verdict.Source,
		VerifiedAt:           verdict.ObservedAt,
		QuotaRemainingMinute: remMin,
		QuotaRemainingDay:    remDay,
	}

	// Cache result
	s.mu.Lock()
	s.latestVerdicts[cameraID] = res
	s.mu.Unlock()

	return &res, nil
}

// StartEnvironmentalTrigger starts a background loop polling flood chokepoints during rain events (Trigger 1).
func (s *Service) StartEnvironmentalTrigger(ctx context.Context, pollInterval time.Duration, onVerdict func(hotset.Verdict)) {
	s.log.Info("flood: starting environmental trigger loop", "interval", pollInterval)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial check on start
	s.checkEnvironmentalConditions(ctx, onVerdict)

	for {
		select {
		case <-ctx.Done():
			s.log.Info("flood: stopping environmental trigger loop")
			return
		case <-ticker.C:
			s.checkEnvironmentalConditions(ctx, onVerdict)
		}
	}
}

func (s *Service) checkEnvironmentalConditions(ctx context.Context, onVerdict func(hotset.Verdict)) {
	if s.weatherSvc == nil {
		return
	}

	w, err := s.weatherSvc.GetWeather(ctx)
	if err != nil {
		s.log.Warn("flood: failed to fetch weather for environmental trigger", "error", err)
		return
	}
	if w == nil {
		return
	}

	// Reconciled threshold: RainMm >= 1.0 or WMO rain codes (51-67, 80-82, 95-99)
	isRaining := w.RainMm >= 1.0 || isRainWmoCode(w.WeatherCode)
	if !isRaining {
		s.log.Debug("flood: dry weather (0 VLM calls consumed)", "rain_mm", w.RainMm, "wmo_code", w.WeatherCode)
		return
	}

	s.log.Info("flood: active rain event detected, inspecting chokepoints", "rain_mm", w.RainMm, "wmo_code", w.WeatherCode)

	for _, cp := range s.chokepoints {
		if ctx.Err() != nil {
			return
		}

		// Enforce rate limiter
		if !s.limiter.Allow() {
			s.log.Warn("flood: skipping chokepoint environmental verification due to rate/quota limits", "camera_id", cp.CameraID)
			break
		}

		cam, ok := s.camReader.ByID(cp.CameraID)
		if !ok || cam == nil {
			continue
		}

		hotsetCam := hotset.Camera{
			ID:          cam.ID,
			Name:        cam.DisplayName,
			SnapshotURL: cam.SnapshotURL,
			Zone:        cam.Zone,
		}

		verdict, err := s.analyzer.Analyze(ctx, hotsetCam)
		if err != nil {
			s.log.Warn("flood: background analysis failed", "camera_id", cp.CameraID, "error", err)
			continue
		}

		floodSeverity := verdict.FloodSeverity
		if floodSeverity == "" {
			floodSeverity = SeverityDry
		}

		res := FloodVerificationResult{
			CameraID:      cam.ID,
			CameraName:    cam.DisplayName,
			FloodSeverity: floodSeverity,
			Confidence:    verdict.Confidence,
			Source:        verdict.Source,
			VerifiedAt:    verdict.ObservedAt,
		}
		status := s.limiter.Status()
		res.QuotaRemainingMinute = status.RemainingRPM
		res.QuotaRemainingDay = status.RemainingRPD

		s.mu.Lock()
		s.latestVerdicts[cp.CameraID] = res
		s.mu.Unlock()

		if onVerdict != nil {
			onVerdict(verdict)
		}
	}
}

func isRainWmoCode(code int) bool {
	return (code >= 51 && code <= 67) || (code >= 80 && code <= 82) || (code >= 95 && code <= 99)
}
