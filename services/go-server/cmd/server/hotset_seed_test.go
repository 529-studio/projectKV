package main

import (
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
)

type dummyCameraReader struct {
	cameras []camera.Camera
}

func (d *dummyCameraReader) All() []camera.Camera                         { return d.cameras }
func (d *dummyCameraReader) ByID(id string) (*camera.Camera, bool)        { return nil, false }
func (d *dummyCameraReader) Zones() []string                              { return nil }
func (d *dummyCameraReader) Phuongs() []camera.Phuong                     { return nil }
func (d *dummyCameraReader) Filter(zone, corridor string) []camera.Camera { return nil }
func (d *dummyCameraReader) FilterByPhuong(phuongName string) []camera.Camera {
	return nil
}

func TestBuildFixedHotSet_NilReader(t *testing.T) {
	res := buildFixedHotSet(nil, 40)
	if res != nil {
		t.Errorf("expected nil for nil reader, got %v", res)
	}
}

func TestBuildFixedHotSet_EmptyReader(t *testing.T) {
	res := buildFixedHotSet(&dummyCameraReader{cameras: []camera.Camera{}}, 40)
	if res != nil {
		t.Errorf("expected nil for empty reader, got %v", res)
	}
}

func TestBuildFixedHotSet_FiltersOfflineAndGhosts(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-offline", Title: "An Sương 1", IsOnline: false},
		{ID: "cam-ghost", Title: "An Sương 2", IsOnline: true, IsGhost: true},
		{ID: "cam-dup", Title: "An Sương 3", IsOnline: true, DuplicateOf: "cam-master"},
		{ID: "cam-valid", Title: "An Sương 4", IsOnline: true, Zone: "Zone A", SnapshotURL: "http://example.com/snap.jpg"},
	}

	reader := &dummyCameraReader{cameras: cams}
	res := buildFixedHotSet(reader, 10)

	if len(res) != 1 {
		t.Fatalf("expected 1 valid camera, got %d", len(res))
	}
	if res[0].ID != "cam-valid" {
		t.Errorf("expected cam-valid, got %s", res[0].ID)
	}
	if res[0].Name != "An Sương 4" {
		t.Errorf("expected name 'An Sương 4', got %s", res[0].Name)
	}
	if res[0].Zone != "Zone A" {
		t.Errorf("expected zone 'Zone A', got %s", res[0].Zone)
	}
	if res[0].SnapshotURL != "http://example.com/snap.jpg" {
		t.Errorf("expected snapshot URL 'http://example.com/snap.jpg', got %s", res[0].SnapshotURL)
	}
}

func TestBuildFixedHotSet_PrioritisesCriticalKeywordsAndPads(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-regular-1", Title: "Đường Thường 1", IsOnline: true},
		{ID: "cam-regular-2", Title: "Đường Thường 2", IsOnline: true},
		{ID: "cam-an-suong", Title: "Nút giao An Sương", IsOnline: true},
		{ID: "cam-cat-lai", Title: "Cảng Cát Lái", IsOnline: true},
		{ID: "cam-regular-3", Title: "Đường Thường 3", IsOnline: true},
	}

	reader := &dummyCameraReader{cameras: cams}
	res := buildFixedHotSet(reader, 3)

	if len(res) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(res))
	}

	// First two must be critical choke points (An Sương, Cát Lái)
	if res[0].ID != "cam-an-suong" {
		t.Errorf("expected first camera to be cam-an-suong, got %s", res[0].ID)
	}
	if res[1].ID != "cam-cat-lai" {
		t.Errorf("expected second camera to be cam-cat-lai, got %s", res[1].ID)
	}
	// Third should be padded from regular cameras
	if res[2].ID != "cam-regular-1" {
		t.Errorf("expected third camera to be cam-regular-1, got %s", res[2].ID)
	}
}

func TestBuildFixedHotSet_PrioritisesS1PriorityCorridorIntersections(t *testing.T) {
	cams := []camera.Camera{
		// 1. Regular camera
		{ID: "cam-regular", Title: "Đường Thường", IsOnline: true},
		// 2. Keyword camera, but not intersection (mid-block or single road)
		{ID: "cam-an-suong-kw", Title: "Nút giao An Sương", IsOnline: true, RoadKeys: []string{"quoc_lo_1"}},
		// 3. Priority corridor intersection (vo_van_kiet, 2 keys) -> should be #1 priority
		{ID: "cam-vvk-intersection", Title: "Võ Văn Kiệt - An Bình", IsOnline: true, RoadKeys: []string{"vo_van_kiet", "an_binh"}},
		// 4. Priority corridor intersection (cong_hoa, 2 keys) -> should be #2 priority
		{ID: "cam-cong-hoa-intersection", Title: "Cộng Hòa - Út Tịch", IsOnline: true, RoadKeys: []string{"cong_hoa", "ut_tich"}},
		// 5. Priority corridor mid-block (len == 1) -> does not qualify for Pass 1, but matches criticalKeywords in Pass 2
		{ID: "cam-vvk-midblock", Title: "Võ Văn Kiệt giữa cầu", IsOnline: true, RoadKeys: []string{"vo_van_kiet"}},
	}

	reader := &dummyCameraReader{cameras: cams}
	res := buildFixedHotSet(reader, 3)

	if len(res) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(res))
	}

	// First two MUST be the 2-key intersection cameras on priority corridors
	if res[0].ID != "cam-vvk-intersection" {
		t.Errorf("expected first camera to be cam-vvk-intersection, got %s", res[0].ID)
	}
	if res[1].ID != "cam-cong-hoa-intersection" {
		t.Errorf("expected second camera to be cam-cong-hoa-intersection, got %s", res[1].ID)
	}

	// Third is keyword match from Pass 2 (An Sương or VVK midblock)
	if res[2].ID != "cam-an-suong-kw" && res[2].ID != "cam-vvk-midblock" {
		t.Errorf("expected third camera to be a keyword match, got %s", res[2].ID)
	}
}
