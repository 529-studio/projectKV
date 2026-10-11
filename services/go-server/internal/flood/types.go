package flood

import (
	"context"
	"errors"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/weather"
)

// Canonical flood severity levels
const (
	SeverityDry            = "DRY"
	SeverityWet            = "WET"
	SeverityFloodedShallow = "FLOODED_SHALLOW"
	SeverityFloodedDeep    = "FLOODED_DEEP"
	SeverityUnknown        = "UNKNOWN"
)

// Coordinates represents geographic lat/long.
type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// FloodChokepoint represents an authentic municipal flood hotspot camera fixture entry.
type FloodChokepoint struct {
	CameraID       string      `json:"camera_id"`
	CameraName     string      `json:"camera_name"`
	District       string      `json:"district"`
	FloodMechanism string      `json:"flood_mechanism"` // "tidal" | "rain" | "compound"
	TypicalDepthCm int         `json:"typical_depth_cm"`
	Coordinates    Coordinates `json:"coordinates"`
	H3Res7         string      `json:"h3_res7"`
	H3Res9         string      `json:"h3_res9"`
	Citation       string      `json:"citation"`
	Notes          string      `json:"notes"`
}

// ChokepointsCatalog represents the curated flood hotspots file structure.
type ChokepointsCatalog struct {
	Version         string            `json:"version"`
	Description     string            `json:"description"`
	CreatedAt       string            `json:"created_at"`
	CitationSources []string          `json:"citation_sources"`
	Chokepoints     []FloodChokepoint `json:"chokepoints"`
}

// FloodVerificationResult is returned by on-demand or background verification.
type FloodVerificationResult struct {
	CameraID             string    `json:"camera_id"`
	CameraName           string    `json:"camera_name"`
	FloodSeverity        string    `json:"flood_severity"`
	Confidence           float64   `json:"confidence"`
	Description          string    `json:"description,omitempty"`
	Source               string    `json:"source"`
	VerifiedAt           time.Time `json:"verified_at"`
	QuotaRemainingMinute int       `json:"quota_remaining_minute"`
	QuotaRemainingDay    int       `json:"quota_remaining_day"`
}

// QuotaStatus represents rate limit and daily quota utilization.
type QuotaStatus struct {
	LimitRPM       int       `json:"limit_rpm"`
	RemainingRPM   int       `json:"remaining_rpm"`
	LimitRPD       int       `json:"limit_rpd"`
	RemainingRPD   int       `json:"remaining_rpd"`
	CurrentDay     string    `json:"current_day"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
}

// Sentinel errors for flood verification service
var (
	ErrRateLimited        = errors.New("rate limit exceeded (10 RPM)")
	ErrDailyQuotaExceeded = errors.New("daily quota exceeded (300 RPD)")
	ErrCameraNotFound     = errors.New("camera not found in catalog")
)

// WeatherProvider defines the contract for fetching weather observations.
type WeatherProvider interface {
	GetWeather(ctx context.Context) (*weather.WeatherData, error)
}
