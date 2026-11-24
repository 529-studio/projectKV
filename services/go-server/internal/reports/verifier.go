package reports

import (
	"context"
	"math"
	"strings"
	"time"
)

// Verifier executes spatial proximity corroboration and Bayesian confidence scoring.
type Verifier struct {
	locator  CameraLocator
	verdicts VerdictReader
	tide     TideReader
	clock    Clock
	radiusM  float64
}

// NewVerifier constructs a Verifier with injected dependencies.
func NewVerifier(locator CameraLocator, verdicts VerdictReader, tide TideReader, clock Clock) *Verifier {
	if clock == nil {
		clock = RealClock{}
	}
	return &Verifier{
		locator:  locator,
		verdicts: verdicts,
		tide:     tide,
		clock:    clock,
		radiusM:  DefaultCorroborationRadiusMeters,
	}
}

// SetRadiusMeters customizes the spatial corroboration radius.
func (v *Verifier) SetRadiusMeters(r float64) {
	if r > 0 {
		v.radiusM = r
	}
}

// Verify evaluates a report against nearby CCTV cameras, multiple reports, and river tide surge.
func (v *Verifier) Verify(ctx context.Context, report HazardReport) VerificationResult {
	now := v.clock.Now()

	// Check if already expired by TTL
	if !report.ExpiresAt.IsZero() && (now.Equal(report.ExpiresAt) || now.After(report.ExpiresAt)) {
		return VerificationResult{
			Status:     ReportStatusExpired,
			Confidence: report.Confidence,
			Detail: CorroborationDetail{
				NearbyCameraCount: 0,
			},
		}
	}

	lat := report.Latitude
	lng := report.Longitude

	// If lat/lng zero but camera_id provided, locate camera coordinates
	if lat == 0 && lng == 0 && report.CameraID != "" && v.locator != nil {
		if cam, ok := v.locator.GetCamera(report.CameraID); ok {
			lat = cam.Latitude
			lng = cam.Longitude
		}
	}

	var nearbyCams []NearbyCamera
	if v.locator != nil && lat != 0 && lng != 0 {
		nearbyCams = v.locator.FindCamerasWithin(lat, lng, v.radiusM)
	}

	// 1. Calculate CV Score from nearest/most severe camera
	var bestCVScore float64 = 0.0
	var corroboratingCamID string
	var corroboratingDist float64 = 0.0
	hasDryCameraConfirmed := false

	for _, cam := range nearbyCams {
		if v.verdicts == nil {
			continue
		}
		verdict, ok := v.verdicts.GetLatestVerdict(cam.ID)
		if !ok {
			continue
		}

		score := scoreVerdict(verdict)
		if score > bestCVScore {
			bestCVScore = score
			corroboratingCamID = cam.ID
			corroboratingDist = cam.DistanceM
		}
		if score == 0.0 && isCameraRecent(verdict, now) {
			hasDryCameraConfirmed = true
		}
	}

	// If a direct camera ID was reported, evaluate it directly if not already chosen
	if report.CameraID != "" && v.verdicts != nil && corroboratingCamID == "" {
		if verdict, ok := v.verdicts.GetLatestVerdict(report.CameraID); ok {
			score := scoreVerdict(verdict)
			if score > bestCVScore {
				bestCVScore = score
				corroboratingCamID = report.CameraID
				corroboratingDist = 0.0
			}
			if score == 0.0 && isCameraRecent(verdict, now) {
				hasDryCameraConfirmed = true
			}
		}
	}

	// 2. Calculate Report Count Score
	reportCountScore := scoreReportCount(report.ReportCount)

	// 3. Calculate Tidal Elevation Score
	tideScore := 0.1
	if v.tide != nil {
		tideLevel := v.tide.PredictTide("phu_an", now)
		tideScore = scoreTideLevel(tideLevel)
	}

	// Bayesian weighted combination: C = 0.45*CV + 0.35*Reports + 0.20*Tide
	rawConfidence := (0.45 * bestCVScore) + (0.35 * reportCountScore) + (0.20 * tideScore)
	confidence := math.Round(rawConfidence*100) / 100

	// 4. Determine state machine progression
	nextStatus := ReportStatusSubmitted

	// Corroborated when confidence >= 0.50 AND (camera confirms water OR multiple reporters + high tide)
	if (confidence >= 0.50 && bestCVScore >= 0.5) || (reportCountScore >= 0.7 && tideScore >= 0.7 && confidence >= 0.50) {
		nextStatus = ReportStatusCorroborated
	} else if len(nearbyCams) > 0 && hasDryCameraConfirmed && bestCVScore == 0.0 && confidence < 0.40 {
		// Cameras exist within 300m and are verified dry
		nextStatus = ReportStatusUnverified
	} else {
		// Keep current status if valid, else remain submitted
		if report.Status == ReportStatusCorroborated || report.Status == ReportStatusUnverified {
			nextStatus = report.Status
		}
	}

	return VerificationResult{
		Status:                nextStatus,
		Confidence:            confidence,
		CorroboratingCameraID: corroboratingCamID,
		CorroboratingDistance: math.Round(corroboratingDist*10) / 10,
		Detail: CorroborationDetail{
			CVScore:           bestCVScore,
			ReportCountScore:  reportCountScore,
			TideScore:         tideScore,
			NearbyCameraCount: len(nearbyCams),
		},
	}
}

func scoreVerdict(v CameraVerdictInfo) float64 {
	switch strings.ToUpper(strings.TrimSpace(v.FloodSeverity)) {
	case "FLOODED_DEEP":
		return 1.0
	case "FLOODED_SHALLOW":
		return 0.8
	case "WET":
		return 0.5
	}
	if v.FloodDepthCm != nil {
		if *v.FloodDepthCm >= 20.0 {
			return 1.0
		} else if *v.FloodDepthCm >= 10.0 {
			return 0.8
		} else if *v.FloodDepthCm > 0.0 {
			return 0.5
		}
	}
	return 0.0
}

func scoreReportCount(count int) float64 {
	if count >= 3 {
		return 1.0
	} else if count == 2 {
		return 0.7
	} else if count == 1 {
		return 0.4
	}
	return 0.0
}

func scoreTideLevel(tideM float64) float64 {
	if tideM >= 1.60 {
		return 1.0 // Alert Level 3
	} else if tideM >= 1.50 {
		return 0.7 // Alert Level 2
	} else if tideM >= 1.40 {
		return 0.4 // Alert Level 1
	}
	return 0.1 // Normal
}

func isCameraRecent(v CameraVerdictInfo, now time.Time) bool {
	if v.ObservedAt.IsZero() {
		return false
	}
	return now.Sub(v.ObservedAt) <= 30*time.Minute
}
