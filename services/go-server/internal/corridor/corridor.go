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
	CameraID      string     `json:"camera_id"`
	Name          string     `json:"name,omitempty"`
	Severity      string     `json:"severity"` // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
	FloodSeverity string     `json:"flood_severity,omitempty"`
	Confidence    float64    `json:"confidence,omitempty"`
	Source        string     `json:"source,omitempty"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
	Stale         bool       `json:"stale"`
}

// CorridorStatus represents the aggregated health and traffic condition of a named road corridor.
type CorridorStatus struct {
	RoadKey               string          `json:"road_key"`
	WorstSeverity         string          `json:"worst_severity"` // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
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
				CameraID: cam.ID,
				Name:     camName,
				Severity: "UNKNOWN",
				Stale:    false,
			})
			continue
		}

		// Filter mock verdicts in non-dev environment (ADR-004 §D10 / Provenance safety rule)
		if strings.ToLower(v.Source) == "mock" && strings.ToLower(s.appEnv) != "dev" {
			cameraVerdicts = append(cameraVerdicts, CameraVerdict{
				CameraID: cam.ID,
				Name:     camName,
				Severity: "UNKNOWN",
				Stale:    false,
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
			CameraID:      cam.ID,
			Name:          camName,
			Severity:      sev,
			FloodSeverity: v.FloodSeverity,
			Confidence:    v.Confidence,
			Source:        v.Source,
			ObservedAt:    obsPtr,
			Stale:         isStale,
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
	}

	return &CorridorStatus{
		RoadKey:               target,
		WorstSeverity:         worstSeverity,
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
