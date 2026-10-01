package corridor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/gin-gonic/gin"
)

// mockCameraProvider implements CameraProvider for testing.
type mockCameraProvider struct {
	cameras []camera.Camera
}

func (m *mockCameraProvider) All() []camera.Camera {
	return m.cameras
}

// mockVerdictProvider implements VerdictProvider for testing.
type mockVerdictProvider struct {
	verdicts []hotset.Verdict
}

func (m *mockVerdictProvider) GetLatest() []hotset.Verdict {
	return m.verdicts
}

func sampleCameras() []camera.Camera {
	return []camera.Camera{
		{
			ID:          "cam-1",
			DisplayName: "Võ Văn Kiệt - Ký Con",
			RoadKey:     "vo_van_kiet",
			RoadKeys:    []string{"vo_van_kiet", "ky_con"},
		},
		{
			ID:          "cam-2",
			DisplayName: "Võ Văn Kiệt - Cầu Calmette",
			RoadKey:     "vo_van_kiet",
			RoadKeys:    []string{"vo_van_kiet", "cau_calmette"},
		},
		{
			ID:          "cam-3",
			DisplayName: "Điện Biên Phủ - Pasteur",
			RoadKey:     "dien_bien_phu",
			RoadKeys:    []string{"dien_bien_phu", "pasteur"},
		},
		{
			ID:          "cam-4",
			DisplayName: "Pasteur - Lê Thánh Tôn",
			RoadKey:     "pasteur",
			RoadKeys:    []string{"pasteur", "le_thanh_ton"},
		},
	}
}

func TestGetStatus_CongestionEvaluation(t *testing.T) {
	now := time.Now()
	obs1 := now.Add(-30 * time.Second)
	obs2 := now.Add(-60 * time.Second)

	cameras := sampleCameras()
	verdicts := []hotset.Verdict{
		{
			CameraID:   "cam-1",
			Severity:   "CLEAR",
			ObservedAt: obs1,
			Source:     "vlm",
		},
		{
			CameraID:   "cam-2",
			Severity:   "CONGESTED",
			ObservedAt: obs2,
			Source:     "vlm",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	status, ok := svc.GetStatus("vo_van_kiet")
	if !ok || status == nil {
		t.Fatalf("expected status for vo_van_kiet, got ok=%v", ok)
	}

	if status.WorstSeverity != "CONGESTED" {
		t.Errorf("expected worst severity CONGESTED, got %s", status.WorstSeverity)
	}
	if status.TotalCameras != 2 {
		t.Errorf("expected total 2 cameras, got %d", status.TotalCameras)
	}
	if status.VerifiedCameras != 2 {
		t.Errorf("expected 2 verified cameras, got %d", status.VerifiedCameras)
	}
	if status.CoveragePct != 100.0 {
		t.Errorf("expected coverage 100%%, got %.1f", status.CoveragePct)
	}
	if status.OldestObservationAgeS == nil || *status.OldestObservationAgeS < 55 {
		t.Errorf("expected oldest observation ~60s, got %v", status.OldestObservationAgeS)
	}
}

func TestGetStatus_HonestyRules_NoData(t *testing.T) {
	cameras := sampleCameras()
	// No verdicts at all
	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: []hotset.Verdict{}}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	status, ok := svc.GetStatus("vo_van_kiet")
	if !ok || status == nil {
		t.Fatalf("expected status for vo_van_kiet, got ok=%v", ok)
	}

	// Honesty rules: never says "OK to go", unverified = UNKNOWN, coverage = 0%
	if status.WorstSeverity != "UNKNOWN" {
		t.Errorf("expected UNKNOWN for unverified corridor, got %s", status.WorstSeverity)
	}
	if status.CoveragePct != 0.0 {
		t.Errorf("expected 0%% coverage, got %.1f", status.CoveragePct)
	}
	if status.OldestObservationAgeS != nil {
		t.Errorf("expected nil oldest observation age, got %v", status.OldestObservationAgeS)
	}
	if status.VerifiedCameras != 0 {
		t.Errorf("expected 0 verified cameras, got %d", status.VerifiedCameras)
	}
	for _, c := range status.Cameras {
		if c.Severity != "UNKNOWN" {
			t.Errorf("expected unverified camera to have severity UNKNOWN, got %s", c.Severity)
		}
	}
}

func TestGetStatus_StalenessFiltering(t *testing.T) {
	now := time.Now()
	// Stale observation (> 180s)
	obsStale := now.Add(-300 * time.Second)

	cameras := sampleCameras()
	verdicts := []hotset.Verdict{
		{
			CameraID:   "cam-1",
			Severity:   "CLEAR",
			ObservedAt: obsStale,
			Source:     "vlm",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	status, ok := svc.GetStatus("vo_van_kiet")
	if !ok || status == nil {
		t.Fatalf("expected status for vo_van_kiet, got ok=%v", ok)
	}

	// Stale camera must NOT contribute to verified count or worst severity
	if status.WorstSeverity != "UNKNOWN" {
		t.Errorf("expected UNKNOWN for stale corridor, got %s", status.WorstSeverity)
	}
	if status.VerifiedCameras != 0 {
		t.Errorf("expected 0 verified cameras for stale data, got %d", status.VerifiedCameras)
	}
	if status.CoveragePct != 0.0 {
		t.Errorf("expected 0%% coverage, got %.1f", status.CoveragePct)
	}

	// Stale flag must be true on the camera
	var foundCam1 bool
	for _, c := range status.Cameras {
		if c.CameraID == "cam-1" {
			foundCam1 = true
			if !c.Stale {
				t.Errorf("expected cam-1 to have Stale=true")
			}
		}
	}
	if !foundCam1 {
		t.Errorf("expected cam-1 in cameras list")
	}
}

func TestGetStatus_MockProvenanceFiltering(t *testing.T) {
	now := time.Now()
	obs := now.Add(-20 * time.Second)

	cameras := sampleCameras()
	verdicts := []hotset.Verdict{
		{
			CameraID:   "cam-1",
			Severity:   "CLEAR",
			ObservedAt: obs,
			Source:     "mock",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}

	// 1. In production: mock verdicts must be filtered out
	svcProd := NewService(camProv, verdProv, "production", 180*time.Second)
	statusProd, ok := svcProd.GetStatus("vo_van_kiet")
	if !ok || statusProd == nil {
		t.Fatalf("expected status in prod")
	}
	if statusProd.VerifiedCameras != 0 || statusProd.WorstSeverity != "UNKNOWN" {
		t.Errorf("production must ignore mock verdicts: verified=%d, worst=%s",
			statusProd.VerifiedCameras, statusProd.WorstSeverity)
	}

	// 2. In dev: mock verdicts are accepted
	svcDev := NewService(camProv, verdProv, "dev", 180*time.Second)
	statusDev, ok := svcDev.GetStatus("vo_van_kiet")
	if !ok || statusDev == nil {
		t.Fatalf("expected status in dev")
	}
	if statusDev.VerifiedCameras != 1 || statusDev.WorstSeverity != "CLEAR" {
		t.Errorf("dev must accept mock verdicts: verified=%d, worst=%s",
			statusDev.VerifiedCameras, statusDev.WorstSeverity)
	}
}

func TestGetStatus_PartialCoverage(t *testing.T) {
	now := time.Now()
	obs := now.Add(-40 * time.Second)

	cameras := sampleCameras()
	// Only cam-1 has a verdict; cam-2 does not
	verdicts := []hotset.Verdict{
		{
			CameraID:   "cam-1",
			Severity:   "MODERATE",
			ObservedAt: obs,
			Source:     "vlm",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	status, ok := svc.GetStatus("vo_van_kiet")
	if !ok || status == nil {
		t.Fatalf("expected status")
	}

	if status.WorstSeverity != "MODERATE" {
		t.Errorf("expected MODERATE, got %s", status.WorstSeverity)
	}
	if status.TotalCameras != 2 {
		t.Errorf("expected 2 total, got %d", status.TotalCameras)
	}
	if status.VerifiedCameras != 1 {
		t.Errorf("expected 1 verified, got %d", status.VerifiedCameras)
	}
	if status.CoveragePct != 50.0 {
		t.Errorf("expected 50%% coverage, got %.1f", status.CoveragePct)
	}
}

func TestGetStatus_NonExistentRoad(t *testing.T) {
	camProv := &mockCameraProvider{cameras: sampleCameras()}
	verdProv := &mockVerdictProvider{verdicts: []hotset.Verdict{}}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	status, ok := svc.GetStatus("non_existent_road_key_123")
	if ok || status != nil {
		t.Errorf("expected ok=false for non-existent road, got ok=%v", ok)
	}
}

func TestListRoads(t *testing.T) {
	cameras := sampleCameras()
	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: []hotset.Verdict{}}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	resp := svc.ListRoads()
	if resp.Total == 0 || len(resp.Roads) == 0 {
		t.Fatalf("expected non-empty roads list")
	}

	// vo_van_kiet appears in 2 cameras, pasteur in 2 cameras
	var voVanKietCount, pasteurCount int
	for _, r := range resp.Roads {
		if r.RoadKey == "vo_van_kiet" {
			voVanKietCount = r.CameraCount
		}
		if r.RoadKey == "pasteur" {
			pasteurCount = r.CameraCount
		}
	}
	if voVanKietCount != 2 {
		t.Errorf("expected vo_van_kiet to have 2 cameras, got %d", voVanKietCount)
	}
	if pasteurCount != 2 {
		t.Errorf("expected pasteur to have 2 cameras, got %d", pasteurCount)
	}
}

func TestHandler_HTTPRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cameras := sampleCameras()
	verdicts := []hotset.Verdict{
		{
			CameraID:   "cam-1",
			Severity:   "CLEAR",
			ObservedAt: time.Now().Add(-10 * time.Second),
			Source:     "vlm",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)
	handler := NewHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api)

	// 1. GET /api/roads/:road_key/status -> 200
	req := httptest.NewRequest(http.MethodGet, "/api/roads/vo_van_kiet/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var st CorridorStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if st.RoadKey != "vo_van_kiet" {
		t.Errorf("expected road_key=vo_van_kiet, got %s", st.RoadKey)
	}

	// 2. GET /api/roads/unknown/status -> 404
	req404 := httptest.NewRequest(http.MethodGet, "/api/roads/unknown_road_xyz/status", nil)
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, req404)
	if w404.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown road, got %d", w404.Code)
	}

	// 3. GET /api/roads -> 200
	reqRoads := httptest.NewRequest(http.MethodGet, "/api/roads", nil)
	wRoads := httptest.NewRecorder()
	r.ServeHTTP(wRoads, reqRoads)
	if wRoads.Code != http.StatusOK {
		t.Errorf("expected 200 for /api/roads, got %d", wRoads.Code)
	}
	var roadsResp RoadsResponse
	if err := json.Unmarshal(wRoads.Body.Bytes(), &roadsResp); err != nil {
		t.Fatalf("failed to unmarshal roads response: %v", err)
	}
	if roadsResp.Total == 0 {
		t.Errorf("expected total > 0")
	}
}
