package camera

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestRouter() (*gin.Engine, *Catalog) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	pBenNgheID := "P_BEN_NGHE"
	pBenNgheName := "Phường Bến Nghé"
	pCatLaiID := "P_CAT_LAI"
	pCatLaiName := "Phường Cát Lái"

	cams := []Camera{
		{
			ID:          "cam-d1",
			Title:       "Cam D1",
			DisplayName: "Nguyễn Huệ - Lê Lợi",
			Status:      StatusOnline,
			IsOnline:    true,
			Latitude:    10.7745,
			Longitude:   106.7012,
			SnapshotURL: "http://example.com/d1.jpg",
			Zone:        ZoneNoiThanh,
			PhuongID:    &pBenNgheID,
			PhuongName:  &pBenNgheName,
		},
		{
			ID:          "cam-cl",
			Title:       "Cam CL",
			DisplayName: "Cát Lái - Đường A",
			Status:      StatusOnline,
			IsOnline:    true,
			Latitude:    10.7650,
			Longitude:   106.7800,
			SnapshotURL: "http://example.com/cl.jpg",
			Zone:        ZoneCatLai,
			PhuongID:    &pCatLaiID,
			PhuongName:  &pCatLaiName,
		},
		{
			ID:          "cam-bd",
			Title:       "Cam BD",
			DisplayName: "Mỹ Phước - Tân Vạn",
			Status:      StatusOffline,
			IsOnline:    false,
			Latitude:    10.9300,
			Longitude:   106.7000,
			SnapshotURL: "http://example.com/bd.jpg",
			Zone:        ZoneBinhDuong,
		},
	}

	cat := NewCatalog(cams)
	api := router.Group("/api")
	RegisterRoutes(api, cat)

	return router, cat
}

func TestGetCameras_All(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cameras []Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cameras); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(cameras) != 3 {
		t.Errorf("expected 3 cameras, got %d", len(cameras))
	}
}

func TestGetCameras_FilterZone(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras?zone=Cụm+Cảng+Cát+Lái", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cameras []Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cameras); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(cameras) != 1 || cameras[0].ID != "cam-cl" {
		t.Errorf("expected 1 camera (cam-cl), got %v", cameras)
	}
}

func TestGetCameras_FilterCorridor(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras?corridor=lê+lợi", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cameras []Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cameras); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(cameras) != 1 || cameras[0].ID != "cam-d1" {
		t.Errorf("expected 1 camera (cam-d1), got %v", cameras)
	}
}

func TestGetCameras_FilterBoth(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras?zone=Bình+Dương&corridor=Mỹ+Phước", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cameras []Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cameras); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(cameras) != 1 || cameras[0].ID != "cam-bd" {
		t.Errorf("expected 1 camera (cam-bd), got %v", cameras)
	}
}

func TestGetCameras_NoMatch(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras?zone=NonExistentZone", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Ensure empty array [] is returned, not null
	body := rec.Body.String()
	if body != "[]" {
		t.Errorf("expected empty JSON array '[]', got %q", body)
	}
}

func TestGetCameraByID_Success(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras/cam-d1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cam Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cam); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if cam.ID != "cam-d1" || cam.DisplayName != "Nguyễn Huệ - Lê Lợi" {
		t.Errorf("unexpected camera data: %+v", cam)
	}
}

func TestGetCameraByID_NotFound(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras/unknown-id", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp["error"] != ErrCameraNotFound {
		t.Errorf("expected error message %q, got %q", ErrCameraNotFound, errResp["error"])
	}
}

func TestGetZones(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/zones", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var zones []string
	if err := json.Unmarshal(rec.Body.Bytes(), &zones); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(zones) != 3 {
		t.Fatalf("expected 3 zones, got %d", len(zones))
	}

	// Check sorted order
	if zones[0] != ZoneBinhDuong || zones[1] != ZoneCatLai || zones[2] != ZoneNoiThanh {
		t.Errorf("unexpected sorted zones: %v", zones)
	}
}

// MockReader testing Liskov Substitution Principle and Dependency Inversion Principle
type mockReader struct {
	cameras []Camera
}

func (m *mockReader) All() []Camera {
	return m.cameras
}

func (m *mockReader) ByID(id string) (*Camera, bool) {
	for i := range m.cameras {
		if m.cameras[i].ID == id {
			return &m.cameras[i], true
		}
	}
	return nil, false
}

func (m *mockReader) Zones() []string {
	return []string{"MockZone"}
}

func (m *mockReader) Phuongs() []string {
	return []string{"MockPhuong"}
}

func (m *mockReader) Filter(zone, corridor string) []Camera {
	return m.cameras
}

func (m *mockReader) FilterByPhuong(phuongName string) []Camera {
	return m.cameras
}

func TestHandler_WithMockReader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	mock := &mockReader{
		cameras: []Camera{{ID: "mock-1", DisplayName: "Mock Cam"}},
	}
	h := NewHandler(mock)
	api := router.Group("/api")
	h.RegisterRoutes(api)

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras/mock-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with mock reader, got %d", rec.Code)
	}
}

func TestGetPhuongs(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/phuong", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var phuongs []string
	if err := json.Unmarshal(rec.Body.Bytes(), &phuongs); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(phuongs) != 2 {
		t.Fatalf("expected 2 phuongs, got %d (%v)", len(phuongs), phuongs)
	}
	if phuongs[0] != "Phường Bến Nghé" || phuongs[1] != "Phường Cát Lái" {
		t.Errorf("unexpected sorted phuongs: %v", phuongs)
	}
}

func TestGetCameras_IncludesPhuongFields(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(raw) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(raw))
	}

	// Verify cam-d1 has phuong fields
	var d1 map[string]interface{}
	for _, c := range raw {
		if c["id"] == "cam-d1" {
			d1 = c
			break
		}
	}
	if d1 == nil {
		t.Fatal("cam-d1 not found in response")
	}
	if d1["phuong_id"] != "P_BEN_NGHE" || d1["phuong_name"] != "Phường Bến Nghé" {
		t.Errorf("expected phuong fields for cam-d1, got %v", d1)
	}
}

func TestGetCameras_FilterPhuong(t *testing.T) {
	router, _ := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/cameras?phuong=Phường+Bến+Nghé", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var cameras []Camera
	if err := json.Unmarshal(rec.Body.Bytes(), &cameras); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(cameras) != 1 || cameras[0].ID != "cam-d1" {
		t.Errorf("expected 1 camera (cam-d1), got %v", cameras)
	}
}
