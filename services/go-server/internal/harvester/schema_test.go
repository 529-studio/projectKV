package harvester

import (
	"testing"
)

func TestGroundTruthManifest_PolygonDirectionValidation(t *testing.T) {
	baseManifest := func() GroundTruthManifest {
		return GroundTruthManifest{
			FrameID:     "cam_001_123456",
			CameraID:    "cam_001",
			ImagePath:   "proofs/2026-10-12/cam_001/123456.jpg",
			Annotator:   "auto_harvester",
			AnnotatedAt: "2026-10-12T07:00:00Z",
			Labels: LabelsPayload{
				Traffic: "CLEAR",
				Flood:   "NONE",
			},
		}
	}

	// 1. Valid directional roadway annotations (direction "a" and "b")
	m := baseManifest()
	m.Annotations = []ManifestPolygon{
		{
			Class:     "roadway",
			Direction: "a",
			Polygon:   [][]float64{{0.1, 0.9}, {0.45, 0.9}, {0.3, 0.5}},
		},
		{
			Class:     "roadway",
			Direction: "b",
			Polygon:   [][]float64{{0.55, 0.9}, {0.9, 0.9}, {0.7, 0.5}},
		},
		{
			Class:   "water_puddle",
			Polygon: [][]float64{{0.2, 0.8}, {0.4, 0.8}, {0.3, 0.7}},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}

	// 2. Direction on non-roadway class rejected
	mBadClass := baseManifest()
	mBadClass.Annotations = []ManifestPolygon{
		{
			Class:     "car",
			Direction: "a",
			Polygon:   [][]float64{{0.1, 0.1}, {0.2, 0.1}, {0.2, 0.2}},
		},
	}
	if err := mBadClass.Validate(); err == nil {
		t.Fatal("expected error when direction is set on car annotation, got nil")
	}

	// 3. Unknown direction value rejected
	mBadDir := baseManifest()
	mBadDir.Annotations = []ManifestPolygon{
		{
			Class:     "roadway",
			Direction: "c",
			Polygon:   [][]float64{{0.1, 0.1}, {0.2, 0.1}, {0.2, 0.2}},
		},
	}
	if err := mBadDir.Validate(); err == nil {
		t.Fatal("expected error when direction is 'c', got nil")
	}
}
