// alert_builder.go — pure helper; no I/O, no imports from internal/.
package notify

import (
	"time"
)

// BuildFloodAlert assembles a FloodAlert from already-resolved camera fields and
// chokepoint data. camName, zone, ward, snapshotURL may be empty if camera metadata
// is absent. typicalDepthCm may be 0. If observedAt is zero, it falls back to time.Now().
func BuildFloodAlert(
	cameraID string,
	camName string,
	zone string,
	ward string,
	snapshotURL string,
	typicalDepthCm int,
	severity string,
	observedAt time.Time,
) FloodAlert {
	obs := observedAt
	if obs.IsZero() {
		obs = time.Now()
	}

	return FloodAlert{
		CameraID:       cameraID,
		CameraName:     camName,
		Zone:           zone,
		Ward:           ward,
		FloodSeverity:  severity,
		TypicalDepthCm: typicalDepthCm,
		ObservedAt:     obs,
		SnapshotURL:    snapshotURL,
	}
}

// BuildCorridorAlert assembles a CorridorAlert payload with defaults.
// roadName falls back to roadKey if empty. passability falls back to "CAUTION" if empty.
// If observedAt is zero, it falls back to time.Now().
func BuildCorridorAlert(
	roadKey string,
	roadName string,
	passability string,
	worstFloodSeverity string,
	maxDepthCm *float64,
	peakLocation string,
	estimatedDelayMin int,
	observedAt time.Time,
	routeWebURL string,
) CorridorAlert {
	obs := observedAt
	if obs.IsZero() {
		obs = time.Now()
	}
	if roadName == "" {
		roadName = roadKey
	}
	if passability == "" {
		passability = "CAUTION"
	}
	return CorridorAlert{
		RoadKey:            roadKey,
		RoadName:           roadName,
		Passability:        passability,
		WorstFloodSeverity: worstFloodSeverity,
		MaxDepthCm:         maxDepthCm,
		PeakLocation:       peakLocation,
		EstimatedDelayMin:  estimatedDelayMin,
		ObservedAt:         obs,
		RouteWebURL:        routeWebURL,
	}
}
