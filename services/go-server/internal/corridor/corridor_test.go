package corridor

import (
	"encoding/json"
	"math"
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

	// 1.5 GET /api/roads/status-summary -> 200
	reqSum := httptest.NewRequest(http.MethodGet, "/api/roads/status-summary", nil)
	wSum := httptest.NewRecorder()
	r.ServeHTTP(wSum, reqSum)
	if wSum.Code != http.StatusOK {
		t.Errorf("expected 200 for /api/roads/status-summary, got %d", wSum.Code)
	}
	var sumMap map[string]CorridorStatus
	if err := json.Unmarshal(wSum.Body.Bytes(), &sumMap); err != nil {
		t.Fatalf("failed to unmarshal JSON summary: %v", err)
	}
	if _, ok := sumMap["vo_van_kiet"]; !ok {
		t.Errorf("expected vo_van_kiet in summary map")
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

	// 4. GET /api/roads/:road_key/route -> 200
	reqRoute := httptest.NewRequest(http.MethodGet, "/api/roads/vo_van_kiet/route", nil)
	wRoute := httptest.NewRecorder()
	r.ServeHTTP(wRoute, reqRoute)
	if wRoute.Code != http.StatusOK {
		t.Errorf("expected 200 for /api/roads/:road_key/route, got %d: %s", wRoute.Code, wRoute.Body.String())
	}

	// 5. GET /api/corridors/overview -> 200
	reqOverview := httptest.NewRequest(http.MethodGet, "/api/corridors/overview", nil)
	wOverview := httptest.NewRecorder()
	r.ServeHTTP(wOverview, reqOverview)
	if wOverview.Code != http.StatusOK {
		t.Errorf("expected 200 for /api/corridors/overview, got %d: %s", wOverview.Code, wOverview.Body.String())
	}
}

func TestGetRoute_PassableAndGeoJSON(t *testing.T) {
	now := time.Now()
	cameras := sampleCameras()
	depthZero := 0.0
	verdicts := []hotset.Verdict{
		{
			CameraID:     "cam-1",
			Severity:     "CLEAR",
			FloodDepthCm: &depthZero,
			ObservedAt:   now.Add(-20 * time.Second),
			Source:       "onnx",
		},
		{
			CameraID:     "cam-2",
			Severity:     "CLEAR",
			FloodDepthCm: &depthZero,
			ObservedAt:   now.Add(-10 * time.Second),
			Source:       "onnx",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	geoJSON, summary, ok := svc.GetRoute("vo_van_kiet")
	if !ok {
		t.Fatalf("expected GetRoute to succeed for vo_van_kiet")
	}

	if summary.PassabilityVerdict != PassabilityPassable {
		t.Errorf("expected PASSABLE, got %s", summary.PassabilityVerdict)
	}
	if summary.StatusColor != "#22c55e" {
		t.Errorf("expected green #22c55e, got %s", summary.StatusColor)
	}
	if summary.FreeFlowSeconds <= 0 {
		t.Errorf("expected positive free flow seconds")
	}
	if summary.EstimatedSeconds < summary.FreeFlowSeconds {
		t.Errorf("estimated seconds cannot be less than free flow seconds")
	}

	// Verify GeoJSON structure
	if geoJSON.Type != "FeatureCollection" {
		t.Errorf("expected FeatureCollection, got %s", geoJSON.Type)
	}
	if len(geoJSON.Features) < 1 {
		t.Fatalf("expected at least 1 feature (LineString)")
	}

	lineFeature := geoJSON.Features[0]
	if lineFeature.Geometry.Type != "LineString" {
		t.Errorf("first feature must be LineString, got %s", lineFeature.Geometry.Type)
	}
}

func TestGetRoute_ImpassableFlood(t *testing.T) {
	now := time.Now()
	cameras := sampleCameras()
	depthDeep := 26.5
	hazardLvl := 3
	verdicts := []hotset.Verdict{
		{
			CameraID:         "cam-1",
			Severity:         "CONGESTED",
			FloodSeverity:    "FLOODED_DEEP",
			FloodDepthCm:     &depthDeep,
			FloodHazardLevel: &hazardLvl,
			ObservedAt:       now.Add(-15 * time.Second),
			Source:           "onnx",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	_, summary, ok := svc.GetRoute("vo_van_kiet")
	if !ok {
		t.Fatalf("expected GetRoute to succeed")
	}

	if summary.PassabilityVerdict != PassabilityImpassable {
		t.Errorf("expected IMPASSABLE for 26.5cm flood, got %s", summary.PassabilityVerdict)
	}
	if summary.StatusColor != "#ef4444" {
		t.Errorf("expected red #ef4444, got %s", summary.StatusColor)
	}
	if summary.DelaySeconds < 600 {
		t.Errorf("expected delay penalty for deep flood, got %d", summary.DelaySeconds)
	}
}

func TestGetCorridorsOverview(t *testing.T) {
	cameras := sampleCameras()
	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: nil}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	overview := svc.GetCorridorsOverview()
	if len(overview) == 0 {
		t.Fatalf("expected non-empty corridors overview")
	}
	foundVoVanKiet := false
	for _, item := range overview {
		if item.RoadKey == "vo_van_kiet" {
			foundVoVanKiet = true
			if item.LengthKm <= 0 {
				t.Errorf("expected positive length for vo_van_kiet")
			}
		}
	}
	if !foundVoVanKiet {
		t.Errorf("expected vo_van_kiet in corridors overview")
	}
}

// testHaversineKm calculates great-circle distance between two (lon, lat) points in kilometers.
func testHaversineKm(lon1, lat1, lon2, lat2 float64) float64 {
	const r = 6371.0
	phi1 := lat1 * math.Pi / 180.0
	phi2 := lat2 * math.Pi / 180.0
	dphi := (lat2 - lat1) * math.Pi / 180.0
	dlam := (lon2 - lon1) * math.Pi / 180.0
	a := math.Sin(dphi/2)*math.Sin(dphi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(dlam/2)*math.Sin(dlam/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// TestArterialCorridors_GeometryInvariants enforces physical road continuity, bounds sanity, and absence of zig-zags.
func TestArterialCorridors_GeometryInvariants(t *testing.T) {
	if len(ArterialCorridors) == 0 {
		t.Fatalf("ArterialCorridors map is empty")
	}

	// HCMC urban bounding box
	const (
		minLon = 106.30
		maxLon = 107.00
		minLat = 10.30
		maxLat = 11.20
		// Max allowed gap between two consecutive points on an arterial road (km)
		maxSegmentKm = 4.5
		// Max allowed reversal angle between adjacent segments (degrees)
		maxReversalAngleDeg = 110.0
	)

	for key, corridor := range ArterialCorridors {
		t.Run(key, func(t *testing.T) {
			coords := corridor.Coordinates
			if len(coords) < 2 {
				t.Fatalf("corridor %s must have at least 2 points, got %d", key, len(coords))
			}

			var totalPolylineKm float64
			for i := 0; i < len(coords); i++ {
				lon, lat := coords[i][0], coords[i][1]
				if lon < minLon || lon > maxLon || lat < minLat || lat > maxLat {
					t.Errorf("corridor %s point %d (lon=%.6f, lat=%.6f) out of HCMC bounds [%.2f..%.2f, %.2f..%.2f]",
						key, i, lon, lat, minLon, maxLon, minLat, maxLat)
				}

				if i > 0 {
					prevLon, prevLat := coords[i-1][0], coords[i-1][1]
					segDist := testHaversineKm(prevLon, prevLat, lon, lat)
					totalPolylineKm += segDist
					if segDist > maxSegmentKm {
						t.Errorf("corridor %s segment %d->%d distance %.2f km exceeds max allowed %.2f km",
							key, i-1, i, segDist, maxSegmentKm)
					}
				}

				if i > 0 && i < len(coords)-1 {
					prevLon, prevLat := coords[i-1][0], coords[i-1][1]
					nextLon, nextLat := coords[i+1][0], coords[i+1][1]
					v1x, v1y := lon-prevLon, lat-prevLat
					v2x, v2y := nextLon-lon, nextLat-lat
					dot := v1x*v2x + v1y*v2y
					m1 := math.Hypot(v1x, v1y)
					m2 := math.Hypot(v2x, v2y)
					if m1 > 0 && m2 > 0 {
						cosTheta := dot / (m1 * m2)
						if cosTheta < -1.0 {
							cosTheta = -1.0
						} else if cosTheta > 1.0 {
							cosTheta = 1.0
						}
						angleDeg := math.Acos(cosTheta) * 180.0 / math.Pi
						if angleDeg > maxReversalAngleDeg {
							t.Errorf("corridor %s point %d has zig-zag reversal angle %.1f° > %.1f° at (%.6f, %.6f)",
								key, i, angleDeg, maxReversalAngleDeg, lon, lat)
						}
					}
				}
			}

			// Validate total polyline distance against declared LengthKm (within 50% - 150%)
			if corridor.LengthKm > 0 {
				ratio := totalPolylineKm / corridor.LengthKm
				if ratio < 0.50 || ratio > 1.50 {
					t.Errorf("corridor %s polyline sum %.2f km deviates excessively from declared length %.2f km (ratio=%.2f)",
						key, totalPolylineKm, corridor.LengthKm, ratio)
				}
			}
		})
	}
}

func TestVehicleClearanceProfiles(t *testing.T) {
	// 1. Normalization tests
	if NormalizeVehicleType("scooter") != VehicleScooter {
		t.Errorf("expected scooter, got %s", NormalizeVehicleType("scooter"))
	}
	if NormalizeVehicleType("motorbike") != VehicleMotorbike || NormalizeVehicleType("xe_so") != VehicleMotorbike {
		t.Errorf("expected motorbike normalization")
	}
	if NormalizeVehicleType("sedan") != VehicleSedan || NormalizeVehicleType("car") != VehicleSedan {
		t.Errorf("expected sedan normalization")
	}
	if NormalizeVehicleType("suv") != VehicleSUV || NormalizeVehicleType("ban_tai") != VehicleSUV {
		t.Errorf("expected suv normalization")
	}
	if NormalizeVehicleType("unknown_profile") != VehicleScooter {
		t.Errorf("expected fallback to scooter")
	}

	// 2. Dynamic divergence test at 22.0 cm flood
	now := time.Now()
	floodDepth := 22.0
	cameras := sampleCameras()
	verdicts := []hotset.Verdict{
		{
			CameraID:      "cam-1",
			Severity:      "CLEAR",
			FloodSeverity: "FLOODED_SHALLOW",
			FloodDepthCm:  &floodDepth,
			ObservedAt:    now.Add(-10 * time.Second),
			Source:        "onnx",
		},
	}

	camProv := &mockCameraProvider{cameras: cameras}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	// Scooter: 22cm >= 20cm => IMPASSABLE
	_, scooterSummary, ok := svc.GetRoute("vo_van_kiet", VehicleScooter)
	if !ok || scooterSummary.PassabilityVerdict != PassabilityImpassable {
		t.Errorf("expected scooter to be IMPASSABLE at 22cm, got %s", scooterSummary.PassabilityVerdict)
	}
	if scooterSummary.StatusColor != "#ef4444" {
		t.Errorf("expected red for scooter, got %s", scooterSummary.StatusColor)
	}

	// Motorbike: 22cm is between 15cm and 28cm => CAUTION
	_, motorbikeSummary, ok := svc.GetRoute("vo_van_kiet", VehicleMotorbike)
	if !ok || motorbikeSummary.PassabilityVerdict != PassabilityCaution {
		t.Errorf("expected motorbike to be CAUTION at 22cm, got %s", motorbikeSummary.PassabilityVerdict)
	}
	if motorbikeSummary.StatusColor != "#eab308" {
		t.Errorf("expected amber for motorbike, got %s", motorbikeSummary.StatusColor)
	}

	// Sedan: 22cm is between 15cm and 25cm => CAUTION
	_, sedanSummary, ok := svc.GetRoute("vo_van_kiet", VehicleSedan)
	if !ok || sedanSummary.PassabilityVerdict != PassabilityCaution {
		t.Errorf("expected sedan to be CAUTION at 22cm, got %s", sedanSummary.PassabilityVerdict)
	}

	// SUV: 22cm < 25cm => PASSABLE
	_, suvSummary, ok := svc.GetRoute("vo_van_kiet", VehicleSUV)
	if !ok || suvSummary.PassabilityVerdict != PassabilityPassable {
		t.Errorf("expected SUV to be PASSABLE at 22cm, got %s", suvSummary.PassabilityVerdict)
	}
	if suvSummary.StatusColor != "#22c55e" {
		t.Errorf("expected green for SUV, got %s", suvSummary.StatusColor)
	}
}

func TestEvaluateCommute_MultiCorridorAndDetour(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	floodDepth := 25.0 // 25cm flood on Nguyen Huu Canh

	cams := []camera.Camera{
		{
			ID:          "cam-nhc-1",
			DisplayName: "Nguyễn Hữu Cảnh - Ung Văn Khiêm",
			RoadKey:     "nguyen_huu_canh",
			RoadKeys:    []string{"nguyen_huu_canh"},
		},
		{
			ID:          "cam-dbp-1",
			DisplayName: "Điện Biên Phủ - Hàng Xanh",
			RoadKey:     "dien_bien_phu",
			RoadKeys:    []string{"dien_bien_phu"},
		},
	}

	verdicts := []hotset.Verdict{
		{
			CameraID:      "cam-nhc-1",
			Severity:      "CONGESTED",
			FloodSeverity: "FLOODED_DEEP",
			FloodDepthCm:  &floodDepth,
			ObservedAt:    now.Add(-10 * time.Second),
			Source:        "onnx",
		},
		{
			CameraID:      "cam-dbp-1",
			Severity:      "CLEAR",
			FloodSeverity: "DRY",
			ObservedAt:    now.Add(-10 * time.Second),
			Source:        "onnx",
		},
	}

	camProv := &mockCameraProvider{cameras: cams}
	verdProv := &mockVerdictProvider{verdicts: verdicts}
	svc := NewService(camProv, verdProv, "production", 180*time.Second)

	// 1. Commute evaluating only Nguyen Huu Canh for Scooter
	scooterEval := svc.EvaluateCommute([]string{"nguyen_huu_canh"}, VehicleScooter)
	if scooterEval.PassabilityVerdict != PassabilityImpassable {
		t.Errorf("expected scooter to be IMPASSABLE on Nguyen Huu Canh at 25cm, got %s", scooterEval.PassabilityVerdict)
	}
	if scooterEval.WorstBottleneck == nil {
		t.Fatalf("expected worst bottleneck to be populated")
	}
	if scooterEval.WorstBottleneck.CameraID != "cam-nhc-1" {
		t.Errorf("expected bottleneck cam-nhc-1, got %s", scooterEval.WorstBottleneck.CameraID)
	}
	if scooterEval.WorstBottleneck.FloodDepthCm == nil || *scooterEval.WorstBottleneck.FloodDepthCm != 25.0 {
		t.Errorf("expected depth 25cm, got %v", scooterEval.WorstBottleneck.FloodDepthCm)
	}

	// Detour check: Nguyen Huu Canh impassable -> suggests Dien Bien Phu!
	if scooterEval.Detour == nil {
		t.Fatalf("expected detour suggestion to Dien Bien Phu, got nil")
	}
	if scooterEval.Detour.DetourCorridorKey != "dien_bien_phu" {
		t.Errorf("expected detour key dien_bien_phu, got %s", scooterEval.Detour.DetourCorridorKey)
	}
	if scooterEval.Detour.PassabilityVerdict != PassabilityPassable {
		t.Errorf("expected detour to be PASSABLE, got %s", scooterEval.Detour.PassabilityVerdict)
	}

	// 2. SUV on same route: 25cm is CAUTION (threshold 25cm), not IMPASSABLE (threshold 40cm)
	suvEval := svc.EvaluateCommute([]string{"nguyen_huu_canh"}, VehicleSUV)
	if suvEval.PassabilityVerdict != PassabilityCaution {
		t.Errorf("expected SUV to be CAUTION on 25cm, got %s", suvEval.PassabilityVerdict)
	}

	// 3. HTTP handler test
	handler := NewHandler(svc)
	router := gin.New()
	handler.RegisterRoutes(router.Group("/api"))

	// Missing corridors -> 400
	req400 := httptest.NewRequest(http.MethodGet, "/api/corridors/commute-eval", nil)
	w400 := httptest.NewRecorder()
	router.ServeHTTP(w400, req400)
	if w400.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing corridors param, got %d", w400.Code)
	}

	// Valid corridors -> 200
	req200 := httptest.NewRequest(http.MethodGet, "/api/corridors/commute-eval?corridors=nguyen_huu_canh&vehicle=scooter", nil)
	w200 := httptest.NewRecorder()
	router.ServeHTTP(w200, req200)
	if w200.Code != http.StatusOK {
		t.Errorf("expected 200 for valid corridors, got %d", w200.Code)
	}

	var httpResp CommuteEvaluationResponse
	if err := json.Unmarshal(w200.Body.Bytes(), &httpResp); err != nil {
		t.Fatalf("failed to unmarshal JSON response: %v", err)
	}
	if httpResp.PassabilityVerdict != PassabilityImpassable {
		t.Errorf("expected IMPASSABLE from API, got %s", httpResp.PassabilityVerdict)
	}
	if httpResp.Detour == nil || httpResp.Detour.DetourCorridorKey != "dien_bien_phu" {
		t.Errorf("expected detour to dien_bien_phu from API, got %+v", httpResp.Detour)
	}
}
