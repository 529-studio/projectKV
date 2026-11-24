// Package corridor implements traffic corridor status aggregation described in
// ADR-004 §D1 (2a Corridor status): status of a named road from its cameras.
package corridor

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// CameraVerdict represents a single camera's evaluation within a corridor.
type CameraVerdict struct {
	CameraID         string     `json:"camera_id"`
	Name             string     `json:"name,omitempty"`
	Latitude         float64    `json:"latitude,omitempty"`
	Longitude        float64    `json:"longitude,omitempty"`
	Severity         string     `json:"severity"` // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
	FloodSeverity    string     `json:"flood_severity,omitempty"`
	FloodDepthCm     *float64   `json:"flood_depth_cm,omitempty"`
	FloodHazardLevel *int       `json:"flood_hazard_level,omitempty"`
	Confidence       float64    `json:"confidence,omitempty"`
	Source           string     `json:"source,omitempty"`
	ObservedAt       *time.Time `json:"observed_at,omitempty"`
	Stale            bool       `json:"stale"`
}

// CorridorStatus represents the aggregated health and traffic condition of a named road corridor.
type CorridorStatus struct {
	RoadKey               string          `json:"road_key"`
	WorstSeverity         string          `json:"worst_severity"` // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
	WorstFloodSeverity    string          `json:"worst_flood_severity,omitempty"`
	MaxDepthCm            *float64        `json:"max_depth_cm,omitempty"`
	CoveragePct           float64         `json:"coverage_pct"`
	OldestObservationAgeS *int            `json:"oldest_observation_age_s"`
	TotalCameras          int             `json:"total_cameras"`
	VerifiedCameras       int             `json:"verified_cameras"`
	Cameras               []CameraVerdict `json:"cameras"`
}

// RoadSummary represents a known road key and its associated camera count.
type RoadSummary struct {
	RoadKey     string `json:"road_key"`
	CameraCount int    `json:"camera_count"`
}

// RoadsResponse wraps the list of all known road keys with camera counts.
type RoadsResponse struct {
	Total int           `json:"total"`
	Roads []RoadSummary `json:"roads"`
}

// CameraProvider defines camera lookup operations needed by the corridor service.
type CameraProvider interface {
	All() []camera.Camera
}

// VerdictProvider defines verdict query operations needed by the corridor service.
type VerdictProvider interface {
	GetLatest() []hotset.Verdict
}

// FloodHistoryObservation represents a historical flood depth reading for a camera.
type FloodHistoryObservation struct {
	CameraID     string
	FloodDepthCm *float64
	ObservedAt   time.Time
}

// FloodHistoryProvider defines the interface to fetch historical flood observations.
type FloodHistoryProvider interface {
	GetRecentByCameraIDs(ctx context.Context, cameraIDs []string, start, end time.Time) ([]FloodHistoryObservation, error)
}

// TidePredictor calculates astronomical river tide height in meters at a given station and time.
type TidePredictor interface {
	PredictTide(station string, t time.Time) float64
}

// HourlyHistoryItem represents aggregated corridor conditions within a 1-hour window.
type HourlyHistoryItem struct {
	Time           time.Time `json:"time"`
	HourLabel      string    `json:"hour_label"`       // e.g. "17:00"
	AvgDepthCm     float64   `json:"avg_depth_cm"`     // Arithmetic mean depth in cm (0.0 if dry/unmeasured)
	MaxDepthCm     float64   `json:"max_depth_cm"`     // Peak flood depth observed along the corridor in cm
	TideLevelM     float64   `json:"tide_level_m"`     // Astronomical tide height in meters
	TideAlertLevel string    `json:"tide_alert_level"` // "NORMAL" | "LEVEL_1" | "LEVEL_2" | "LEVEL_3"
	HazardLevel    string    `json:"hazard_level"`     // "CLEAR" | "CAUTION" | "IMPASSABLE"
}

// HistorySummary provides peak metrics and a glanceable commuter advisory headline.
type HistorySummary struct {
	PeakTideTime   *time.Time        `json:"peak_tide_time,omitempty"`
	PeakTideLevelM float64           `json:"peak_tide_level_m"`
	PeakFloodTime  *time.Time        `json:"peak_flood_time,omitempty"`
	MaxDepthCm     float64           `json:"max_depth_cm"`
	Headline       map[string]string `json:"headline"`
}

// CorridorHistoryResponse represents the downsampled historical trends payload for /api/roads/:road_key/history.
type CorridorHistoryResponse struct {
	RoadKey     string              `json:"road_key"`
	Hours       int                 `json:"hours"`
	Station     string              `json:"station"`      // "phu_an" | "nha_be"
	StationName map[string]string   `json:"station_name"` // e.g. {"vi": "Trạm Phú An (Sông Sài Gòn)", "en": "Phu An Station (Saigon River)"}
	Summary     HistorySummary      `json:"summary"`
	Timeline    []HourlyHistoryItem `json:"timeline"`
}

// Option configures optional Service dependencies.
type Option func(*Service)

// WithFloodHistory injects a FloodHistoryProvider.
func WithFloodHistory(prov FloodHistoryProvider) Option {
	return func(s *Service) {
		s.floodHistory = prov
	}
}

// WithTidePredictor injects a TidePredictor.
func WithTidePredictor(tp TidePredictor) Option {
	return func(s *Service) {
		s.tidePredictor = tp
	}
}

// WithNowFunc injects a custom time provider (for deterministic tests).
func WithNowFunc(fn func() time.Time) Option {
	return func(s *Service) {
		s.nowFunc = fn
	}
}

// Service provides corridor status and road inventory operations.
type Service struct {
	cameras       CameraProvider
	verdicts      VerdictProvider
	floodHistory  FloodHistoryProvider
	tidePredictor TidePredictor
	nowFunc       func() time.Time
	appEnv        string
	staleTTL      time.Duration
}

// NewService constructs a Service with injected dependencies and optional features.
func NewService(cameras CameraProvider, verdicts VerdictProvider, appEnv string, staleTTL time.Duration, opts ...Option) *Service {
	if staleTTL <= 0 {
		staleTTL = 180 * time.Second
	}
	s := &Service{
		cameras:  cameras,
		verdicts: verdicts,
		appEnv:   appEnv,
		staleTTL: staleTTL,
		nowFunc:  time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Severity ranking helper: CONGESTED/HEAVY (4) > MODERATE (2) > CLEAR (1) > UNKNOWN (0).
func severityRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CONGESTED", "HEAVY":
		return 4
	case "MODERATE":
		return 2
	case "CLEAR":
		return 1
	default:
		return 0
	}
}

// GetStatus returns the aggregated CorridorStatus for the specified roadKey.
// Returns (status, true) if cameras matching the roadKey exist, or (nil, false) otherwise.
func (s *Service) GetStatus(roadKey string) (*CorridorStatus, bool) {
	target := strings.ToLower(strings.TrimSpace(roadKey))
	if target == "" {
		return nil, false
	}

	allCameras := s.cameras.All()
	var matchingCameras []camera.Camera
	for _, c := range allCameras {
		if strings.EqualFold(c.RoadKey, target) {
			matchingCameras = append(matchingCameras, c)
			continue
		}
		for _, k := range c.RoadKeys {
			if strings.EqualFold(k, target) {
				matchingCameras = append(matchingCameras, c)
				break
			}
		}
	}

	if len(matchingCameras) == 0 {
		return nil, false
	}

	return s.aggregateStatus(target, matchingCameras), true
}

// GetHistory returns downsampled hourly historical flood depth and tidal correlation metrics
// for the given roadKey over the past hours (clamped to [1, 72], default 24).
// Returns (response, true) if cameras matching the roadKey exist, or (nil, false) otherwise.
func (s *Service) GetHistory(roadKey string, hours int) (*CorridorHistoryResponse, bool) {
	target := strings.ToLower(strings.TrimSpace(roadKey))
	if target == "" {
		return nil, false
	}

	allCameras := s.cameras.All()
	var matchingCameraIDs []string
	for _, c := range allCameras {
		if strings.EqualFold(c.RoadKey, target) {
			matchingCameraIDs = append(matchingCameraIDs, c.ID)
			continue
		}
		for _, k := range c.RoadKeys {
			if strings.EqualFold(k, target) {
				matchingCameraIDs = append(matchingCameraIDs, c.ID)
				break
			}
		}
	}

	if len(matchingCameraIDs) == 0 {
		return nil, false
	}

	if hours <= 0 {
		hours = 24
	} else if hours > 72 {
		hours = 72
	}

	station := "phu_an"
	stationName := map[string]string{
		"vi": "Trạm Phú An (Sông Sài Gòn)",
		"en": "Phu An Station (Saigon River)",
	}
	if target == "huynh_tan_phat" {
		station = "nha_be"
		stationName = map[string]string{
			"vi": "Trạm Nhà Bè (Ngã ba sông Đồng Tranh)",
			"en": "Nha Be Station (Dong Tranh River)",
		}
	}

	now := s.nowFunc().UTC()
	windowEnd := now.Truncate(time.Hour).Add(time.Hour)
	windowStart := windowEnd.Add(-time.Duration(hours) * time.Hour)

	var observations []FloodHistoryObservation
	if s.floodHistory != nil {
		obs, err := s.floodHistory.GetRecentByCameraIDs(context.Background(), matchingCameraIDs, windowStart, windowEnd)
		if err == nil {
			observations = obs
		}
	}

	timeline := make([]HourlyHistoryItem, hours)
	for i := 0; i < hours; i++ {
		bucketStart := windowStart.Add(time.Duration(i) * time.Hour)
		bucketEnd := bucketStart.Add(time.Hour)

		var sumDepth float64
		var countMeasured int
		var maxDepth float64

		for _, ob := range observations {
			if (ob.ObservedAt.Equal(bucketStart) || ob.ObservedAt.After(bucketStart)) && ob.ObservedAt.Before(bucketEnd) {
				if ob.FloodDepthCm != nil {
					d := *ob.FloodDepthCm
					sumDepth += d
					countMeasured++
					if d > maxDepth {
						maxDepth = d
					}
				}
			}
		}

		avgDepth := 0.0
		if countMeasured > 0 {
			avgDepth = math.Round((sumDepth/float64(countMeasured))*10) / 10
		}
		maxDepth = math.Round(maxDepth*10) / 10

		var tideLevel float64
		if s.tidePredictor != nil {
			tideLevel = s.tidePredictor.PredictTide(station, bucketStart)
		}

		tideAlert := "NORMAL"
		if tideLevel >= 1.60 {
			tideAlert = "LEVEL_3"
		} else if tideLevel >= 1.50 {
			tideAlert = "LEVEL_2"
		} else if tideLevel >= 1.40 {
			tideAlert = "LEVEL_1"
		}

		hazard := "CLEAR"
		if maxDepth > 20.0 {
			hazard = "IMPASSABLE"
		} else if maxDepth >= 10.0 {
			hazard = "CAUTION"
		}

		timeline[i] = HourlyHistoryItem{
			Time:           bucketStart,
			HourLabel:      bucketStart.Format("15:04"),
			AvgDepthCm:     avgDepth,
			MaxDepthCm:     maxDepth,
			TideLevelM:     tideLevel,
			TideAlertLevel: tideAlert,
			HazardLevel:    hazard,
		}
	}

	var peakTideLevel float64 = -999.0
	var peakTideTime time.Time
	var peakFloodDepth float64 = 0.0
	var peakFloodTime *time.Time

	for _, item := range timeline {
		if item.TideLevelM > peakTideLevel {
			peakTideLevel = item.TideLevelM
			peakTideTime = item.Time
		}
		if item.MaxDepthCm > peakFloodDepth {
			peakFloodDepth = item.MaxDepthCm
			t := item.Time
			peakFloodTime = &t
		}
	}

	tideHourStr := peakTideTime.Format("15:04")
	var headline map[string]string
	if peakFloodTime != nil && peakFloodDepth > 0 {
		floodHourStr := peakFloodTime.Format("15:04")
		headline = map[string]string{
			"vi": fmt.Sprintf("Đỉnh ngập %.0fcm lúc %s • Đỉnh triều %.2fm lúc %s", peakFloodDepth, floodHourStr, peakTideLevel, tideHourStr),
			"en": fmt.Sprintf("Peak flood %.0fcm at %s • Peak tide %.2fm at %s", peakFloodDepth, floodHourStr, peakTideLevel, tideHourStr),
		}
	} else {
		headline = map[string]string{
			"vi": fmt.Sprintf("Tuyến đường khô ráo • Đỉnh triều %.2fm lúc %s", peakTideLevel, tideHourStr),
			"en": fmt.Sprintf("Corridor dry • Peak tide %.2fm at %s", peakTideLevel, tideHourStr),
		}
	}

	var ptTime *time.Time
	if !peakTideTime.IsZero() {
		ptTime = &peakTideTime
	}

	return &CorridorHistoryResponse{
		RoadKey:     target,
		Hours:       hours,
		Station:     station,
		StationName: stationName,
		Summary: HistorySummary{
			PeakTideTime:   ptTime,
			PeakTideLevelM: peakTideLevel,
			PeakFloodTime:  peakFloodTime,
			MaxDepthCm:     peakFloodDepth,
			Headline:       headline,
		},
		Timeline: timeline,
	}, true
}

// GetStatusSummary returns a map of all corridor statuses by road key.
func (s *Service) GetStatusSummary() map[string]CorridorStatus {
	allCameras := s.cameras.All()
	cameraMap := make(map[string][]camera.Camera)
	for _, c := range allCameras {
		keys := make(map[string]struct{})
		if c.RoadKey != "" {
			keys[strings.ToLower(c.RoadKey)] = struct{}{}
		}
		for _, k := range c.RoadKeys {
			if k != "" {
				keys[strings.ToLower(k)] = struct{}{}
			}
		}
		for k := range keys {
			cameraMap[k] = append(cameraMap[k], c)
		}
	}

	summaries := make(map[string]CorridorStatus)
	for roadKey, cameras := range cameraMap {
		status := s.aggregateStatus(roadKey, cameras)
		summaries[roadKey] = *status
	}
	return summaries
}

func (s *Service) aggregateStatus(roadKey string, matchingCameras []camera.Camera) *CorridorStatus {
	// Index latest verdicts by camera ID
	latestVerdicts := s.verdicts.GetLatest()
	verdictMap := make(map[string]hotset.Verdict, len(latestVerdicts))
	for _, v := range latestVerdicts {
		verdictMap[v.CameraID] = v
	}

	var (
		cameraVerdicts = make([]CameraVerdict, 0, len(matchingCameras))
		verifiedCount  = 0
		worstRank      = 0
		worstSeverity  = "UNKNOWN"
		worstFloodSev  = "UNKNOWN"
		maxDepthCm     *float64
		maxAgeSeconds  *int
	)

	for _, cam := range matchingCameras {
		camName := strings.TrimSpace(cam.DisplayName)
		if camName == "" {
			camName = strings.TrimSpace(cam.Title)
		}

		v, hasVerdict := verdictMap[cam.ID]
		if !hasVerdict {
			cameraVerdicts = append(cameraVerdicts, CameraVerdict{
				CameraID:  cam.ID,
				Name:      camName,
				Latitude:  cam.Latitude,
				Longitude: cam.Longitude,
				Severity:  "UNKNOWN",
				Stale:     false,
			})
			continue
		}

		// Filter mock verdicts in non-dev environment (ADR-004 §D10 / Provenance safety rule)
		if strings.ToLower(v.Source) == "mock" && strings.ToLower(s.appEnv) != "dev" {
			cameraVerdicts = append(cameraVerdicts, CameraVerdict{
				CameraID:  cam.ID,
				Name:      camName,
				Latitude:  cam.Latitude,
				Longitude: cam.Longitude,
				Severity:  "UNKNOWN",
				Stale:     false,
			})
			continue
		}

		// Evaluate observation time and staleness
		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}

		isStale := v.Stale
		if !isStale && !obs.IsZero() && time.Since(obs) > s.staleTTL {
			isStale = true
		}

		var obsPtr *time.Time
		if !obs.IsZero() {
			obsCopy := obs
			obsPtr = &obsCopy
		}

		var depthPtr *float64
		var hazardLvlPtr *int
		if v.FloodDepthCm != nil {
			dCopy := *v.FloodDepthCm
			depthPtr = &dCopy
		}
		if v.FloodHazardLevel != nil {
			hCopy := *v.FloodHazardLevel
			hazardLvlPtr = &hCopy
		}

		sev := strings.ToUpper(strings.TrimSpace(v.Severity))
		if sev == "" {
			sev = "UNKNOWN"
		}

		// Fresh, non-stale verdict qualifies as verified camera
		if !isStale && sev != "UNKNOWN" {
			verifiedCount++
			rank := severityRank(sev)
			if rank > worstRank {
				worstRank = rank
				worstSeverity = sev
			}

			if v.FloodDepthCm != nil {
				if maxDepthCm == nil || *v.FloodDepthCm > *maxDepthCm {
					dCopy := *v.FloodDepthCm
					maxDepthCm = &dCopy
				}
			}
			if floodRank(v.FloodSeverity) > floodRank(worstFloodSev) {
				worstFloodSev = v.FloodSeverity
			}

			if !obs.IsZero() {
				age := int(time.Since(obs).Seconds())
				if age < 0 {
					age = 0
				}
				if maxAgeSeconds == nil || age > *maxAgeSeconds {
					ageCopy := age
					maxAgeSeconds = &ageCopy
				}
			}
		}

		cameraVerdicts = append(cameraVerdicts, CameraVerdict{
			CameraID:         cam.ID,
			Name:             camName,
			Latitude:         cam.Latitude,
			Longitude:        cam.Longitude,
			Severity:         sev,
			FloodSeverity:    v.FloodSeverity,
			FloodDepthCm:     depthPtr,
			FloodHazardLevel: hazardLvlPtr,
			Confidence:       v.Confidence,
			Source:           v.Source,
			ObservedAt:       obsPtr,
			Stale:            isStale,
		})
	}

	// Sort cameras alphabetically by name for predictable deterministic output
	sort.Slice(cameraVerdicts, func(i, j int) bool {
		return cameraVerdicts[i].Name < cameraVerdicts[j].Name
	})

	total := len(matchingCameras)
	coveragePct := 0.0
	if total > 0 && verifiedCount > 0 {
		coveragePct = math.Round((float64(verifiedCount)/float64(total))*1000) / 10
	}

	// Honesty rule (ADR-004 §D4): If no fresh verdict verified, worst_severity is UNKNOWN
	if verifiedCount == 0 {
		worstSeverity = "UNKNOWN"
		worstFloodSev = "UNKNOWN"
	}

	return &CorridorStatus{
		RoadKey:               roadKey,
		WorstSeverity:         worstSeverity,
		WorstFloodSeverity:    worstFloodSev,
		MaxDepthCm:            maxDepthCm,
		CoveragePct:           coveragePct,
		OldestObservationAgeS: maxAgeSeconds,
		TotalCameras:          total,
		VerifiedCameras:       verifiedCount,
		Cameras:               cameraVerdicts,
	}
}

// ListRoads returns all distinct road keys across the catalog with camera counts,
// sorted by camera count descending, then road key ascending.
func (s *Service) ListRoads() RoadsResponse {
	allCameras := s.cameras.All()
	countMap := make(map[string]int)

	for _, cam := range allCameras {
		keys := make(map[string]struct{})
		if cam.RoadKey != "" {
			keys[strings.ToLower(cam.RoadKey)] = struct{}{}
		}
		for _, k := range cam.RoadKeys {
			if k != "" {
				keys[strings.ToLower(k)] = struct{}{}
			}
		}
		for k := range keys {
			countMap[k]++
		}
	}

	roads := make([]RoadSummary, 0, len(countMap))
	for k, count := range countMap {
		roads = append(roads, RoadSummary{
			RoadKey:     k,
			CameraCount: count,
		})
	}

	sort.Slice(roads, func(i, j int) bool {
		if roads[i].CameraCount != roads[j].CameraCount {
			return roads[i].CameraCount > roads[j].CameraCount
		}
		return roads[i].RoadKey < roads[j].RoadKey
	})

	return RoadsResponse{
		Total: len(roads),
		Roads: roads,
	}
}

// GetRoute returns the GeoJSON FeatureCollection and route summary for a named arterial corridor.
func (s *Service) GetRoute(roadKey string, vehicle ...VehicleType) (*GeoJSONFeatureCollection, *CorridorRouteSummary, bool) {
	status, ok := s.GetStatus(roadKey)
	if !ok || status == nil {
		return nil, nil, false
	}

	vType := VehicleScooter
	if len(vehicle) > 0 && vehicle[0] != "" {
		vType = vehicle[0]
	}

	def, hasDef := LookupCorridor(roadKey)
	if !hasDef {
		// Synthesize corridor geometry from matching cameras
		allCameras := s.cameras.All()
		var coords [][2]float64
		target := strings.ToLower(strings.TrimSpace(roadKey))
		for _, c := range allCameras {
			if strings.EqualFold(c.RoadKey, target) && c.Longitude != 0 && c.Latitude != 0 {
				coords = append(coords, [2]float64{c.Longitude, c.Latitude})
			}
		}
		sort.Slice(coords, func(i, j int) bool {
			return coords[i][0] < coords[j][0]
		})
		if len(coords) < 2 {
			if len(coords) == 1 {
				coords = append(coords, coords[0])
			} else {
				coords = [][2]float64{{106.69, 10.77}, {106.70, 10.78}}
			}
		}
		def = &CorridorDefinition{
			Key:              target,
			DisplayName:      strings.Title(strings.ReplaceAll(target, "_", " ")),
			Description:      "Tuyến đường " + target,
			LengthKm:         5.0,
			FreeFlowSpeedKmh: 35.0,
			Coordinates:      coords,
		}
	}

	summary := s.calculateRouteSummary(def, status, vType)

	var features []GeoJSONFeature

	// Feature 1: The corridor path LineString
	lineProps := map[string]interface{}{
		"road_key":             summary.RoadKey,
		"display_name":         summary.DisplayName,
		"description":          summary.Description,
		"length_km":            summary.LengthKm,
		"free_flow_seconds":    summary.FreeFlowSeconds,
		"estimated_seconds":    summary.EstimatedSeconds,
		"delay_seconds":        summary.DelaySeconds,
		"worst_severity":       summary.WorstSeverity,
		"worst_flood_severity": summary.WorstFloodSeverity,
		"passability_verdict":  string(summary.PassabilityVerdict),
		"status_color":         summary.StatusColor,
		"total_cameras":        summary.TotalCameras,
		"verified_cameras":     summary.VerifiedCameras,
		"coverage_pct":         summary.CoveragePct,
		"stroke":               summary.StatusColor,
		"stroke-width":         5,
		"stroke-opacity":       0.85,
	}
	if summary.MaxDepthCm != nil {
		lineProps["max_depth_cm"] = *summary.MaxDepthCm
	}
	if summary.MaxHazardLevel != nil {
		lineProps["max_hazard_level"] = *summary.MaxHazardLevel
	}

	features = append(features, GeoJSONFeature{
		Type: "Feature",
		Geometry: GeoJSONGeometry{
			Type:        "LineString",
			Coordinates: def.Coordinates,
		},
		Properties: lineProps,
	})

	// Features 2..N: Individual cameras along the corridor
	for _, cam := range status.Cameras {
		if cam.Latitude == 0 && cam.Longitude == 0 {
			continue
		}
		camProps := map[string]interface{}{
			"camera_id":      cam.CameraID,
			"name":           cam.Name,
			"severity":       cam.Severity,
			"flood_severity": cam.FloodSeverity,
			"stale":          cam.Stale,
			"marker-color":   severityToColor(cam.Severity, cam.FloodSeverity, cam.FloodDepthCm),
		}
		if cam.FloodDepthCm != nil {
			camProps["flood_depth_cm"] = *cam.FloodDepthCm
		}
		if cam.FloodHazardLevel != nil {
			camProps["flood_hazard_level"] = *cam.FloodHazardLevel
		}
		features = append(features, GeoJSONFeature{
			Type: "Feature",
			Geometry: GeoJSONGeometry{
				Type:        "Point",
				Coordinates: [2]float64{cam.Longitude, cam.Latitude},
			},
			Properties: camProps,
		})
	}

	return &GeoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	}, summary, true
}

// calculateRouteSummary computes travel duration, flood delay penalty, and passability verdict based on vehicle clearance.
func (s *Service) calculateRouteSummary(def *CorridorDefinition, status *CorridorStatus, vehicle VehicleType) *CorridorRouteSummary {
	var (
		maxDepthCm      *float64
		maxHazardLvl    *int
		worstFloodSev   = "DRY"
		chokepointCount = 0
	)

	for _, cam := range status.Cameras {
		if cam.Stale {
			continue
		}
		if cam.FloodDepthCm != nil {
			if maxDepthCm == nil || *cam.FloodDepthCm > *maxDepthCm {
				dCopy := *cam.FloodDepthCm
				maxDepthCm = &dCopy
			}
		}
		if cam.FloodHazardLevel != nil {
			if maxHazardLvl == nil || *cam.FloodHazardLevel > *maxHazardLvl {
				hCopy := *cam.FloodHazardLevel
				maxHazardLvl = &hCopy
			}
		}
		if cam.Severity == "CONGESTED" || cam.Severity == "HEAVY" || (cam.FloodDepthCm != nil && *cam.FloodDepthCm >= 10.0) {
			chokepointCount++
		}
		if floodRank(cam.FloodSeverity) > floodRank(worstFloodSev) {
			worstFloodSev = cam.FloodSeverity
		}
	}

	freeFlowSpeed := def.FreeFlowSpeedKmh
	if freeFlowSpeed <= 0 {
		freeFlowSpeed = 35.0
	}
	freeFlowSec := int(math.Round((def.LengthKm / freeFlowSpeed) * 3600))
	if freeFlowSec < 60 {
		freeFlowSec = 60
	}

	congestionFactor := 1.0
	switch status.WorstSeverity {
	case "CONGESTED", "HEAVY":
		congestionFactor = 2.2
	case "MODERATE":
		congestionFactor = 1.4
	default:
		congestionFactor = 1.0
	}

	estimatedSec := int(math.Round(float64(freeFlowSec) * congestionFactor))

	// Water flood delay penalty
	if maxDepthCm != nil {
		if *maxDepthCm > 20.0 {
			estimatedSec += 900 // +15 mins
		} else if *maxDepthCm >= 10.0 {
			estimatedSec += 360 // +6 mins
		} else if *maxDepthCm > 0.0 {
			estimatedSec += 120 // +2 mins
		}
	}

	delaySec := estimatedSec - freeFlowSec
	if delaySec < 0 {
		delaySec = 0
	}

	var (
		passability PassabilityVerdict
		statusColor string
	)

	thresholds, ok := VehicleThresholds[vehicle]
	if !ok {
		thresholds = VehicleThresholds[VehicleScooter]
	}

	isDepthImpassable := maxDepthCm != nil && *maxDepthCm >= thresholds.ImpassableDepthCm
	isDepthCaution := maxDepthCm != nil && *maxDepthCm >= thresholds.CautionDepthCm
	isFallbackImpassable := maxDepthCm == nil && ((maxHazardLvl != nil && *maxHazardLvl >= 3) || worstFloodSev == "FLOODED_DEEP")
	isFallbackCaution := maxDepthCm == nil && ((maxHazardLvl != nil && *maxHazardLvl >= 2) || worstFloodSev == "FLOODED_SHALLOW")
	isTrafficCongested := status.WorstSeverity == "CONGESTED" || status.WorstSeverity == "HEAVY"

	if isDepthImpassable || isFallbackImpassable {
		passability = PassabilityImpassable
		statusColor = "#ef4444" // Red: danger / engine drowning risk
	} else if isDepthCaution || isFallbackCaution || isTrafficCongested {
		passability = PassabilityCaution
		statusColor = "#eab308" // Yellow: caution / low clearance warning / congested
	} else if status.VerifiedCameras > 0 {
		passability = PassabilityPassable
		statusColor = "#22c55e" // Green: passable / normal flow
	} else {
		passability = PassabilityUnknown
		statusColor = "#71717a" // Muted / unknown
	}

	return &CorridorRouteSummary{
		RoadKey:            def.Key,
		DisplayName:        def.DisplayName,
		Description:        def.Description,
		LengthKm:           def.LengthKm,
		FreeFlowSpeedKmh:   def.FreeFlowSpeedKmh,
		FreeFlowSeconds:    freeFlowSec,
		EstimatedSeconds:   estimatedSec,
		DelaySeconds:       delaySec,
		WorstSeverity:      status.WorstSeverity,
		WorstFloodSeverity: worstFloodSev,
		MaxDepthCm:         maxDepthCm,
		MaxHazardLevel:     maxHazardLvl,
		PassabilityVerdict: passability,
		StatusColor:        statusColor,
		TotalCameras:       status.TotalCameras,
		VerifiedCameras:    status.VerifiedCameras,
		CoveragePct:        status.CoveragePct,
		ChokepointCount:    chokepointCount,
		Vehicle:            vehicle,
	}
}

// GetCorridorsOverview returns the operational route summary for all registered arterial corridors.
func (s *Service) GetCorridorsOverview(vehicle ...VehicleType) []CorridorRouteSummary {
	vType := VehicleScooter
	if len(vehicle) > 0 && vehicle[0] != "" {
		vType = vehicle[0]
	}

	orderedKeys := []string{
		"vo_van_kiet",
		"mai_chi_tho",
		"dien_bien_phu",
		"nguyen_huu_canh",
		"cong_hoa",
		"pham_van_dong",
		"nam_ky_khoi_nghia",
		"cach_mang_thang_tam",
		"nguyen_van_linh",
		"quoc_lo_1",
	}

	summaries := make([]CorridorRouteSummary, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		def, ok := ArterialCorridors[key]
		if !ok {
			continue
		}
		status, hasStatus := s.GetStatus(key)
		if !hasStatus || status == nil {
			freeFlowSec := int(math.Round((def.LengthKm / def.FreeFlowSpeedKmh) * 3600))
			summaries = append(summaries, CorridorRouteSummary{
				RoadKey:            def.Key,
				DisplayName:        def.DisplayName,
				Description:        def.Description,
				LengthKm:           def.LengthKm,
				FreeFlowSpeedKmh:   def.FreeFlowSpeedKmh,
				FreeFlowSeconds:    freeFlowSec,
				EstimatedSeconds:   freeFlowSec,
				DelaySeconds:       0,
				WorstSeverity:      "UNKNOWN",
				WorstFloodSeverity: "UNKNOWN",
				PassabilityVerdict: PassabilityUnknown,
				StatusColor:        "#71717a",
				Vehicle:            vType,
			})
			continue
		}
		summary := s.calculateRouteSummary(&def, status, vType)
		summaries = append(summaries, *summary)
	}

	return summaries
}

// CorridorDetourPairs defines canonical arterial detour pairings in Ho Chi Minh City.
var CorridorDetourPairs = map[string][]string{
	"nguyen_huu_canh":     {"dien_bien_phu"},
	"dien_bien_phu":       {"nguyen_huu_canh", "pham_van_dong"},
	"cach_mang_thang_tam": {"nam_ky_khoi_nghia"},
	"nam_ky_khoi_nghia":   {"cach_mang_thang_tam"},
	"cong_hoa":            {"pham_van_dong", "cach_mang_thang_tam"},
	"pham_van_dong":       {"cong_hoa", "dien_bien_phu"},
	"vo_van_kiet":         {"nguyen_van_linh"},
	"nguyen_van_linh":     {"vo_van_kiet"},
	"mai_chi_tho":         {"dien_bien_phu"},
}

// passabilityRank maps verdict to integer severity rank: IMPASSABLE (3) > CAUTION (2) > UNKNOWN (1) > PASSABLE (0).
func passabilityRank(v PassabilityVerdict) int {
	switch v {
	case PassabilityImpassable:
		return 3
	case PassabilityCaution:
		return 2
	case PassabilityUnknown:
		return 1
	default:
		return 0
	}
}

// EvaluateCommute calculates aggregated trip hazard, duration, delay, worst bottleneck, and detour options for a set of corridors.
func (s *Service) EvaluateCommute(corridorKeys []string, vehicle VehicleType) *CommuteEvaluationResponse {
	if vehicle == "" {
		vehicle = VehicleScooter
	}
	vehicle = NormalizeVehicleType(string(vehicle))

	var (
		summaries        []CorridorRouteSummary
		totalDistanceKm  float64
		freeFlowSeconds  int
		estimatedSeconds int
		delaySeconds     int
		worstVerdict     = PassabilityPassable
		worstStatusColor = "#22c55e"
		worstBottleneck  *CommuteHazardBottleneck
		impassableKeys   []string
		seenKeys         = make(map[string]bool)
	)

	// Retrieve threshold for vehicle clearance
	thresholds, ok := VehicleThresholds[vehicle]
	if !ok {
		thresholds = VehicleThresholds[VehicleScooter]
	}

	for _, rawKey := range corridorKeys {
		normKey := normalizeCorridorKey(rawKey)
		if normKey == "" || seenKeys[normKey] {
			continue
		}
		seenKeys[normKey] = true

		def, ok := ArterialCorridors[normKey]
		if !ok {
			continue
		}

		status, hasStatus := s.GetStatus(normKey)
		var summary *CorridorRouteSummary
		if !hasStatus || status == nil {
			ffSec := int(math.Round((def.LengthKm / def.FreeFlowSpeedKmh) * 3600))
			summary = &CorridorRouteSummary{
				RoadKey:            def.Key,
				DisplayName:        def.DisplayName,
				Description:        def.Description,
				LengthKm:           def.LengthKm,
				FreeFlowSpeedKmh:   def.FreeFlowSpeedKmh,
				FreeFlowSeconds:    ffSec,
				EstimatedSeconds:   ffSec,
				DelaySeconds:       0,
				WorstSeverity:      "UNKNOWN",
				WorstFloodSeverity: "UNKNOWN",
				PassabilityVerdict: PassabilityUnknown,
				StatusColor:        "#71717a",
				Vehicle:            vehicle,
			}
		} else {
			summary = s.calculateRouteSummary(&def, status, vehicle)

			// Scan individual cameras in this corridor to identify bottleneck
			for _, cam := range status.Cameras {
				if cam.Stale {
					continue
				}

				// Check if this camera is a more severe bottleneck than current worst
				isBetterCandidate := false
				if worstBottleneck == nil {
					isBetterCandidate = true
				} else {
					// 1. Water depth priority
					camDepth := 0.0
					if cam.FloodDepthCm != nil {
						camDepth = *cam.FloodDepthCm
					}
					worstDepth := 0.0
					if worstBottleneck.FloodDepthCm != nil {
						worstDepth = *worstBottleneck.FloodDepthCm
					}

					if camDepth > worstDepth {
						isBetterCandidate = true
					} else if camDepth == worstDepth {
						// 2. Flood severity rank
						if floodRank(cam.FloodSeverity) > floodRank(worstBottleneck.FloodSeverity) {
							isBetterCandidate = true
						} else if floodRank(cam.FloodSeverity) == floodRank(worstBottleneck.FloodSeverity) {
							// 3. Traffic severity rank
							if severityRank(cam.Severity) > severityRank(worstBottleneck.Severity) {
								isBetterCandidate = true
							}
						}
					}
				}

				if isBetterCandidate && (cam.FloodDepthCm != nil || cam.Severity == "CONGESTED" || cam.Severity == "HEAVY" || floodRank(cam.FloodSeverity) >= 2) {
					desc := ""
					if cam.FloodDepthCm != nil && *cam.FloodDepthCm >= thresholds.ImpassableDepthCm {
						desc = fmt.Sprintf("Đoạn ngập sâu nhất: %s: %.0f cm (Nguy cơ chết máy)", cam.Name, *cam.FloodDepthCm)
					} else if cam.FloodDepthCm != nil && *cam.FloodDepthCm >= thresholds.CautionDepthCm {
						desc = fmt.Sprintf("Đoạn ngập sâu nhất: %s: %.0f cm (Cảnh báo xe gầm thấp)", cam.Name, *cam.FloodDepthCm)
					} else if cam.FloodDepthCm != nil && *cam.FloodDepthCm > 0 {
						desc = fmt.Sprintf("Đoạn đọng nước: %s: %.0f cm", cam.Name, *cam.FloodDepthCm)
					} else if cam.Severity == "CONGESTED" || cam.Severity == "HEAVY" {
						desc = fmt.Sprintf("Ùn tắc nghiêm trọng tại: %s", cam.Name)
					}

					worstBottleneck = &CommuteHazardBottleneck{
						CameraID:         cam.CameraID,
						Name:             cam.Name,
						Severity:         cam.Severity,
						FloodSeverity:    cam.FloodSeverity,
						FloodDepthCm:     cam.FloodDepthCm,
						FloodHazardLevel: cam.FloodHazardLevel,
						CorridorKey:      def.Key,
						CorridorName:     def.DisplayName,
						Description:      desc,
					}
				}
			}
		}

		summaries = append(summaries, *summary)
		totalDistanceKm += summary.LengthKm
		freeFlowSeconds += summary.FreeFlowSeconds
		estimatedSeconds += summary.EstimatedSeconds
		delaySeconds += summary.DelaySeconds

		if passabilityRank(summary.PassabilityVerdict) > passabilityRank(worstVerdict) {
			worstVerdict = summary.PassabilityVerdict
			worstStatusColor = summary.StatusColor
		}

		if summary.PassabilityVerdict == PassabilityImpassable || summary.PassabilityVerdict == PassabilityCaution {
			impassableKeys = append(impassableKeys, def.Key)
		}
	}

	// Detour determination: if journey is Impassable or Caution, seek a passable alternative
	var detour *DetourSuggestion
	if len(impassableKeys) > 0 {
		for _, badKey := range impassableKeys {
			pairs, exists := CorridorDetourPairs[badKey]
			if !exists {
				continue
			}

			badDef, _ := ArterialCorridors[badKey]
			for _, candidateKey := range pairs {
				// Avoid suggesting a corridor that is already part of the user's route
				if seenKeys[candidateKey] {
					continue
				}

				candDef, ok := ArterialCorridors[candidateKey]
				if !ok {
					continue
				}

				candStatus, hasStatus := s.GetStatus(candidateKey)
				var candSummary *CorridorRouteSummary
				if !hasStatus || candStatus == nil {
					continue
				}
				candSummary = s.calculateRouteSummary(&candDef, candStatus, vehicle)

				// A viable detour must be PASSABLE, or strictly safer than the original corridor
				if candSummary.PassabilityVerdict == PassabilityPassable {
					detour = &DetourSuggestion{
						OriginalCorridorKey: badKey,
						DetourCorridorKey:   candDef.Key,
						DetourCorridorName:  candDef.DisplayName,
						PassabilityVerdict:  candSummary.PassabilityVerdict,
						MaxDepthCm:          candSummary.MaxDepthCm,
						DelaySeconds:        candSummary.DelaySeconds,
						Reason:              fmt.Sprintf("Tránh tuyến %s (%s); %s thông thoáng và an toàn.", badDef.DisplayName, badKey, candDef.DisplayName),
					}
					break
				}
			}
			if detour != nil {
				break
			}
		}
	}

	return &CommuteEvaluationResponse{
		PassabilityVerdict: worstVerdict,
		StatusColor:        worstStatusColor,
		TotalDistanceKm:    math.Round(totalDistanceKm*10) / 10,
		FreeFlowSeconds:    freeFlowSeconds,
		EstimatedSeconds:   estimatedSeconds,
		DelaySeconds:       delaySeconds,
		WorstBottleneck:    worstBottleneck,
		Detour:             detour,
		Corridors:          summaries,
		Vehicle:            vehicle,
	}
}

func floodRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FLOODED_DEEP":
		return 4
	case "FLOODED_SHALLOW":
		return 3
	case "WET":
		return 2
	case "DRY":
		return 1
	default:
		return 0
	}
}

func severityToColor(trafficSev, floodSev string, depthCm *float64) string {
	if depthCm != nil && *depthCm > 20.0 {
		return "#ef4444"
	}
	if strings.ToUpper(floodSev) == "FLOODED_DEEP" {
		return "#ef4444"
	}
	if (depthCm != nil && *depthCm >= 10.0) || strings.ToUpper(trafficSev) == "CONGESTED" || strings.ToUpper(trafficSev) == "HEAVY" {
		return "#eab308"
	}
	if strings.ToUpper(trafficSev) == "CLEAR" {
		return "#22c55e"
	}
	return "#71717a"
}
