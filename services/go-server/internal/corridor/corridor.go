// Package corridor implements traffic corridor status aggregation described in
// ADR-004 §D1 (2a Corridor status): status of a named road from its cameras.
package corridor

import (
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

// Service provides corridor status and road inventory operations.
type Service struct {
	cameras  CameraProvider
	verdicts VerdictProvider
	appEnv   string
	staleTTL time.Duration
}

// NewService constructs a Service with injected dependencies.
func NewService(cameras CameraProvider, verdicts VerdictProvider, appEnv string, staleTTL time.Duration) *Service {
	if staleTTL <= 0 {
		staleTTL = 180 * time.Second
	}
	return &Service{
		cameras:  cameras,
		verdicts: verdicts,
		appEnv:   appEnv,
		staleTTL: staleTTL,
	}
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
		RoadKey:               target,
		WorstSeverity:         worstSeverity,
		WorstFloodSeverity:    worstFloodSev,
		MaxDepthCm:            maxDepthCm,
		CoveragePct:           coveragePct,
		OldestObservationAgeS: maxAgeSeconds,
		TotalCameras:          total,
		VerifiedCameras:       verifiedCount,
		Cameras:               cameraVerdicts,
	}, true
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
func (s *Service) GetRoute(roadKey string) (*GeoJSONFeatureCollection, *CorridorRouteSummary, bool) {
	status, ok := s.GetStatus(roadKey)
	if !ok || status == nil {
		return nil, nil, false
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

	summary := s.calculateRouteSummary(def, status)

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

// calculateRouteSummary computes travel duration, flood delay penalty, and passability verdict.
func (s *Service) calculateRouteSummary(def *CorridorDefinition, status *CorridorStatus) *CorridorRouteSummary {
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

	if (maxDepthCm != nil && *maxDepthCm > 20.0) || (maxHazardLvl != nil && *maxHazardLvl >= 3) || worstFloodSev == "FLOODED_DEEP" {
		passability = PassabilityImpassable
		statusColor = "#ef4444" // Red: danger / engine drowning risk
	} else if (maxDepthCm != nil && *maxDepthCm >= 10.0) || (maxHazardLvl != nil && *maxHazardLvl >= 2) || status.WorstSeverity == "CONGESTED" || status.WorstSeverity == "HEAVY" || worstFloodSev == "FLOODED_SHALLOW" {
		passability = PassabilityCaution
		statusColor = "#eab308" // Yellow: caution / low scooter warning / congested
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
	}
}

// GetCorridorsOverview returns the operational route summary for all registered arterial corridors.
func (s *Service) GetCorridorsOverview() []CorridorRouteSummary {
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
			})
			continue
		}
		summary := s.calculateRouteSummary(&def, status)
		summaries = append(summaries, *summary)
	}

	return summaries
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
