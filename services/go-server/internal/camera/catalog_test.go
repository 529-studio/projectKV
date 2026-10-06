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

func TestCatalog_Deduplication(t *testing.T) {
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

	// Ghost state is no longer hardcoded in the catalog: the static liveness
	// (isOnline/status) must be preserved verbatim from the JSON catalog.
	ghostCam, ok := cat.ByID("595ddbbf3dcfc400106f28b8")
	if !ok {
		t.Fatal("expected ghost camera to exist")
	}
	if ghostCam.IsGhost {
		t.Error("expected IsGhost to remain false; catalog no longer hardcodes ghost state")
	}
	if !ghostCam.IsOnline || ghostCam.Status != "UP" {
		t.Errorf("expected catalog-provided state to be preserved, got isOnline=%v status=%s",
			ghostCam.IsOnline, ghostCam.Status)
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

func TestCatalog_Phuongs(t *testing.T) {
	cams := []Camera{
		{ID: "c1", PhuongName: "Bến Nghé", PhuongType: "Phường"},
		{ID: "c2", PhuongName: "Tân Định", PhuongType: "Phường"},
		{ID: "c3", PhuongName: "Bến Nghé", PhuongType: "Phường"}, // duplicate should be deduplicated
		{ID: "c4", PhuongName: ""},                               // empty should be excluded
	}

	cat := NewCatalog(cams)
	phuongs := cat.Phuongs()

	expected := []Phuong{
		{Type: "Phường", Name: "Bến Nghé"},
		{Type: "Phường", Name: "Tân Định"},
	}
	if !reflect.DeepEqual(phuongs, expected) {
		t.Errorf("expected phuongs %v, got %v", expected, phuongs)
	}
}

func TestCatalog_FilterByPhuong(t *testing.T) {
	cams := []Camera{
		{ID: "c1", DisplayName: "Cam 1", PhuongName: "Bến Nghé", PhuongType: "Phường"},
		{ID: "c2", DisplayName: "Cam 2", PhuongName: "Tân Định", PhuongType: "Phường"},
		{ID: "c3", DisplayName: "Cam 3", PhuongName: "Bến Nghé", PhuongType: "Phường"},
		{ID: "c4", DisplayName: "Cam 4", PhuongName: ""},
	}

	cat := NewCatalog(cams)

	// Filter case-insensitive (prefix-less)
	matches := cat.FilterByPhuong("bến nghé")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for bến nghé, got %d", len(matches))
	}
	if matches[0].ID != "c1" || matches[1].ID != "c3" {
		t.Errorf("unexpected matches: %v", matches)
	}

	// Filter with prefix
	matchesPrefix := cat.FilterByPhuong("phường bến nghé")
	if len(matchesPrefix) != 2 {
		t.Fatalf("expected 2 matches for phường bến nghé, got %d", len(matchesPrefix))
	}

	// Filter empty string
	emptyMatches := cat.FilterByPhuong("")
	if len(emptyMatches) != 0 {
		t.Errorf("expected 0 matches for empty string, got %d", len(emptyMatches))
	}

	// Filter non-existent
	noMatches := cat.FilterByPhuong("Không tồn tại")
	if len(noMatches) != 0 {
		t.Errorf("expected 0 matches for non-existent phuong, got %d", len(noMatches))
	}
}

func TestCamera_JSONSerialization_PhuongFields(t *testing.T) {
	pID := "P_001"

	camWithPhuong := Camera{
		ID:          "cam-with-p",
		DisplayName: "Nguyễn Huệ",
		PhuongID:    &pID,
		PhuongType:  "Phường",
		PhuongName:  "Bến Nghé",
		RoadType:    "Đường",
		RoadName:    "Nguyễn Huệ",
	}

	camWithoutPhuong := Camera{
		ID:          "cam-without-p",
		DisplayName: "Quốc lộ 1",
		PhuongID:    nil,
	}

	// 1. Serialization of camera with Phường and Road fields
	dataWith, err := json.Marshal(camWithPhuong)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var mapWith map[string]interface{}
	if err := json.Unmarshal(dataWith, &mapWith); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if mapWith["phuong_id"] != "P_001" || mapWith["phuong_name"] != "Bến Nghé" || mapWith["phuong_type"] != "Phường" {
		t.Errorf("expected phuong fields in json, got %v", mapWith)
	}
	if mapWith["road_type"] != "Đường" || mapWith["road_name"] != "Nguyễn Huệ" {
		t.Errorf("expected road fields in json, got %v", mapWith)
	}

	// 2. Serialization and deserialization of camera without Phường
	dataWithout, err := json.Marshal(camWithoutPhuong)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var mapWithout map[string]interface{}
	if err := json.Unmarshal(dataWithout, &mapWithout); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, ok := mapWithout["phuong_name"]; ok {
		t.Errorf("expected phuong_name to be omitted, got %v", mapWithout["phuong_name"])
	}
	if _, ok := mapWithout["phuong_type"]; ok {
		t.Errorf("expected phuong_type to be omitted, got %v", mapWithout["phuong_type"])
	}
	var unmarshaled Camera
	if err := json.Unmarshal(dataWithout, &unmarshaled); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if unmarshaled.PhuongID != nil {
		t.Errorf("expected nil PhuongID, got %v", *unmarshaled.PhuongID)
	}
	if unmarshaled.PhuongName != "" {
		t.Errorf("expected empty PhuongName, got %v", unmarshaled.PhuongName)
	}
	if unmarshaled.PhuongType != "" {
		t.Errorf("expected empty PhuongType, got %v", unmarshaled.PhuongType)
	}
}

func TestCamera_JSONSerialization_H3Fields(t *testing.T) {
	camWithH3 := Camera{
		ID:          "cam-with-h3",
		DisplayName: "Nguyễn Huệ",
		H3Res7:      "8765b5662ffffff",
		H3Res9:      "8965b5662abffff",
	}

	camWithoutH3 := Camera{
		ID:          "cam-without-h3",
		DisplayName: "Quốc lộ 1",
	}

	// 1. Serialization with H3
	dataWith, err := json.Marshal(camWithH3)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var mapWith map[string]interface{}
	if err := json.Unmarshal(dataWith, &mapWith); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if mapWith["h3_res7"] != "8765b5662ffffff" || mapWith["h3_res9"] != "8965b5662abffff" {
		t.Errorf("expected H3 fields in json, got %v", mapWith)
	}

	// 2. Serialization without H3 (omitempty)
	dataWithout, err := json.Marshal(camWithoutH3)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var mapWithout map[string]interface{}
	if err := json.Unmarshal(dataWithout, &mapWithout); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, ok := mapWithout["h3_res7"]; ok {
		t.Errorf("expected h3_res7 to be omitted when empty, got %v", mapWithout["h3_res7"])
	}
	if _, ok := mapWithout["h3_res9"]; ok {
		t.Errorf("expected h3_res9 to be omitted when empty, got %v", mapWithout["h3_res9"])
	}

	// 3. Deserialization of null H3 fields
	jsonWithNull := []byte(`{"id": "cam-null", "h3_res7": null, "h3_res9": null}`)
	var unmarshaledNull Camera
	if err := json.Unmarshal(jsonWithNull, &unmarshaledNull); err != nil {
		t.Fatalf("unmarshal error with null H3: %v", err)
	}
	if unmarshaledNull.H3Res7 != "" || unmarshaledNull.H3Res9 != "" {
		t.Errorf("expected empty string for null H3 fields, got res7=%q, res9=%q", unmarshaledNull.H3Res7, unmarshaledNull.H3Res9)
	}
}

func TestLoadCatalog_GracefulFallback(t *testing.T) {
	tempDir := t.TempDir()

	// Create base cameras_all.json in tempDir
	baseCameras := []Camera{
		{ID: "base-1", DisplayName: "Base Camera 1"},
	}
	baseJSON, _ := json.Marshal(baseCameras)
	basePath := filepath.Join(tempDir, "cameras_all.json")
	if err := os.WriteFile(basePath, baseJSON, 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}

	// Request missing enriched file in same directory
	missingEnrichedPath := filepath.Join(tempDir, "cameras_enriched.json")
	cat, err := LoadCatalog(missingEnrichedPath)
	if err != nil {
		t.Fatalf("expected graceful fallback to cameras_all.json without error, got: %v", err)
	}

	cams := cat.All()
	if len(cams) != 1 || cams[0].ID != "base-1" {
		t.Errorf("expected 1 camera from base catalog, got %v", cams)
	}
	if cams[0].PhuongID != nil || cams[0].PhuongName != "" {
		t.Errorf("expected empty phuong fields in fallback, got id=%v name=%v", cams[0].PhuongID, cams[0].PhuongName)
	}
}
