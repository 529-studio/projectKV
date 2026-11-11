package main

// The boot-time hydration path rebuilds `hotset.Verdict` values from SQLite before
// serving GET /api/traffic-status and GET /api/v1/cameras/hotset/status. Issue #401
// AC11 fails silently if that rebuild forgets the hazard columns: the fields reach
// SQLite and never reach the wire. This test pins the single shared mapping.

import (
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
)

func TestHotsetVerdictFromStore_CarriesPhysicalHazardFields(t *testing.T) {
	depth := 14.5
	level := 3
	persisted := store.Verdict{
		CameraID:             "cam_dbp_01",
		Severity:             "CLEAR",
		FloodSeverity:        hotset.SeverityFloodedDeep,
		Confidence:           0.78,
		Source:               "onnx",
		FloodDepthCm:         &depth,
		FloodHazardLevel:     &level,
		FloodHazardBasis:     "AVOIDANCE_INFERRED",
		FloodDepthConfidence: "UNMEASURED",
		FloodCalibrationRef:  "CURB_15CM",
	}

	got := hotsetVerdictFromStore(persisted)

	if got.FloodDepthCm == nil || *got.FloodDepthCm != depth {
		t.Errorf("FloodDepthCm = %v, want %v", got.FloodDepthCm, depth)
	}
	if got.FloodHazardLevel == nil || *got.FloodHazardLevel != level {
		t.Errorf("FloodHazardLevel = %v, want %v", got.FloodHazardLevel, level)
	}
	if got.FloodHazardBasis != "AVOIDANCE_INFERRED" {
		t.Errorf("FloodHazardBasis = %q, want AVOIDANCE_INFERRED", got.FloodHazardBasis)
	}
	if got.FloodDepthConfidence != "UNMEASURED" {
		t.Errorf("FloodDepthConfidence = %q, want UNMEASURED", got.FloodDepthConfidence)
	}
	if got.FloodCalibrationRef != "CURB_15CM" {
		t.Errorf("FloodCalibrationRef = %q, want CURB_15CM", got.FloodCalibrationRef)
	}
}

func TestHotsetVerdictFromStore_PreservesNilAndMeasuredZero(t *testing.T) {
	unmeasured := hotsetVerdictFromStore(store.Verdict{CameraID: "cam-a", Severity: "CLEAR"})
	if unmeasured.FloodDepthCm != nil || unmeasured.FloodHazardLevel != nil {
		t.Fatalf("unmeasured verdict carries hazard data: (%v, %v)", unmeasured.FloodDepthCm, unmeasured.FloodHazardLevel)
	}
	if unmeasured.FloodCalibrationRef != "" {
		t.Errorf("FloodCalibrationRef = %q, want empty for a pre-migration row", unmeasured.FloodCalibrationRef)
	}

	zero := 0.0
	level := 1
	measuredDry := hotsetVerdictFromStore(store.Verdict{
		CameraID:         "cam-b",
		Severity:         "CLEAR",
		FloodDepthCm:     &zero,
		FloodHazardLevel: &level,
	})
	if measuredDry.FloodDepthCm == nil || *measuredDry.FloodDepthCm != 0.0 {
		t.Fatalf("FloodDepthCm = %v, want a measured 0.0", measuredDry.FloodDepthCm)
	}
}
