package notify

import (
	"testing"
	"time"
)

func TestBuildFloodAlert_FullCamera(t *testing.T) {
	obs := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	alert := BuildFloodAlert(
		"586e2995f9fab7001111b0b5",
		"Ung Van Khiem",
		"Khu vực Nội thành TP.HCM",
		"Thạnh Mỹ Tây",
		"https://example.com/snap.jpg",
		35,
		"FLOODED_DEEP",
		obs,
	)
	if alert.CameraID != "586e2995f9fab7001111b0b5" {
		t.Errorf("CameraID: got %q", alert.CameraID)
	}
	if alert.CameraName != "Ung Van Khiem" {
		t.Errorf("CameraName: got %q", alert.CameraName)
	}
	if alert.Zone != "Khu vực Nội thành TP.HCM" {
		t.Errorf("Zone: got %q", alert.Zone)
	}
	if alert.Ward != "Thạnh Mỹ Tây" {
		t.Errorf("Ward: got %q", alert.Ward)
	}
	if alert.SnapshotURL != "https://example.com/snap.jpg" {
		t.Errorf("SnapshotURL: got %q", alert.SnapshotURL)
	}
	if alert.TypicalDepthCm != 35 {
		t.Errorf("TypicalDepthCm: got %d", alert.TypicalDepthCm)
	}
	if alert.FloodSeverity != "FLOODED_DEEP" {
		t.Errorf("FloodSeverity: got %q", alert.FloodSeverity)
	}
	if alert.ObservedAt != obs {
		t.Errorf("ObservedAt: got %v", alert.ObservedAt)
	}
}

func TestBuildFloodAlert_NilCamera_FallsBackToCameraID(t *testing.T) {
	alert := BuildFloodAlert("cam-unknown", "", "", "", "", 0, "WET", time.Time{})
	if alert.CameraName != "" {
		t.Errorf("expected empty CameraName, got %q", alert.CameraName)
	}
	if alert.CameraID != "cam-unknown" {
		t.Errorf("CameraID: got %q", alert.CameraID)
	}
	if alert.ObservedAt.IsZero() {
		t.Error("expected ObservedAt to be non-zero after fallback")
	}
}

func TestBuildFloodAlert_ZeroObservedAt_FallsBackToNow(t *testing.T) {
	before := time.Now()
	alert := BuildFloodAlert("cam-x", "Test", "D1", "Ward", "", 0, "FLOODED_SHALLOW", time.Time{})
	after := time.Now()
	if alert.ObservedAt.Before(before) || alert.ObservedAt.After(after) {
		t.Errorf("ObservedAt fallback outside expected window: %v", alert.ObservedAt)
	}
}

func TestBuildCorridorAlert(t *testing.T) {
	depth := 35.0
	obs := time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC)
	alert := BuildCorridorAlert(
		"vo_van_kiet",
		"Đại lộ Võ Văn Kiệt",
		"IMPASSABLE",
		"FLOODED_DEEP",
		&depth,
		"Cầu Rạch Chiếc",
		25,
		obs,
		"https://warded.529studio.site/roads/vo_van_kiet",
	)

	if alert.RoadKey != "vo_van_kiet" {
		t.Errorf("RoadKey: got %q, want vo_van_kiet", alert.RoadKey)
	}
	if alert.RoadName != "Đại lộ Võ Văn Kiệt" {
		t.Errorf("RoadName: got %q", alert.RoadName)
	}
	if alert.Passability != "IMPASSABLE" {
		t.Errorf("Passability: got %q", alert.Passability)
	}
	if alert.MaxDepthCm == nil || *alert.MaxDepthCm != 35.0 {
		t.Errorf("MaxDepthCm: got %v, want 35.0", alert.MaxDepthCm)
	}
	if alert.PeakLocation != "Cầu Rạch Chiếc" {
		t.Errorf("PeakLocation: got %q", alert.PeakLocation)
	}
	if alert.EstimatedDelayMin != 25 {
		t.Errorf("EstimatedDelayMin: got %d, want 25", alert.EstimatedDelayMin)
	}
	if alert.ObservedAt != obs {
		t.Errorf("ObservedAt: got %v", alert.ObservedAt)
	}

	// Fallback test
	fallback := BuildCorridorAlert("cong_hoa", "", "", "", nil, "", 0, time.Time{}, "")
	if fallback.RoadName != "cong_hoa" {
		t.Errorf("RoadName fallback: got %q, want cong_hoa", fallback.RoadName)
	}
	if fallback.Passability != "CAUTION" {
		t.Errorf("Passability fallback: got %q, want CAUTION", fallback.Passability)
	}
	if fallback.ObservedAt.IsZero() {
		t.Errorf("ObservedAt fallback should not be zero")
	}
}
