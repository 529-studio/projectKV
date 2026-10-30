// alert_builder.go — pure helper; no I/O, no imports from internal/.
package notify

import (
	"time"
)

// BuildFloodAlert assembles a FloodAlert from already-resolved camera fields and
// chokepoint data. camName, district, ward, snapshotURL may be empty if camera metadata
// is absent. typicalDepthCm may be 0. If observedAt is zero, it falls back to time.Now().
func BuildFloodAlert(
	cameraID string,
	camName string,
	district string,
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
		District:       district,
		Ward:           ward,
		FloodSeverity:  severity,
		TypicalDepthCm: typicalDepthCm,
		ObservedAt:     obs,
		SnapshotURL:    snapshotURL,
	}
}
