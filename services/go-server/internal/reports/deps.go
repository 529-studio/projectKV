package reports

import (
	"time"
)

// CameraLocator defines spatial lookup for cameras near a given coordinate.
type CameraLocator interface {
	FindCamerasWithin(lat, lng float64, radiusMeters float64) []NearbyCamera
	GetCamera(id string) (NearbyCamera, bool)
}

// VerdictReader provides real-time CV verdict information for cameras.
type VerdictReader interface {
	GetLatestVerdict(cameraID string) (CameraVerdictInfo, bool)
}

// TideReader provides river tide elevations for hydrological surge correlation.
type TideReader interface {
	PredictTide(station string, t time.Time) float64
}

// Clock provides time for deterministic unit testing.
type Clock interface {
	Now() time.Time
}

// RealClock is the production wall-clock implementation.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}
