package live_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
)

// mockBroadcaster records emitted hex cells for testing.
type mockBroadcaster struct {
	mu    sync.Mutex
	cells []live.HexCell
}

func (m *mockBroadcaster) BroadcastHex(cell live.HexCell) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cells = append(m.cells, cell)
}

func (m *mockBroadcaster) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cells)
}

func (m *mockBroadcaster) latest() *live.HexCell {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cells) == 0 {
		return nil
	}
	c := m.cells[len(m.cells)-1]
	return &c
}

func sampleCameras() []camera.Camera {
	return []camera.Camera{
		{
			ID:          "cam-1",
			DisplayName: "Đường Lê Duẩn - Pasteur",
			Title:       "Lê Duẩn / Pasteur",
			H3Res7:      "8765b5662ffffff",
			H3Res9:      "8965b5662abffff",
		},
		{
			ID:          "cam-2",
			DisplayName: "Đường Nam Kỳ Khởi Nghĩa - Lê Lợi",
			Title:       "NKKN / Lê Lợi",
			H3Res7:      "8765b5662ffffff",
			H3Res9:      "8965b5662abffff",
		},
		{
			ID:          "cam-3",
			DisplayName: "Đường Đồng Khởi - Ngô Đức Kế",
			Title:       "Đồng Khởi / NĐK",
			H3Res7:      "8765b5662ffffff",
			H3Res9:      "8965b5662abffff",
		},
		{
			ID:          "cam-4",
			DisplayName: "Đường Tôn Đức Thắng - Cổng Ba Son",
			Title:       "Tôn Đức Thắng",
			H3Res7:      "8765b5662ffffff",
			H3Res9:      "8965b5662abffff",
		},
	}
}

func samplePolygons() map[string][][]float64 {
	return map[string][][]float64{
		"8765b5662ffffff": {
			{10.7782718, 106.7178091},
			{10.7916211, 106.7147297},
			{10.7956588, 106.7016189},
			{10.7863460, 106.6915878},
			{10.7729960, 106.6946685},
			{10.7689594, 106.7077791},
		},
	}
}

// 1. Cell with 2 cameras -> no event (below floor of 3).
func TestHexAggregator_BelowFloor_NoEvent(t *testing.T) {
	cams := sampleCameras()[:2] // cam-1, cam-2 only
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, samplePolygons(), 7, 3, broadcaster, nil)

	// Send 2 fresh non-mock verdicts
	v1 := hotset.Verdict{
		CameraID:   "cam-1",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.9,
		Source:     "vlm",
		Timestamp:  time.Now(),
	}
	cell1, ok1 := agg.ProcessVerdict(v1)
	if ok1 || cell1 != nil {
		t.Fatalf("expected no aggregate with 1 camera, got ok=%v, cell=%v", ok1, cell1)
	}

	v2 := hotset.Verdict{
		CameraID:   "cam-2",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.85,
		Source:     "vlm",
		Timestamp:  time.Now(),
	}
	cell2, ok2 := agg.ProcessVerdict(v2)
	if ok2 || cell2 != nil {
		t.Fatalf("expected no aggregate with 2 cameras (below floor of 3), got ok=%v, cell=%v", ok2, cell2)
	}

	if broadcaster.count() != 0 {
		t.Fatalf("expected 0 broadcast events, got %d", broadcaster.count())
	}
}

// 2. Cell with 3 cameras (1 stale) -> only 2 qualifying -> no event.
func TestHexAggregator_StaleExclusion_BelowFloor(t *testing.T) {
	cams := sampleCameras()[:3]
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, samplePolygons(), 7, 3, broadcaster, nil)

	// cam-1: fresh
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-1",
		Severity:   hotset.SeverityClear,
		Confidence: 0.9,
		Source:     "vlm",
		Stale:      false,
		Timestamp:  time.Now(),
	})

	// cam-2: fresh
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-2",
		Severity:   hotset.SeverityModerate,
		Confidence: 0.8,
		Source:     "vlm",
		Stale:      false,
		Timestamp:  time.Now(),
	})

	// cam-3: STALE
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:   "cam-3",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.95,
		Source:     "vlm",
		Stale:      true, // Stale!
		Timestamp:  time.Now().Add(-10 * time.Minute),
	})

	if ok || cell != nil {
		t.Fatalf("expected no event when 1 of 3 cameras is stale (only 2 qualifying), got ok=%v, cell=%v", ok, cell)
	}

	if broadcaster.count() != 0 {
		t.Fatalf("expected 0 broadcast events, got %d", broadcaster.count())
	}
}

// 2b. Mock verdict exclusion -> only fresh non-mock count.
func TestHexAggregator_MockExclusion_BelowFloor(t *testing.T) {
	cams := sampleCameras()[:3]
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, samplePolygons(), 7, 3, broadcaster, nil)

	// cam-1: fresh vlm
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-1",
		Severity:   hotset.SeverityClear,
		Confidence: 0.9,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	// cam-2: fresh vlm
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-2",
		Severity:   hotset.SeverityModerate,
		Confidence: 0.8,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	// cam-3: MOCK source
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:   "cam-3",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.99,
		Source:     "mock", // Mock source!
		Timestamp:  time.Now(),
	})

	if ok || cell != nil {
		t.Fatalf("expected no event when 1 of 3 cameras has source=mock, got ok=%v, cell=%v", ok, cell)
	}

	if broadcaster.count() != 0 {
		t.Fatalf("expected 0 broadcast events, got %d", broadcaster.count())
	}
}

// 3. Cell with 3 fresh cameras -> event with worst severity.
func TestHexAggregator_ThreeFreshCameras_EmitsEvent(t *testing.T) {
	cams := sampleCameras()[:3]
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, samplePolygons(), 7, 3, broadcaster, nil)

	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-1",
		Severity:   hotset.SeverityClear,
		Confidence: 0.9,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-2",
		Severity:   hotset.SeverityClear,
		Confidence: 0.85,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:   "cam-3",
		Severity:   hotset.SeverityClear,
		Confidence: 0.8,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	if !ok || cell == nil {
		t.Fatalf("expected cell event with 3 qualifying cameras, got ok=%v, cell=%v", ok, cell)
	}

	if cell.Severity != hotset.SeverityClear {
		t.Errorf("expected severity CLEAR, got %q", cell.Severity)
	}
	if cell.CameraCount != 3 {
		t.Errorf("expected camera_count=3, got %d", cell.CameraCount)
	}
	if cell.H3ID != "8765b5662ffffff" {
		t.Errorf("expected h3_id 8765b5662ffffff, got %q", cell.H3ID)
	}
	if cell.Resolution != 7 {
		t.Errorf("expected resolution 7, got %d", cell.Resolution)
	}
	if len(cell.Polygon) != 6 {
		t.Errorf("expected 6 polygon vertices, got %d", len(cell.Polygon))
	}
	if broadcaster.count() != 1 {
		t.Errorf("expected 1 broadcast event, got %d", broadcaster.count())
	}
}

// 4. Mixed severities -> worst-of is selected.
func TestHexAggregator_MixedSeverities_SelectsWorstOf(t *testing.T) {
	cams := sampleCameras()[:3]
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, samplePolygons(), 7, 3, broadcaster, nil)

	// cam-1: CLEAR
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-1",
		Severity:   hotset.SeverityClear,
		Confidence: 0.9,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	// cam-2: CONGESTED (worst!)
	agg.OnVerdict(hotset.Verdict{
		CameraID:   "cam-2",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.95,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	// cam-3: MODERATE
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:   "cam-3",
		Severity:   hotset.SeverityModerate,
		Confidence: 0.75,
		Source:     "vlm",
		Timestamp:  time.Now(),
	})

	if !ok || cell == nil {
		t.Fatalf("expected valid cell aggregate, got ok=%v", ok)
	}

	// Worst-of rule: CONGESTED must win over CLEAR and MODERATE
	if cell.Severity != hotset.SeverityCongested {
		t.Errorf("expected worst severity CONGESTED, got %q", cell.Severity)
	}
	if cell.WorstCamera != "cam-2" {
		t.Errorf("expected WorstCamera cam-2, got %q", cell.WorstCamera)
	}
	if cell.WorstCameraName != "Đường Nam Kỳ Khởi Nghĩa - Lê Lợi" {
		t.Errorf("expected WorstCameraName 'Đường Nam Kỳ Khởi Nghĩa - Lê Lợi', got %q", cell.WorstCameraName)
	}
	if cell.CameraCount != 3 {
		t.Errorf("expected CameraCount=3, got %d", cell.CameraCount)
	}
}

// 5. Severity precedence ordering test
func TestSeverityRank(t *testing.T) {
	ranks := map[string]int{
		hotset.SeverityCongested:      4,
		hotset.SeverityFloodedDeep:    4,
		hotset.SeverityHeavy:          3,
		hotset.SeverityFloodedShallow: 3,
		hotset.SeverityModerate:       2,
		hotset.SeverityWet:            2,
		hotset.SeverityClear:          1,
		hotset.SeverityDry:            1,
		hotset.SeverityUnknown:        0,
		"":                            0,
		"INVALID":                     0,
	}

	for sev, expected := range ranks {
		if r := live.SeverityRank(sev); r != expected {
			t.Errorf("SeverityRank(%q) = %d, expected %d", sev, r, expected)
		}
	}
}

// 6. SSE Hub BroadcastHex delivers hex_update event to connected clients.
func TestHub_BroadcastHex_DeliversHexUpdateEvent(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	resp := openSSE(t, srv.URL)
	defer resp.Body.Close()

	time.Sleep(30 * time.Millisecond)

	cell := live.HexCell{
		H3ID:            "8765b5662ffffff",
		Resolution:      7,
		Severity:        hotset.SeverityCongested,
		CameraCount:     3,
		WorstCamera:     "cam-2",
		WorstCameraName: "Đường Nam Kỳ Khởi Nghĩa - Lê Lợi",
		Polygon: [][]float64{
			{10.778, 106.717},
			{10.791, 106.714},
		},
	}
	hub.BroadcastHex(cell)

	ev, data, err := readEvent(resp, 2*time.Second)
	if err != nil {
		t.Fatalf("read SSE event: %v", err)
	}
	if ev != live.EventHexUpdate {
		t.Errorf("expected event %q, got %q", live.EventHexUpdate, ev)
	}

	var received live.HexCell
	if err := json.Unmarshal([]byte(data), &received); err != nil {
		t.Fatalf("failed to unmarshal hex_update payload: %v", err)
	}

	if received.H3ID != cell.H3ID {
		t.Errorf("expected H3ID %q, got %q", cell.H3ID, received.H3ID)
	}
	if received.Severity != cell.Severity {
		t.Errorf("expected Severity %q, got %q", cell.Severity, received.Severity)
	}
	if received.CameraCount != cell.CameraCount {
		t.Errorf("expected CameraCount %d, got %d", cell.CameraCount, received.CameraCount)
	}
	if received.WorstCamera != cell.WorstCamera {
		t.Errorf("expected WorstCamera %q, got %q", cell.WorstCamera, received.WorstCamera)
	}
	if received.WorstCameraName != cell.WorstCameraName {
		t.Errorf("expected WorstCameraName %q, got %q", cell.WorstCameraName, received.WorstCameraName)
	}
	if len(received.Polygon) != len(cell.Polygon) {
		t.Errorf("expected Polygon len %d, got %d", len(cell.Polygon), len(received.Polygon))
	}
}

// 7. LoadPolygons from static JSON file
func TestLoadPolygons(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "polygons.json")

	content := `{
		"8765b5662ffffff": [
			[10.7782718, 106.7178091],
			[10.7916211, 106.7147297]
		]
	}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	polys, err := live.LoadPolygons(filePath)
	if err != nil {
		t.Fatalf("LoadPolygons failed: %v", err)
	}
	if len(polys) != 1 {
		t.Fatalf("expected 1 polygon, got %d", len(polys))
	}
	pts, ok := polys["8765b5662ffffff"]
	if !ok || len(pts) != 2 {
		t.Fatalf("expected polygon with 2 points, got %v", pts)
	}

	// Non-existent file error
	_, err = live.LoadPolygons(filepath.Join(tmpDir, "non_existent.json"))
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

// 8. Resolution 9 grouping
func TestHexAggregator_Resolution9(t *testing.T) {
	cams := sampleCameras()[:3]
	broadcaster := &mockBroadcaster{}
	agg := live.NewHexAggregator(cams, nil, 9, 3, broadcaster, nil)

	agg.OnVerdict(hotset.Verdict{CameraID: "cam-1", Severity: "CLEAR", Source: "vlm"})
	agg.OnVerdict(hotset.Verdict{CameraID: "cam-2", Severity: "HEAVY", Source: "vlm"})
	cell, ok := agg.ProcessVerdict(hotset.Verdict{CameraID: "cam-3", Severity: "MODERATE", Source: "vlm"})

	if !ok || cell == nil {
		t.Fatalf("expected qualifying cell for res 9, got ok=%v", ok)
	}
	if cell.Resolution != 9 {
		t.Errorf("expected resolution 9, got %d", cell.Resolution)
	}
	if cell.H3ID != "8965b5662abffff" {
		t.Errorf("expected H3ID 8965b5662abffff, got %q", cell.H3ID)
	}
	if cell.Severity != hotset.SeverityHeavy {
		t.Errorf("expected worst severity HEAVY, got %q", cell.Severity)
	}
}
