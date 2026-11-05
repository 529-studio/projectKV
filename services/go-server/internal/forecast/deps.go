package forecast

import (
	"context"
	"time"
)

// Observation represents one historical traffic observation for a camera.
// Kept as a pure value object within forecast to preserve leaf package boundaries.
type Observation struct {
	CameraID        string
	PCUTotal        float64
	SaturationIndex float64
	Severity        string
	Confidence      float64
	Source          string
	ObservedAt      time.Time
}

// HistoryReader defines the query capability required from the history repository
// per Dependency Inversion Principle (DIP).
type HistoryReader interface {
	GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]Observation, error)
	GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]Observation, error)
}

// CameraInfo holds camera metadata needed by the forecast service.
type CameraInfo struct {
	ID           string
	RoadKey      string
	RoadKeys     []string
	BaseCapacity float64
}

// CameraCatalog defines camera existence validation and metadata retrieval.
type CameraCatalog interface {
	GetCamera(id string) (CameraInfo, bool)
}
