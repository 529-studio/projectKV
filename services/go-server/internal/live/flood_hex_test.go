package live

import (
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

type mockFloodBroadcaster struct {
	mu    sync.Mutex
	cells []FloodHexCell
}

func (m *mockFloodBroadcaster) BroadcastFloodHex(cell FloodHexCell) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cells = append(m.cells, cell)
}

func (m *mockFloodBroadcaster) getCells() []FloodHexCell {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]FloodHexCell, len(m.cells))
	copy(out, m.cells)
	return out
}

func TestFloodSeverityRank(t *testing.T) {
	cases := []struct {
		sev  string
		want int
	}{
		{hotset.SeverityFloodedDeep, 3},
		{hotset.SeverityFloodedShallow, 2},
		{hotset.SeverityWet, 1},
		{hotset.SeverityDry, 0},
		{hotset.SeverityUnknown, 0},
		{hotset.SeverityClear, 0},
		{hotset.SeverityCongested, 0},
	}

	for _, tc := range cases {
		got := FloodSeverityRank(tc.sev)
		if got != tc.want {
			t.Errorf("FloodSeverityRank(%q) = %d, want %d", tc.sev, got, tc.want)
		}
	}
}

func TestFloodHexAggregator_SafetyFloor_OneCamera(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-1", DisplayName: "Cau Rach Chiec", H3Res7: "87654321fffffff"},
	}
	polys := map[string][][]float64{
		"87654321fffffff": {{10.8, 106.7}, {10.81, 106.71}},
	}
	broadcaster := &mockFloodBroadcaster{}
	agg := NewFloodHexAggregator(cams, polys, 7, 1, broadcaster, nil)

	// 1. Dry verdict -> rank 0 -> no event
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-1",
		FloodSeverity: hotset.SeverityDry,
		Source:        "vlm",
	})
	if ok || cell != nil {
		t.Fatalf("expected DRY verdict to produce no qualifying flood cell")
	}

	// 2. Active flood verdict -> rank 2 -> produces event (floor >= 1 met)
	cell, ok = agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-1",
		FloodSeverity: hotset.SeverityFloodedShallow,
		Source:        "vlm",
	})
	if !ok || cell == nil {
		t.Fatalf("expected FLOODED_SHALLOW to qualify with safety floor >= 1")
	}
	if cell.FloodSeverity != hotset.SeverityFloodedShallow {
		t.Errorf("expected FloodSeverity %s, got %s", hotset.SeverityFloodedShallow, cell.FloodSeverity)
	}
	if cell.WorstCamera != "cam-1" {
		t.Errorf("expected WorstCamera cam-1, got %s", cell.WorstCamera)
	}
	if cell.WorstCameraName != "Cau Rach Chiec" {
		t.Errorf("expected WorstCameraName Cau Rach Chiec, got %s", cell.WorstCameraName)
	}

	cells := broadcaster.getCells()
	if len(cells) != 1 {
		t.Fatalf("expected 1 broadcast event, got %d", len(cells))
	}
}

func TestFloodHexAggregator_WorstOfPrecedence(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-1", DisplayName: "Diem 1", H3Res7: "87654321fffffff"},
		{ID: "cam-2", DisplayName: "Diem 2", H3Res7: "87654321fffffff"},
		{ID: "cam-3", DisplayName: "Diem 3", H3Res7: "87654321fffffff"},
	}
	broadcaster := &mockFloodBroadcaster{}
	agg := NewFloodHexAggregator(cams, nil, 7, 1, broadcaster, nil)

	agg.OnVerdict(hotset.Verdict{CameraID: "cam-1", FloodSeverity: hotset.SeverityWet, Source: "vlm"})
	agg.OnVerdict(hotset.Verdict{CameraID: "cam-2", FloodSeverity: hotset.SeverityFloodedDeep, Source: "vlm"})
	cell, ok := agg.ProcessVerdict(hotset.Verdict{CameraID: "cam-3", FloodSeverity: hotset.SeverityFloodedShallow, Source: "vlm"})

	if !ok || cell == nil {
		t.Fatalf("expected cell to qualify")
	}
	if cell.FloodSeverity != hotset.SeverityFloodedDeep {
		t.Errorf("expected worst-of severity %s, got %s", hotset.SeverityFloodedDeep, cell.FloodSeverity)
	}
	if cell.WorstCamera != "cam-2" {
		t.Errorf("expected worst camera cam-2, got %s", cell.WorstCamera)
	}
	if cell.CameraCount != 3 {
		t.Errorf("expected CameraCount 3, got %d", cell.CameraCount)
	}
}

func TestFloodHexAggregator_StaleAndMockExclusion(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-1", H3Res7: "87654321fffffff"},
	}
	broadcaster := &mockFloodBroadcaster{}
	agg := NewFloodHexAggregator(cams, nil, 7, 1, broadcaster, nil)

	// Mock verdict rejected by default
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-1",
		FloodSeverity: hotset.SeverityFloodedDeep,
		Source:        "mock",
	})
	if ok || cell != nil {
		t.Errorf("expected mock verdict to be rejected when allowMock=false")
	}

	// Enable allowMock
	agg.SetAllowMock(true)
	cell, ok = agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-1",
		FloodSeverity: hotset.SeverityFloodedDeep,
		Source:        "mock",
	})
	if !ok || cell == nil {
		t.Fatalf("expected mock verdict to qualify when allowMock=true")
	}

	// Stale verdict rejected even with allowMock
	cell, ok = agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-1",
		FloodSeverity: hotset.SeverityFloodedDeep,
		Source:        "mock",
		Stale:         true,
	})
	if ok || cell != nil {
		t.Errorf("expected stale verdict to be rejected")
	}
}

func TestFloodHexAggregator_ObservationTTL_Reaping(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-ttl-1", DisplayName: "TTL Bridge", H3Res7: "87654321fffffff"},
	}
	broadcaster := &mockFloodBroadcaster{}
	agg := NewFloodHexAggregator(cams, nil, 7, 1, broadcaster, nil)
	agg.SetStaleTTL(180 * time.Second)

	// 1. Verdict observed 5 minutes ago -> older than 180s TTL
	oldObs := time.Now().Add(-5 * time.Minute)
	cell, ok := agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-ttl-1",
		FloodSeverity: hotset.SeverityFloodedShallow,
		Source:        "vlm",
		ObservedAt:    oldObs,
		Stale:         false,
	})
	if ok || cell != nil {
		t.Fatalf("expected observation older than TTL to be excluded")
	}

	cells := agg.GetAllCells()
	if len(cells) != 0 {
		t.Fatalf("expected 0 cells for expired observations, got %d", len(cells))
	}

	// 2. Verdict observed 5 seconds ago -> fresh -> qualifies
	freshObs := time.Now().Add(-5 * time.Second)
	cell, ok = agg.ProcessVerdict(hotset.Verdict{
		CameraID:      "cam-ttl-1",
		FloodSeverity: hotset.SeverityFloodedDeep,
		Source:        "vlm",
		ObservedAt:    freshObs,
		Stale:         false,
	})
	if !ok || cell == nil {
		t.Fatalf("expected fresh observation within TTL to qualify")
	}
	if cell.FloodSeverity != hotset.SeverityFloodedDeep {
		t.Errorf("expected FLOODED_DEEP, got %s", cell.FloodSeverity)
	}

	cells = agg.GetAllCells()
	if len(cells) != 1 {
		t.Fatalf("expected 1 active cell, got %d", len(cells))
	}

	// 3. Shorten TTL to 1 second, sleep 10ms, test ReapStaleCells
	agg.SetStaleTTL(1 * time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	cleared := agg.ReapStaleCells()
	if len(cleared) != 1 || cleared[0] != "87654321fffffff" {
		t.Fatalf("expected ReapStaleCells to clear 87654321fffffff, got %v", cleared)
	}

	cells = agg.GetAllCells()
	if len(cells) != 0 {
		t.Fatalf("expected 0 cells after reaping, got %d", len(cells))
	}

	broadcasts := broadcaster.getCells()
	// Should contain the fresh event and then the dry clearance event
	lastEvt := broadcasts[len(broadcasts)-1]
	if lastEvt.CameraCount != 0 || lastEvt.FloodSeverity != hotset.SeverityDry {
		t.Errorf("expected clearance broadcast with CameraCount 0 and DRY, got count=%d sev=%s", lastEvt.CameraCount, lastEvt.FloodSeverity)
	}
}

func TestFloodHexAggregator_OnUserReport_EscalationAndTTL(t *testing.T) {
	cams := []camera.Camera{
		{ID: "cam-usr-1", DisplayName: "Nga 4 Hang Xanh", H3Res7: "87654321fffffff"},
	}
	polys := map[string][][]float64{
		"87654321fffffff": {{10.8, 106.7}, {10.81, 106.71}},
	}
	broadcaster := &mockFloodBroadcaster{}
	agg := NewFloodHexAggregator(cams, polys, 7, 1, broadcaster, nil)

	now := time.Now()

	// 1. First report from IP 1 -> reportCount=1, escalated=false
	count, escalated := agg.OnUserReport("cam-usr-1", "hash_ip_1", now)
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	if escalated {
		t.Errorf("expected escalated=false with single reporter")
	}

	// 2. Second report from same IP within 10m -> reportCount=2, escalated=false (still 1 distinct IP)
	count, escalated = agg.OnUserReport("cam-usr-1", "hash_ip_1", now.Add(2*time.Minute))
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}
	if escalated {
		t.Errorf("expected escalated=false with same IP repeated")
	}

	// 3. Third report from distinct IP 2 within 10m -> reportCount=3, escalated=true (>= 2 distinct IPs)
	count, escalated = agg.OnUserReport("cam-usr-1", "hash_ip_2", now.Add(3*time.Minute))
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
	if !escalated {
		t.Errorf("expected escalated=true with 2 distinct IPs within 10m")
	}

	// 4. Verify GetActiveUserReports returns active summary
	summaries := agg.GetActiveUserReports()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 active user report summary, got %d", len(summaries))
	}
	if summaries[0].CameraID != "cam-usr-1" {
		t.Errorf("expected CameraID cam-usr-1, got %s", summaries[0].CameraID)
	}
	if !summaries[0].Escalated {
		t.Errorf("expected Escalated=true in summary")
	}

	// 5. Test 10-minute window escalation expiry:
	// A new aggregator where IP 1 reported 12 minutes ago, and IP 2 reports now.
	// IP 1 is outside the 10-minute escalation window -> escalated should be false!
	agg2 := NewFloodHexAggregator(cams, polys, 7, 1, broadcaster, nil)
	agg2.OnUserReport("cam-usr-1", "hash_old_ip", now.Add(-12*time.Minute))
	count, escalated = agg2.OnUserReport("cam-usr-1", "hash_new_ip", now)
	if count != 2 { // both within 15m sliding TTL
		t.Errorf("expected count=2 within 15m TTL, got %d", count)
	}
	if escalated {
		t.Errorf("expected escalated=false because hash_old_ip is outside the 10-minute escalation window")
	}

	// 6. Test 15-minute sliding TTL pruning
	agg3 := NewFloodHexAggregator(cams, polys, 7, 1, broadcaster, nil)
	agg3.OnUserReport("cam-usr-1", "hash_expired", now.Add(-16*time.Minute))
	count, escalated = agg3.OnUserReport("cam-usr-1", "hash_fresh", now)
	if count != 1 {
		t.Errorf("expected count=1 after pruning >15m report, got %d", count)
	}
	if escalated {
		t.Errorf("expected escalated=false")
	}
}

