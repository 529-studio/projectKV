package camera

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sampleCameras() []Camera {
	return []Camera{
		{
			ID:          "cam-1",
			Title:       "Cam 1",
			DisplayName: "Nguyễn Huệ - Lê Lợi",
			Status:      StatusOnline,
			IsOnline:    true,
			Latitude:    10.7745,
			Longitude:   106.7012,
			SnapshotURL: "http://example.com/cam1.jpg",
		},
		{
			ID:          "cam-2",
			Title:       "Cam 2",
			DisplayName: "Cát Lái - Đường B",
			Status:      StatusOnline,
			IsOnline:    true,
			Latitude:    10.7650,
			Longitude:   106.7800,
			SnapshotURL: "http://example.com/cam2.jpg",
		},
		{
			ID:          "cam-3",
			Title:       "Cam 3",
			DisplayName: "Võ Văn Kiệt - An Lạc",
			Status:      StatusOffline,
			IsOnline:    false,
			Latitude:    10.7200,
			Longitude:   106.6000,
			SnapshotURL: "http://example.com/cam3.jpg",
			Zone:        "Custom Western Zone",
		},
		{
			ID:          "cam-4",
			Title:       "Cam 4 - Mỹ Phước Tân Vạn",
			DisplayName: "Ngã tư Dĩ An",
			Status:      StatusOnline,
			IsOnline:    true,
			Latitude:    10.9300,
			Longitude:   106.7000,
			SnapshotURL: "http://example.com/cam4.jpg",
		},
	}
}

func TestNewCatalog(t *testing.T) {
	cams := sampleCameras()
	cat := NewCatalog(cams)

	if len(cat.All()) != len(cams) {
		t.Fatalf("expected %d cameras, got %d", len(cams), len(cat.All()))
	}

	// Verify custom zone was preserved
	cam3, ok := cat.ByID("cam-3")
	if !ok || cam3.Zone != "Custom Western Zone" {
		t.Errorf("expected cam-3 to preserve custom zone, got %v", cam3)
	}

	// Verify auto zone derivation via classifier
	cam1, ok := cat.ByID("cam-1")
	if !ok || cam1.Zone != ZoneNoiThanh {
		t.Errorf("expected cam-1 to have %s zone, got %v", ZoneNoiThanh, cam1.Zone)
	}

	cam2, ok := cat.ByID("cam-2")
	if !ok || cam2.Zone != ZoneCatLai {
		t.Errorf("expected cam-2 to have %s zone, got %v", ZoneCatLai, cam2.Zone)
	}

	cam4, ok := cat.ByID("cam-4")
	if !ok || cam4.Zone != ZoneBinhDuong {
		t.Errorf("expected cam-4 to have %s zone, got %v", ZoneBinhDuong, cam4.Zone)
	}
}

// MockZoneClassifier for testing DIP (custom injected classifier)
type mockZoneClassifier struct {
	assignedZone string
}

func (m *mockZoneClassifier) Classify(cam Camera) string {
	return m.assignedZone
}

func TestNewCatalogWithCustomClassifier(t *testing.T) {
	cams := []Camera{
		{ID: "c-1", DisplayName: "Custom Cam"},
	}
	mockClassifier := &mockZoneClassifier{assignedZone: "Mocked Zone"}
	cat := NewCatalogWithClassifier(cams, mockClassifier)

	cam, ok := cat.ByID("c-1")
	if !ok || cam.Zone != "Mocked Zone" {
		t.Errorf("expected Mocked Zone, got %v", cam.Zone)
	}
}

func TestCatalog_ByID(t *testing.T) {
	cat := NewCatalog(sampleCameras())

	cam, ok := cat.ByID("cam-1")
	if !ok || cam == nil || cam.ID != "cam-1" {
		t.Errorf("expected to find cam-1, got %v, ok=%v", cam, ok)
	}

	camNonExistent, ok := cat.ByID("non-existent")
	if ok || camNonExistent != nil {
		t.Errorf("expected non-existent camera to return ok=false, nil, got %v, ok=%v", camNonExistent, ok)
	}
}

func TestCatalog_Zones(t *testing.T) {
	cat := NewCatalog(sampleCameras())
	zones := cat.Zones()

	expected := []string{
		ZoneBinhDuong,
		"Custom Western Zone",
		ZoneCatLai,
		ZoneNoiThanh,
	}

	if !reflect.DeepEqual(zones, expected) {
		t.Errorf("expected zones %v, got %v", expected, zones)
	}
}

func TestCatalog_Filter(t *testing.T) {
	cat := NewCatalog(sampleCameras())

	// No filter -> all
	all := cat.Filter("", "")
	if len(all) != 4 {
		t.Errorf("expected 4 cameras with empty filters, got %d", len(all))
	}

	// Filter by zone (case-insensitive)
	bdCams := cat.Filter("bình dương", "")
	if len(bdCams) != 1 || bdCams[0].ID != "cam-4" {
		t.Errorf("expected cam-4 for Binh Duong filter, got %v", bdCams)
	}

	// Filter by corridor (matching DisplayName)
	leLoiCams := cat.Filter("", "lê lợi")
	if len(leLoiCams) != 1 || leLoiCams[0].ID != "cam-1" {
		t.Errorf("expected cam-1 for 'lê lợi' corridor, got %v", leLoiCams)
	}

	// Filter by corridor (matching Title)
	myPhuocCams := cat.Filter("", "mỹ phước")
	if len(myPhuocCams) != 1 || myPhuocCams[0].ID != "cam-4" {
		t.Errorf("expected cam-4 for 'mỹ phước' corridor, got %v", myPhuocCams)
	}

	// Filter by both zone and corridor
	bothMatch := cat.Filter(ZoneNoiThanh, "Nguyễn Huệ")
	if len(bothMatch) != 1 || bothMatch[0].ID != "cam-1" {
		t.Errorf("expected cam-1 for matching zone and corridor, got %v", bothMatch)
	}

	// Filter with conflicting criteria
	noMatch := cat.Filter(ZoneBinhDuong, "Nguyễn Huệ")
	if len(noMatch) != 0 {
		t.Errorf("expected 0 cameras for conflicting filter, got %d", len(noMatch))
	}
}

func TestDetermineZone_Heuristics(t *testing.T) {
	classifier := NewDefaultZoneClassifier()

	tests := []struct {
		name     string
		camera   Camera
		expected string
	}{
		{
			name:     "Ba Ria - Vung Tau by longitude",
			camera:   Camera{Longitude: 107.1, DisplayName: "QL51"},
			expected: ZoneBaRiaVungTau,
		},
		{
			name:     "Binh Duong by keyword",
			camera:   Camera{DisplayName: "Đại lộ Bình Dương - Thủ Dầu Một"},
			expected: ZoneBinhDuong,
		},
		{
			name:     "Cat Lai by bounding box and name",
			camera:   Camera{Latitude: 10.76, Longitude: 106.77, DisplayName: "Đồng Văn Cống - Nguyễn Thị Định"},
			expected: ZoneCatLai,
		},
		{
			name:     "District 7 - Nha Be by name",
			camera:   Camera{DisplayName: "Huỳnh Tấn Phát - Nguyễn Thị Thập"},
			expected: ZoneQuan7NhaBe,
		},
		{
			name:     "Thu Duc City by coordinates",
			camera:   Camera{Latitude: 10.82, Longitude: 106.78, DisplayName: "Võ Nguyên Giáp"},
			expected: ZoneThuDuc,
		},
		{
			name:     "Binh Tan - Binh Chanh by keyword",
			camera:   Camera{DisplayName: "Kinh Dương Vương - Vòng xoay An Lạc"},
			expected: ZoneBinhTan,
		},
		{
			name:     "District 12 - Hoc Mon - Cu Chi by keyword",
			camera:   Camera{DisplayName: "Ngã 4 An Sương"},
			expected: ZoneQ12HocMonCuChi,
		},
		{
			name:     "Urban Core default",
			camera:   Camera{Latitude: 10.77, Longitude: 106.69, DisplayName: "Hai Bà Trưng - Lê Duẩn"},
			expected: ZoneNoiThanh,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifier.Classify(tc.camera)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestLoadCatalog(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Array format
	cams := sampleCameras()
	arrBytes, _ := json.Marshal(cams)
	arrPath := filepath.Join(tempDir, "array.json")
	if err := os.WriteFile(arrPath, arrBytes, 0644); err != nil {
		t.Fatalf("failed to write array.json: %v", err)
	}

	catArr, err := LoadCatalog(arrPath)
	if err != nil {
		t.Fatalf("LoadCatalog array failed: %v", err)
	}
	if len(catArr.All()) != len(cams) {
		t.Errorf("expected %d cameras, got %d", len(cams), len(catArr.All()))
	}

	// 2. Envelope format {"cameras": [...]}
	envObj := map[string][]Camera{"cameras": cams}
	envBytes, _ := json.Marshal(envObj)
	envPath := filepath.Join(tempDir, "envelope.json")
	if err := os.WriteFile(envPath, envBytes, 0644); err != nil {
		t.Fatalf("failed to write envelope.json: %v", err)
	}

	catEnv, err := LoadCatalog(envPath)
	if err != nil {
		t.Fatalf("LoadCatalog envelope failed: %v", err)
	}
	if len(catEnv.All()) != len(cams) {
		t.Errorf("expected %d cameras, got %d", len(cams), len(catEnv.All()))
	}

	// 3. Map format {"id1": {...}, "id2": {...}}
	mapObj := map[string]Camera{
		"c1": cams[0],
		"c2": cams[1],
	}
	mapBytes, _ := json.Marshal(mapObj)
	mapPath := filepath.Join(tempDir, "map.json")
	if err := os.WriteFile(mapPath, mapBytes, 0644); err != nil {
		t.Fatalf("failed to write map.json: %v", err)
	}

	catMap, err := LoadCatalog(mapPath)
	if err != nil {
		t.Fatalf("LoadCatalog map failed: %v", err)
	}
	if len(catMap.All()) != 2 {
		t.Errorf("expected 2 cameras, got %d", len(catMap.All()))
	}

	// 4. Non-existent file
	_, err = LoadCatalog(filepath.Join(tempDir, "does-not-exist.json"))
	if err == nil {
		t.Errorf("expected error for non-existent file, got nil")
	}

	// 5. Invalid JSON
	invalidPath := filepath.Join(tempDir, "invalid.json")
	_ = os.WriteFile(invalidPath, []byte("{invalid json"), 0644)
	_, err = LoadCatalog(invalidPath)
	if err == nil {
		t.Errorf("expected error for invalid JSON, got nil")
	}
}

func TestCatalog_DeduplicationAndGhost(t *testing.T) {
	cams := []Camera{
		{
			ID:          "595dd9ac3dcfc400106f28b4",
			DisplayName: "Quốc lộ 1 - BQB Trước UBND Q12",
			IsOnline:    true,
			Status:      "UP",
		},
		{
			ID:          "595ddbbf3dcfc400106f28b8",
			DisplayName: "Quốc lộ 1 - Cầu vượt Tân Thới Hiệp 3",
			IsOnline:    true,
			Status:      "UP",
		},
		{
			ID:          "regular-cam-1",
			DisplayName: "Bình thường",
			IsOnline:    true,
			Status:      "UP",
		},
	}

	cat := NewCatalog(cams)

	// Verify duplicate camera mapping
	dupCam, ok := cat.ByID("595dd9ac3dcfc400106f28b4")
	if !ok {
		t.Fatal("expected duplicate camera to exist")
	}
	if dupCam.DuplicateOf != "595ddc123dcfc400106f28ba" {
		t.Errorf("expected DuplicateOf to be 595ddc123dcfc400106f28ba, got %s", dupCam.DuplicateOf)
	}

	// Verify ghost camera conversion to offline
	ghostCam, ok := cat.ByID("595ddbbf3dcfc400106f28b8")
	if !ok {
		t.Fatal("expected ghost camera to exist")
	}
	if !ghostCam.IsGhost {
		t.Error("expected IsGhost to be true")
	}
	if ghostCam.IsOnline {
		t.Error("expected IsOnline to be false for ghost camera")
	}
	if ghostCam.Status != "DOWN" {
		t.Errorf("expected Status to be DOWN, got %s", ghostCam.Status)
	}

	// Verify regular camera untouched
	regCam, ok := cat.ByID("regular-cam-1")
	if !ok {
		t.Fatal("expected regular camera to exist")
	}
	if regCam.IsGhost || regCam.DuplicateOf != "" {
		t.Errorf("expected regular camera not to be ghost or duplicate, got ghost=%v, dup=%s", regCam.IsGhost, regCam.DuplicateOf)
	}
}
