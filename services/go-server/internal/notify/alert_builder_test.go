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
	if alert.District != "Khu vực Nội thành TP.HCM" {
		t.Errorf("District: got %q", alert.District)
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
