package reports

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultReportTTL is the duration after which an uncorroborated report transitions to EXPIRED.
	DefaultReportTTL = 90 * time.Minute
)

// SubmitReportRequest contains user input for a hazard observation.
type SubmitReportRequest struct {
	CameraID     string     `json:"camera_id,omitempty"`
	Latitude     float64    `json:"latitude"`
	Longitude    float64    `json:"longitude"`
	HazardType   HazardType `json:"hazard_type"`
	Description  string     `json:"description,omitempty"`
	ClientIPHash string     `json:"-"`
}

// Service manages the lifecycle, persistence, and verification of crowd-sourced hazard reports.
type Service struct {
	verifier *Verifier
	locator  CameraLocator
	clock    Clock
	ttl      time.Duration
	mu       sync.RWMutex
	reports  map[string]*HazardReport
}

// NewService constructs a Service with a Verifier and storage.
func NewService(verifier *Verifier, locator CameraLocator, clock Clock) *Service {
	if clock == nil {
		clock = RealClock{}
	}
	return &Service{
		verifier: verifier,
		locator:  locator,
		clock:    clock,
		ttl:      DefaultReportTTL,
		reports:  make(map[string]*HazardReport),
	}
}

// SetTTL configures custom TTL for reports.
func (s *Service) SetTTL(d time.Duration) {
	if d > 0 {
		s.ttl = d
	}
}

// SubmitReport registers a new report or corroborates an existing one in the same locality.
func (s *Service) SubmitReport(ctx context.Context, req SubmitReportRequest) (*HazardReport, error) {
	now := s.clock.Now()

	lat := req.Latitude
	lng := req.Longitude

	// Auto-fill coordinates if camera ID is provided
	if lat == 0 && lng == 0 && req.CameraID != "" && s.locator != nil {
		if cam, ok := s.locator.GetCamera(req.CameraID); ok {
			lat = cam.Latitude
			lng = cam.Longitude
		}
	}

	if lat == 0 && lng == 0 {
		return nil, fmt.Errorf("invalid coordinates or camera ID")
	}

	hazardType := req.HazardType
	if hazardType == "" {
		hazardType = HazardTypeFlood
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Look for existing active report to aggregate (same camera or within 50m)
	var existing *HazardReport
	for _, r := range s.reports {
		if r.Status == ReportStatusExpired {
			continue
		}
		if now.After(r.ExpiresAt) {
			continue
		}

		sameCam := req.CameraID != "" && r.CameraID == req.CameraID
		nearby := HaversineDistanceMeters(lat, lng, r.Latitude, r.Longitude) <= 50.0

		if (sameCam || nearby) && r.HazardType == hazardType {
			existing = r
			break
		}
	}

	if existing != nil {
		// Aggregate report
		isNewIP := true
		for _, h := range existing.ClientIPHashes {
			if h == req.ClientIPHash && req.ClientIPHash != "" {
				isNewIP = false
				break
			}
		}
		if isNewIP && req.ClientIPHash != "" {
			existing.ClientIPHashes = append(existing.ClientIPHashes, req.ClientIPHash)
		}
		existing.ReportCount++
		// Extend expiry slightly on corroborating signal
		existing.ExpiresAt = now.Add(s.ttl)

		// Re-verify
		if s.verifier != nil {
			vRes := s.verifier.Verify(ctx, *existing)
			if err := ValidateTransition(existing.Status, vRes.Status); err == nil {
				existing.Status = vRes.Status
				existing.Confidence = vRes.Confidence
				existing.CorroboratingCameraID = vRes.CorroboratingCameraID
				existing.CorroboratingDistance = vRes.CorroboratingDistance
				if vRes.Status == ReportStatusCorroborated && existing.VerifiedAt == nil {
					verifiedAt := now
					existing.VerifiedAt = &verifiedAt
				}
			}
		}
		return copyReport(existing), nil
	}

	// 2. Create new report
	reportID := generateID()
	var ipHashes []string
	if req.ClientIPHash != "" {
		ipHashes = append(ipHashes, req.ClientIPHash)
	}

	report := &HazardReport{
		ID:             reportID,
		CameraID:       req.CameraID,
		Latitude:       lat,
		Longitude:      lng,
		HazardType:     hazardType,
		Description:    req.Description,
		Status:         ReportStatusSubmitted,
		Confidence:     0.10,
		ReportCount:    1,
		ClientIPHashes: ipHashes,
		SubmittedAt:    now,
		ExpiresAt:      now.Add(s.ttl),
	}

	// Verify immediately
	if s.verifier != nil {
		vRes := s.verifier.Verify(ctx, *report)
		if err := ValidateTransition(report.Status, vRes.Status); err == nil {
			report.Status = vRes.Status
			report.Confidence = vRes.Confidence
			report.CorroboratingCameraID = vRes.CorroboratingCameraID
			report.CorroboratingDistance = vRes.CorroboratingDistance
			if vRes.Status == ReportStatusCorroborated {
				verifiedAt := now
				report.VerifiedAt = &verifiedAt
			}
		}
	}

	s.reports[reportID] = report
	return copyReport(report), nil
}

// OnCameraReport bridges feedback reports submitted by camera ID into the verification engine.
func (s *Service) OnCameraReport(cameraID, clientIPHash string, ts time.Time) (*HazardReport, error) {
	return s.SubmitReport(context.Background(), SubmitReportRequest{
		CameraID:     cameraID,
		HazardType:   HazardTypeFlood,
		ClientIPHash: clientIPHash,
	})
}

// GetActiveReports returns all non-expired hazard reports, pruning expired records.
func (s *Service) GetActiveReports() []HazardReport {
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]HazardReport, 0, len(s.reports))
	for id, r := range s.reports {
		if now.After(r.ExpiresAt) {
			_ = ValidateTransition(r.Status, ReportStatusExpired)
			r.Status = ReportStatusExpired
			delete(s.reports, id)
			continue
		}
		out = append(out, *copyReport(r))
	}
	return out
}

// GetReport retrieves a specific report by ID.
func (s *Service) GetReport(id string) (*HazardReport, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.reports[id]
	if !ok {
		return nil, false
	}
	return copyReport(r), true
}

func copyReport(r *HazardReport) *HazardReport {
	if r == nil {
		return nil
	}
	cp := *r
	if r.VerifiedAt != nil {
		vAt := *r.VerifiedAt
		cp.VerifiedAt = &vAt
	}
	return &cp
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
