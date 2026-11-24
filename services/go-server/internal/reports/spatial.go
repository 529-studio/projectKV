package reports

import (
	"math"
)

const (
	// EarthRadiusMeters is the approximate mean radius of the Earth in meters.
	EarthRadiusMeters = 6371000.0
	// DefaultCorroborationRadiusMeters is the maximum distance (300m) to associate a camera with a citizen report.
	DefaultCorroborationRadiusMeters = 300.0
)

// HaversineDistanceMeters calculates the great-circle distance between two GPS coordinates in meters.
func HaversineDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	lat1Rad := lat1 * (math.Pi / 180.0)
	lat2Rad := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return EarthRadiusMeters * c
}
