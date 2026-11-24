package reports

import (
	"errors"
	"fmt"
	"time"
)

// ReportStatus represents the lifecycle state of a crowd-sourced hazard report.
type ReportStatus string

const (
	// ReportStatusSubmitted indicates the report has been received and is awaiting corroboration.
	ReportStatusSubmitted ReportStatus = "SUBMITTED"
	// ReportStatusCorroborated indicates nearby camera feeds or multiple reporters confirm the hazard.
	ReportStatusCorroborated ReportStatus = "CORROBORATED"
	// ReportStatusUnverified indicates nearby cameras are verified dry or corroborating signals are weak.
	ReportStatusUnverified ReportStatus = "UNVERIFIED"
	// ReportStatusExpired is the terminal state once the report passes its time-to-live window.
	ReportStatusExpired ReportStatus = "EXPIRED"
)

var (
	// ErrInvalidStateTransition occurs when attempting an illegal backwards or terminal transition.
	ErrInvalidStateTransition = errors.New("invalid report state transition")
)

// ValidateTransition ensures monotonic state progression.
// Valid paths:
//
//	SUBMITTED -> CORROBORATED | UNVERIFIED | EXPIRED
//	CORROBORATED -> EXPIRED
//	UNVERIFIED -> CORROBORATED | EXPIRED
//	EXPIRED -> (terminal, no transitions allowed)
func ValidateTransition(current, next ReportStatus) error {
	if current == next {
		return nil
	}
	switch current {
	case ReportStatusSubmitted:
		if next == ReportStatusCorroborated || next == ReportStatusUnverified || next == ReportStatusExpired {
			return nil
		}
	case ReportStatusCorroborated:
		if next == ReportStatusExpired {
			return nil
		}
	case ReportStatusUnverified:
		if next == ReportStatusCorroborated || next == ReportStatusExpired {
			return nil
		}
	case ReportStatusExpired:
		return fmt.Errorf("%w: cannot transition from terminal state EXPIRED to %s", ErrInvalidStateTransition, next)
	}
	return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidStateTransition, current, next)
}

// HazardType specifies the nature of reported incident.
type HazardType string

const (
	HazardTypeFlood       HazardType = "flood"
	HazardTypeCongestion  HazardType = "congestion"
	HazardTypeObstruction HazardType = "obstruction"
)

// HazardReport represents a citizen crowd-sourced incident observation.
type HazardReport struct {
	ID                    string       `json:"id"`
	CameraID              string       `json:"camera_id,omitempty"`
	Latitude              float64      `json:"latitude"`
	Longitude             float64      `json:"longitude"`
	HazardType            HazardType   `json:"hazard_type"`
	Description           string       `json:"description,omitempty"`
	Status                ReportStatus `json:"status"`
	Confidence            float64      `json:"confidence"`
	ReportCount           int          `json:"report_count"`
	ClientIPHashes        []string     `json:"-"`
	CorroboratingCameraID string       `json:"corroborating_camera_id,omitempty"`
	CorroboratingDistance float64      `json:"corroborating_distance_m,omitempty"`
	SubmittedAt           time.Time    `json:"submitted_at"`
	VerifiedAt            *time.Time   `json:"verified_at,omitempty"`
	ExpiresAt             time.Time    `json:"expires_at"`
}

// CorroborationDetail holds fine-grained signal breakdown from the Bayesian engine.
type CorroborationDetail struct {
	CVScore           float64 `json:"cv_score"`
	ReportCountScore  float64 `json:"report_count_score"`
	TideScore         float64 `json:"tide_score"`
	NearbyCameraCount int     `json:"nearby_camera_count"`
}

// VerificationResult summarizes the output of the spatial Bayesian verifier.
type VerificationResult struct {
	Status                ReportStatus        `json:"status"`
	Confidence            float64             `json:"confidence"`
	CorroboratingCameraID string              `json:"corroborating_camera_id,omitempty"`
	CorroboratingDistance float64             `json:"corroborating_distance_m,omitempty"`
	Detail                CorroborationDetail `json:"detail"`
}

// NearbyCamera represents a camera located within the proximity search threshold.
type NearbyCamera struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	DistanceM float64 `json:"distance_m"`
}

// CameraVerdictInfo represents observation data required for corroboration.
type CameraVerdictInfo struct {
	CameraID      string
	Severity      string
	FloodSeverity string
	FloodDepthCm  *float64
	Confidence    float64
	ObservedAt    time.Time
}
