package camera_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
)

func TestValidateCameraRoadway_Valid(t *testing.T) {
	roadway := camera.CameraRoadway{
		CameraID:          "cam-001",
		DirectionCoverage: camera.DirectionCoverageDual,
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "cam-001-dir-a",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.50,
			},
			{
				RegionID:      "cam-001-dir-b",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.50,
			},
		},
	}

	if err := camera.ValidateCameraRoadway(roadway); err != nil {
		t.Fatalf("expected valid roadway, got err: %v", err)
	}
}

func TestValidateCameraRoadway_InconsistentCapacityShare(t *testing.T) {
	// Case 1: sum < 0.99
	badLow := camera.CameraRoadway{
		CameraID: "cam-low",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "r1",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.40,
			},
			{
				RegionID:      "r2",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.40,
			},
		},
	}
	if err := camera.ValidateCameraRoadway(badLow); err == nil {
		t.Errorf("expected error for capacity share sum 0.80, got nil")
	}

	// Case 2: sum > 1.01
	badHigh := camera.CameraRoadway{
		CameraID: "cam-high",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "r1",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.60,
			},
			{
				RegionID:      "r2",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.50,
			},
		},
	}
	if err := camera.ValidateCameraRoadway(badHigh); err == nil {
		t.Errorf("expected error for capacity share sum 1.10, got nil")
	}
}

func TestValidateCameraRoadway_InvalidDirection(t *testing.T) {
	badDir := camera.CameraRoadway{
		CameraID: "cam-dir",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "r1",
				Direction:     "x", // Invalid: only 'a' or 'b'
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 1.0,
			},
		},
	}
	if err := camera.ValidateCameraRoadway(badDir); err == nil {
		t.Errorf("expected error for invalid direction 'x', got nil")
	}
}

func TestValidateCameraRoadway_InvalidPolygon(t *testing.T) {
	// Fewer than 3 vertices
	badPoly := camera.CameraRoadway{
		CameraID: "cam-poly",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "r1",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}},
				CapacityShare: 1.0,
			},
		},
	}
	if err := camera.ValidateCameraRoadway(badPoly); err == nil {
		t.Errorf("expected error for < 3 vertices, got nil")
	}

	// Out of bounds coordinates
	badBounds := camera.CameraRoadway{
		CameraID: "cam-bounds",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "r1",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {1.5, 0.9}, {0.3, 0.4}},
				CapacityShare: 1.0,
			},
		},
	}
	if err := camera.ValidateCameraRoadway(badBounds); err == nil {
		t.Errorf("expected error for vertex out of [0, 1] range, got nil")
	}
}

func TestLoadRoadwayRegistry_RejectionAndDegradation(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Valid camera file
	validRoadway := camera.CameraRoadway{
		CameraID:          "cam-valid",
		DirectionCoverage: camera.DirectionCoverageDual,
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "cam-valid-a",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.50,
			},
			{
				RegionID:      "cam-valid-b",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.50,
			},
		},
	}
	validBytes, _ := json.Marshal(validRoadway)
	if err := os.WriteFile(filepath.Join(tmpDir, "cam-valid.json"), validBytes, 0o644); err != nil {
		t.Fatalf("failed to write valid file: %v", err)
	}

	// 2. Invalid camera file (capacity_share sum = 0.70 < 0.99)
	invalidRoadway := camera.CameraRoadway{
		CameraID: "cam-bad",
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "cam-bad-a",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.35,
			},
			{
				RegionID:      "cam-bad-b",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.35,
			},
		},
	}
	invalidBytes, _ := json.Marshal(invalidRoadway)
	if err := os.WriteFile(filepath.Join(tmpDir, "cam-bad.json"), invalidBytes, 0o644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}

	// Load registry
	reg, err := camera.LoadRoadwayRegistry(tmpDir)
	if err != nil {
		t.Fatalf("LoadRoadwayRegistry failed: %v", err)
	}

	// Valid camera must be present
	rw, ok := reg.GetCameraRoadway("cam-valid")
	if !ok || rw == nil {
		t.Fatalf("expected cam-valid to be loaded")
	}
	if len(rw.Regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(rw.Regions))
	}

	// Invalid camera must be rejected and absent from registry
	if _, ok := reg.GetCameraRoadway("cam-bad"); ok {
		t.Errorf("expected cam-bad to be rejected due to bad capacity share")
	}
}

func TestCatalog_RoadwayRegistryIntegration(t *testing.T) {
	memReg := camera.NewMemoryRoadwayRegistry()
	_ = memReg.Register(camera.CameraRoadway{
		CameraID:          "cam-dual",
		DirectionCoverage: camera.DirectionCoverageDual,
		Regions: []camera.RoadwayRegion{
			{
				RegionID:      "cam-dual-a",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.50,
			},
			{
				RegionID:      "cam-dual-b",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.50,
			},
		},
	})

	cams := []camera.Camera{
		{ID: "cam-dual", DisplayName: "Dual Corridors"},
		{ID: "cam-unregistered", DisplayName: "Unregistered Single Flow"},
	}

	cat := camera.NewCatalogWithRegistry(cams, nil, memReg)

	// Registered camera has dual coverage and 2 regions
	c1, ok := cat.ByID("cam-dual")
	if !ok {
		t.Fatalf("cam-dual not found")
	}
	if c1.DirectionCoverage != camera.DirectionCoverageDual {
		t.Errorf("expected DirectionCoverage %q, got %q", camera.DirectionCoverageDual, c1.DirectionCoverage)
	}
	if len(c1.Regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(c1.Regions))
	}

	// Unregistered camera degrades cleanly to direction_coverage: "none"
	c2, ok := cat.ByID("cam-unregistered")
	if !ok {
		t.Fatalf("cam-unregistered not found")
	}
	if c2.DirectionCoverage != camera.DirectionCoverageNone {
		t.Errorf("expected DirectionCoverage %q, got %q", camera.DirectionCoverageNone, c2.DirectionCoverage)
	}
	if len(c2.Regions) != 0 {
		t.Errorf("expected 0 regions, got %d", len(c2.Regions))
	}
}

func TestLoadRoadwayRegistryFromDir_HotSetCameras(t *testing.T) {
	candidates := []string{
		"../../../../data/roadway_regions",
		"../../../data/roadway_regions",
		"../../data/roadway_regions",
		"data/roadway_regions",
	}
	var dirPath string
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			dirPath = c
			break
		}
	}
	if dirPath == "" {
		t.Fatalf("could not locate data/roadway_regions directory")
	}

	reg, err := camera.LoadRoadwayRegistryFromDir(dirPath)
	if err != nil {
		t.Fatalf("LoadRoadwayRegistryFromDir failed: %v", err)
	}

	all := reg.All()
	if len(all) != 59 {
		t.Fatalf("expected 59 hot set cameras loaded, got %d", len(all))
	}

	// Assert zero cameras fall back to direction_coverage: "none"
	for camID, rw := range all {
		if rw.DirectionCoverage == camera.DirectionCoverageNone || rw.DirectionCoverage == "" {
			t.Errorf("camera %s fell back to direction_coverage none or empty", camID)
		}
		if rw.DirectionCoverage != camera.DirectionCoverageDual {
			t.Errorf("camera %s expected dual coverage, got %s", camID, rw.DirectionCoverage)
		}
		if len(rw.Regions) != 2 {
			t.Errorf("camera %s expected 2 regions, got %d", camID, len(rw.Regions))
		}
	}

	// Query critical arterial corridors: cong_hoa and truong_chinh
	criticalCams := []string{
		"586e1f18f9fab7001111b0a5", // Cong Hoa - Truong Chinh junction
		"5874769eb807da0011e33cec", // Cong Hoa - Ut Tich
		"587476e3b807da0011e33cee", // Cong Hoa - Ut Tich
	}
	for _, camID := range criticalCams {
		rw, ok := reg.GetCameraRoadway(camID)
		if !ok || rw == nil {
			t.Errorf("critical arterial camera %s not found in registry", camID)
			continue
		}
		if rw.DirectionCoverage != camera.DirectionCoverageDual {
			t.Errorf("camera %s expected dual direction coverage, got %s", camID, rw.DirectionCoverage)
		}
		if len(rw.Regions) != 2 {
			t.Errorf("camera %s expected 2 regions, got %d", camID, len(rw.Regions))
		}
		dirMap := make(map[string]camera.RoadwayRegion)
		for _, regItem := range rw.Regions {
			dirMap[regItem.Direction] = regItem
			if len(regItem.Polygon) < 3 {
				t.Errorf("camera %s region %s has < 3 polygon points", camID, regItem.RegionID)
			}
		}
		if _, hasA := dirMap["a"]; !hasA {
			t.Errorf("camera %s missing direction 'a'", camID)
		}
		if _, hasB := dirMap["b"]; !hasB {
			t.Errorf("camera %s missing direction 'b'", camID)
		}
	}
}
