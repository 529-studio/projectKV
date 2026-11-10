package camera

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Direction coverage classifications for a camera corridor.
const (
	DirectionCoverageNone    = "none"
	DirectionCoveragePartial = "partial"
	DirectionCoverageDual    = "dual"
)

// RoadwayRegion represents a single directional carriageway polygon and its
// allocated capacity share (ADR-012 D1).
type RoadwayRegion struct {
	RegionID      string      `json:"region_id"`
	Direction     string      `json:"direction"` // "a" (approaching/near) or "b" (receding/far)
	Polygon       [][]float64 `json:"polygon"`   // Normalized [[x, y], ...] in [0, 1] range
	CapacityShare float64     `json:"capacity_share"`
}

// CameraRoadway represents the runtime geometry configuration for a camera.
type CameraRoadway struct {
	CameraID          string          `json:"camera_id"`
	DirectionCoverage string          `json:"direction_coverage"`
	Regions           []RoadwayRegion `json:"regions"`
	SourceLabelIDs    []string        `json:"source_label_ids,omitempty"`
}

// RoadwayRegistry defines the contract for looking up directional roadway geometry.
type RoadwayRegistry interface {
	GetCameraRoadway(cameraID string) (*CameraRoadway, bool)
	All() map[string]CameraRoadway
}

// MemoryRoadwayRegistry is a concurrent-safe in-memory RoadwayRegistry.
type MemoryRoadwayRegistry struct {
	mu       sync.RWMutex
	roadways map[string]CameraRoadway
}

var _ RoadwayRegistry = (*MemoryRoadwayRegistry)(nil)

// NewMemoryRoadwayRegistry creates an empty MemoryRoadwayRegistry.
func NewMemoryRoadwayRegistry() *MemoryRoadwayRegistry {
	return &MemoryRoadwayRegistry{
		roadways: make(map[string]CameraRoadway),
	}
}

// GetCameraRoadway returns the registered roadway geometry for cameraID.
func (r *MemoryRoadwayRegistry) GetCameraRoadway(cameraID string) (*CameraRoadway, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	roadway, ok := r.roadways[cameraID]
	if !ok {
		return nil, false
	}
	// Return a copy
	cp := roadway
	cp.Regions = make([]RoadwayRegion, len(roadway.Regions))
	copy(cp.Regions, roadway.Regions)
	return &cp, true
}

// All returns a shallow copy map of all registered roadways.
func (r *MemoryRoadwayRegistry) All() map[string]CameraRoadway {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make(map[string]CameraRoadway, len(r.roadways))
	for k, v := range r.roadways {
		res[k] = v
	}
	return res
}

// Register adds or replaces a roadway configuration after validating capacity share.
func (r *MemoryRoadwayRegistry) Register(roadway CameraRoadway) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if err := ValidateCameraRoadway(roadway); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roadways[roadway.CameraID] = roadway
	return nil
}

// ValidateCameraRoadway validates that a CameraRoadway strictly complies with ADR-012 D1:
// - CameraID must be non-empty.
// - Regions must be non-empty.
// - Each region must have a valid region_id, direction in {"a", "b"}, valid polygon (>= 3 points).
// - Each capacity_share must be > 0.
// - The sum of capacity_share across all regions must equal 1.0 +- 0.01.
func ValidateCameraRoadway(roadway CameraRoadway) error {
	if strings.TrimSpace(roadway.CameraID) == "" {
		return fmt.Errorf("camera_id is required")
	}
	if len(roadway.Regions) == 0 {
		return fmt.Errorf("camera %s has no regions", roadway.CameraID)
	}

	var totalShare float64
	seenDirections := make(map[string]bool)
	seenRegionIDs := make(map[string]bool)

	for i, reg := range roadway.Regions {
		regID := strings.TrimSpace(reg.RegionID)
		if regID == "" {
			return fmt.Errorf("camera %s region[%d] missing region_id", roadway.CameraID, i)
		}
		if seenRegionIDs[regID] {
			return fmt.Errorf("camera %s duplicate region_id %q", roadway.CameraID, regID)
		}
		seenRegionIDs[regID] = true

		dir := strings.ToLower(strings.TrimSpace(reg.Direction))
		if dir != "a" && dir != "b" {
			return fmt.Errorf("camera %s region[%d] invalid direction %q (must be 'a' or 'b')", roadway.CameraID, i, reg.Direction)
		}
		if seenDirections[dir] {
			return fmt.Errorf("camera %s duplicate direction %q", roadway.CameraID, dir)
		}
		seenDirections[dir] = true

		if len(reg.Polygon) < 3 {
			return fmt.Errorf("camera %s region %s polygon must have at least 3 points, got %d", roadway.CameraID, regID, len(reg.Polygon))
		}
		for ptIdx, pt := range reg.Polygon {
			if len(pt) != 2 {
				return fmt.Errorf("camera %s region %s point[%d] must be [x, y]", roadway.CameraID, regID, ptIdx)
			}
			x, y := pt[0], pt[1]
			if x < 0.0 || x > 1.0 || y < 0.0 || y > 1.0 {
				return fmt.Errorf("camera %s region %s point[%d] out of [0, 1] bounds: [%f, %f]", roadway.CameraID, regID, ptIdx, x, y)
			}
		}

		if reg.CapacityShare <= 0.0 || reg.CapacityShare > 1.0 {
			return fmt.Errorf("camera %s region %s invalid capacity_share: %f (must be > 0.0 and <= 1.0)", roadway.CameraID, regID, reg.CapacityShare)
		}
		totalShare += reg.CapacityShare
	}

	// Constraint: capacity_share must sum to 1.0 +- 0.01
	if math.Abs(totalShare-1.0) > 0.01 {
		return fmt.Errorf("camera %s capacity_share sum %.4f violates constraint (must sum to 1.0 +- 0.01)", roadway.CameraID, totalShare)
	}

	return nil
}

// LoadRoadwayRegistry reads all JSON files in dirPath and returns a populated MemoryRoadwayRegistry.
// If dirPath does not exist, an empty registry is returned without error.
// If any file has an invalid capacity_share (< 0.99 or > 1.01), it is rejected and logged as a warning,
// cleanly degrading that camera to direction_coverage: "none" (ADR-012 D1).
func LoadRoadwayRegistry(dirPath string) (*MemoryRoadwayRegistry, error) {
	registry := NewMemoryRoadwayRegistry()
	if strings.TrimSpace(dirPath) == "" {
		return registry, nil
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("camera: roadway registry directory does not exist, skipping", "path", dirPath)
			return registry, nil
		}
		return nil, fmt.Errorf("stat roadway registry dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("roadway registry path is not a directory: %s", dirPath)
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("read roadway registry dir: %w", err)
	}

	validCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("camera: failed to read roadway registry file", "file", filePath, "err", err)
			continue
		}

		var roadway CameraRoadway
		if err := json.Unmarshal(data, &roadway); err != nil {
			slog.Warn("camera: failed to unmarshal roadway registry file", "file", filePath, "err", err)
			continue
		}

		// Ensure CameraID defaults to file stem if empty
		fileStem := strings.TrimSuffix(entry.Name(), ".json")
		if roadway.CameraID == "" {
			roadway.CameraID = fileStem
		} else if roadway.CameraID != fileStem {
			slog.Warn("camera: roadway registry filename does not match camera_id", "file", entry.Name(), "camera_id", roadway.CameraID)
		}

		// Set default coverage if not specified
		if roadway.DirectionCoverage == "" {
			if len(roadway.Regions) >= 2 {
				roadway.DirectionCoverage = DirectionCoverageDual
			} else if len(roadway.Regions) == 1 {
				roadway.DirectionCoverage = DirectionCoveragePartial
			} else {
				roadway.DirectionCoverage = DirectionCoverageNone
			}
		}

		if err := registry.Register(roadway); err != nil {
			slog.Warn("camera: rejected invalid roadway registry entry", "camera_id", roadway.CameraID, "file", filePath, "err", err)
			continue
		}
		validCount++
	}

	slog.Info("camera: loaded roadway registry", "path", dirPath, "valid_cameras", validCount)
	return registry, nil
}

// LoadRoadwayRegistryFromDir loads directional roadway geometry for all cameras from dirPath.
// It is an alias for LoadRoadwayRegistry adhering to ADR-012 D1.
func LoadRoadwayRegistryFromDir(dirPath string) (*MemoryRoadwayRegistry, error) {
	return LoadRoadwayRegistry(dirPath)
}
