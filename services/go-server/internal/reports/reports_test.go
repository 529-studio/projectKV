package reports_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/reports"
	"github.com/gin-gonic/gin"
)

// mockClock implements reports.Clock for deterministic time testing.
type mockClock struct {
	currentTime time.Time
}

func (m *mockClock) Now() time.Time {
	return m.currentTime
}

// mockLocator implements reports.CameraLocator.
type mockLocator struct {
	cameras map[string]reports.NearbyCamera
}

func (m *mockLocator) FindCamerasWithin(lat, lng float64, radiusMeters float64) []reports.NearbyCamera {
	var out []reports.NearbyCamera
	for _, cam := range m.cameras {
		d := reports.HaversineDistanceMeters(lat, lng, cam.Latitude, cam.Longitude)
		if d <= radiusMeters {
			camCopy := cam
			camCopy.DistanceM = d
			out = append(out, camCopy)
		}
	}
	return out
}

func (m *mockLocator) GetCamera(id string) (reports.NearbyCamera, bool) {
	cam, ok := m.cameras[id]
	return cam, ok
}

// mockVerdicts implements reports.VerdictReader.
type mockVerdicts struct {
	verdicts map[string]reports.CameraVerdictInfo
}

func (m *mockVerdicts) GetLatestVerdict(cameraID string) (reports.CameraVerdictInfo, bool) {
	v, ok := m.verdicts[cameraID]
	return v, ok
}

// mockTide implements reports.TideReader.
type mockTide struct {
	tideLevel float64
}

func (m *mockTide) PredictTide(station string, t time.Time) float64 {
	return m.tideLevel
}

func TestHaversineDistance(t *testing.T) {
	// Distance between Notre Dame Cathedral Saigon (10.77978, 106.69902)
	// and Ben Thanh Market (10.77255, 106.69803) is ~810 meters.
	dist := reports.HaversineDistanceMeters(10.77978, 106.69902, 10.77255, 106.69803)
	if dist < 800 || dist > 830 {
		t.Errorf("expected distance ~810m, got %.2f m", dist)
	}

	// Identical coordinates must be 0 meters
	if d := reports.HaversineDistanceMeters(10.77978, 106.69902, 10.77978, 106.69902); d != 0.0 {
		t.Errorf("expected 0m for identical coordinates, got %.2f m", d)
	}
}

func TestMonotonicStateTransitions(t *testing.T) {
	// Valid transitions
	validPairs := [][2]reports.ReportStatus{
		{reports.ReportStatusSubmitted, reports.ReportStatusCorroborated},
		{reports.ReportStatusSubmitted, reports.ReportStatusUnverified},
		{reports.ReportStatusSubmitted, reports.ReportStatusExpired},
		{reports.ReportStatusCorroborated, reports.ReportStatusExpired},
		{reports.ReportStatusUnverified, reports.ReportStatusCorroborated},
		{reports.ReportStatusUnverified, reports.ReportStatusExpired},
		{reports.ReportStatusSubmitted, reports.ReportStatusSubmitted},
	}

	for _, pair := range validPairs {
		if err := reports.ValidateTransition(pair[0], pair[1]); err != nil {
			t.Errorf("expected valid transition from %s to %s, got: %v", pair[0], pair[1], err)
		}
	}

	// Invalid backwards transitions
	invalidPairs := [][2]reports.ReportStatus{
		{reports.ReportStatusCorroborated, reports.ReportStatusSubmitted},
		{reports.ReportStatusCorroborated, reports.ReportStatusUnverified},
		{reports.ReportStatusExpired, reports.ReportStatusSubmitted},
		{reports.ReportStatusExpired, reports.ReportStatusCorroborated},
		{reports.ReportStatusExpired, reports.ReportStatusUnverified},
	}

	for _, pair := range invalidPairs {
		if err := reports.ValidateTransition(pair[0], pair[1]); err == nil {
			t.Errorf("expected error transitioning from %s to %s, got nil", pair[0], pair[1])
		}
	}
}

func TestVerifier_BayesianScenarios(t *testing.T) {
	baseTime := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	clock := &mockClock{currentTime: baseTime}

	locator := &mockLocator{
		cameras: map[string]reports.NearbyCamera{
			"cam_nearby": {
				ID:        "cam_nearby",
				Title:     "Nearby Cam 1",
				Latitude:  10.77255,
				Longitude: 106.69803,
			},
		},
	}

	// Scenario 1: Camera within 300m detects FLOODED_DEEP -> Corroborated
	verdicts := &mockVerdicts{
		verdicts: map[string]reports.CameraVerdictInfo{
			"cam_nearby": {
				CameraID:      "cam_nearby",
				FloodSeverity: "FLOODED_DEEP",
				Confidence:    0.92,
				ObservedAt:    baseTime.Add(-5 * time.Minute),
			},
		},
	}
	tide := &mockTide{tideLevel: 1.62} // Level 3

	v := reports.NewVerifier(locator, verdicts, tide, clock)

	res := v.Verify(context.Background(), reports.HazardReport{
		Latitude:    10.77260,
		Longitude:   106.69810,
		ReportCount: 1,
		Status:      reports.ReportStatusSubmitted,
		ExpiresAt:   baseTime.Add(90 * time.Minute),
	})

	if res.Status != reports.ReportStatusCorroborated {
		t.Errorf("expected CORROBORATED, got %s", res.Status)
	}
	if res.Confidence < 0.55 {
		t.Errorf("expected confidence >= 0.55, got %.2f", res.Confidence)
	}
	if res.CorroboratingCameraID != "cam_nearby" {
		t.Errorf("expected corroborating cam_nearby, got %s", res.CorroboratingCameraID)
	}

	// Scenario 2: Camera within 300m is DRY -> UNVERIFIED
	verdicts.verdicts["cam_nearby"] = reports.CameraVerdictInfo{
		CameraID:      "cam_nearby",
		FloodSeverity: "DRY",
		Confidence:    0.95,
		ObservedAt:    baseTime.Add(-5 * time.Minute),
	}
	tide.tideLevel = 1.20 // Normal tide

	res2 := v.Verify(context.Background(), reports.HazardReport{
		Latitude:    10.77260,
		Longitude:   106.69810,
		ReportCount: 1,
		Status:      reports.ReportStatusSubmitted,
		ExpiresAt:   baseTime.Add(90 * time.Minute),
	})

	if res2.Status != reports.ReportStatusUnverified {
		t.Errorf("expected UNVERIFIED when nearby camera is dry, got %s", res2.Status)
	}
	if res2.Confidence >= 0.40 {
		t.Errorf("expected confidence < 0.40, got %.2f", res2.Confidence)
	}

	// Scenario 3: Multiple reports (>=3) + High tide (>=1.60m) even without camera water -> Corroborated
	res3 := v.Verify(context.Background(), reports.HazardReport{
		Latitude:    10.77260,
		Longitude:   106.69810,
		ReportCount: 3,
		Status:      reports.ReportStatusSubmitted,
		ExpiresAt:   baseTime.Add(90 * time.Minute),
	})
	tide.tideLevel = 1.65 // High tide surge

	res3 = v.Verify(context.Background(), reports.HazardReport{
		Latitude:    10.77260,
		Longitude:   106.69810,
		ReportCount: 3,
		Status:      reports.ReportStatusSubmitted,
		ExpiresAt:   baseTime.Add(90 * time.Minute),
	})

	if res3.Status != reports.ReportStatusCorroborated {
		t.Errorf("expected multiple reports + high tide to corroborate, got %s", res3.Status)
	}
}

func TestService_SubmitAndExpireLifecycle(t *testing.T) {
	baseTime := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	clock := &mockClock{currentTime: baseTime}

	locator := &mockLocator{
		cameras: map[string]reports.NearbyCamera{
			"cam_a": {
				ID:        "cam_a",
				Title:     "Cam A",
				Latitude:  10.7500,
				Longitude: 106.6800,
			},
		},
	}

	verdicts := &mockVerdicts{
		verdicts: map[string]reports.CameraVerdictInfo{
			"cam_a": {
				CameraID:      "cam_a",
				FloodSeverity: "FLOODED_SHALLOW",
				ObservedAt:    baseTime,
			},
		},
	}

	verifier := reports.NewVerifier(locator, verdicts, &mockTide{tideLevel: 1.55}, clock)
	svc := reports.NewService(verifier, locator, clock)

	// 1. Submit initial report for cam_a
	rep, err := svc.SubmitReport(context.Background(), reports.SubmitReportRequest{
		CameraID:     "cam_a",
		HazardType:   reports.HazardTypeFlood,
		ClientIPHash: "ip_hash_1",
	})
	if err != nil {
		t.Fatalf("failed to submit report: %v", err)
	}
	if rep.Status != reports.ReportStatusCorroborated {
		t.Errorf("expected CORROBORATED, got %s", rep.Status)
	}
	if rep.ReportCount != 1 {
		t.Errorf("expected report count 1, got %d", rep.ReportCount)
	}

	// 2. Aggregate second report from distinct IP within 50m
	rep2, err := svc.SubmitReport(context.Background(), reports.SubmitReportRequest{
		Latitude:     10.7501,
		Longitude:    106.6801,
		HazardType:   reports.HazardTypeFlood,
		ClientIPHash: "ip_hash_2",
	})
	if err != nil {
		t.Fatalf("failed to aggregate report: %v", err)
	}
	if rep2.ID != rep.ID {
		t.Errorf("expected aggregated report to retain ID %s, got %s", rep.ID, rep2.ID)
	}
	if rep2.ReportCount != 2 {
		t.Errorf("expected report count 2, got %d", rep2.ReportCount)
	}

	// 3. Test active listing
	active := svc.GetActiveReports()
	if len(active) != 1 {
		t.Fatalf("expected 1 active report, got %d", len(active))
	}

	// 4. Advance clock past 90 min TTL -> Verify report expires and is pruned
	clock.currentTime = baseTime.Add(91 * time.Minute)
	activeAfter := svc.GetActiveReports()
	if len(activeAfter) != 0 {
		t.Errorf("expected 0 active reports after TTL expiration, got %d", len(activeAfter))
	}
}

func TestHandler_HTTPEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseTime := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	clock := &mockClock{currentTime: baseTime}

	locator := &mockLocator{cameras: map[string]reports.NearbyCamera{}}
	verifier := reports.NewVerifier(locator, nil, nil, clock)
	svc := reports.NewService(verifier, locator, clock)
	handler := reports.NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api)

	// Test POST /api/reports
	body := map[string]interface{}{
		"latitude":    10.75,
		"longitude":   106.68,
		"hazard_type": "flood",
		"description": "Nước ngập nửa bánh xe",
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/reports", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var created reports.HazardReport
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if created.ID == "" {
		t.Errorf("expected non-empty ID")
	}

	// Test GET /api/reports
	reqGet := httptest.NewRequest(http.MethodGet, "/api/reports", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wGet.Code)
	}

	var list []reports.HazardReport
	if err := json.Unmarshal(wGet.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to parse list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 report in list, got %d", len(list))
	}

	// Test GET /api/reports/:id
	reqDetail := httptest.NewRequest(http.MethodGet, "/api/reports/"+created.ID, nil)
	wDetail := httptest.NewRecorder()
	r.ServeHTTP(wDetail, reqDetail)

	if wDetail.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wDetail.Code)
	}

	// Test GET /api/reports/nonexistent -> 404
	req404 := httptest.NewRequest(http.MethodGet, "/api/reports/nonexistent", nil)
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, req404)

	if w404.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w404.Code)
	}
}
