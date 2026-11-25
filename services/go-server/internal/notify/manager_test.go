package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockChannel struct {
	mu           sync.Mutex
	name         string
	sent         []FloodAlert
	sentCorridor []CorridorAlert
	errToRet     error
}

func newMockChannel(name string) *mockChannel {
	return &mockChannel{name: name}
}

func (m *mockChannel) Name() string {
	return m.name
}

func (m *mockChannel) Send(ctx context.Context, alert FloodAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, alert)
	return m.errToRet
}

func (m *mockChannel) SentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func (m *mockChannel) SendCorridor(ctx context.Context, alert CorridorAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentCorridor = append(m.sentCorridor, alert)
	return m.errToRet
}

func (m *mockChannel) SentCorridorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sentCorridor)
}

func TestManager_Dispatch_SeverityFiltering(t *testing.T) {
	debouncer := NewDebouncer(30 * time.Minute)
	mgr := NewManager(debouncer)
	ch := newMockChannel("mock-1")
	mgr.Register(ch)

	// 1. Non-actionable severities (DRY, WET) MUST be skipped
	dryAlert := FloodAlert{CameraID: "cam-dry", FloodSeverity: "DRY"}
	if err := mgr.Dispatch(context.Background(), dryAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wetAlert := FloodAlert{CameraID: "cam-wet", FloodSeverity: "WET"}
	if err := mgr.Dispatch(context.Background(), wetAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ch.SentCount() != 0 {
		t.Fatalf("expected 0 dispatches for DRY/WET, got %d", ch.SentCount())
	}

	// 2. Actionable severities MUST be dispatched
	shallowAlert := FloodAlert{CameraID: "cam-shallow", FloodSeverity: "FLOODED_SHALLOW"}
	if err := mgr.Dispatch(context.Background(), shallowAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 1 {
		t.Fatalf("expected 1 dispatch for FLOODED_SHALLOW, got %d", ch.SentCount())
	}

	deepAlert := FloodAlert{CameraID: "cam-deep", FloodSeverity: "FLOODED_DEEP"}
	if err := mgr.Dispatch(context.Background(), deepAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 2 {
		t.Fatalf("expected 2 dispatches for FLOODED_DEEP, got %d", ch.SentCount())
	}
}

func TestManager_Dispatch_Debouncing(t *testing.T) {
	debouncer := NewDebouncer(30 * time.Minute)
	mgr := NewManager(debouncer)
	ch := newMockChannel("mock-1")
	mgr.Register(ch)

	t0 := time.Now()
	alert := FloodAlert{
		CameraID:      "cam-debounce",
		FloodSeverity: "FLOODED_DEEP",
		ObservedAt:    t0,
	}

	// First alert: dispatched
	if err := mgr.Dispatch(context.Background(), alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 1 {
		t.Fatalf("expected 1 dispatch, got %d", ch.SentCount())
	}

	// Second alert at T+5m for same camera: suppressed by debounce window
	alert2 := FloodAlert{
		CameraID:      "cam-debounce",
		FloodSeverity: "FLOODED_DEEP",
		ObservedAt:    t0.Add(5 * time.Minute),
	}
	if err := mgr.Dispatch(context.Background(), alert2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 1 {
		t.Fatalf("expected dispatch count to remain 1 (suppressed), got %d", ch.SentCount())
	}

	// Alert for a DIFFERENT camera: dispatched
	alertDiffCam := FloodAlert{
		CameraID:      "cam-other",
		FloodSeverity: "FLOODED_DEEP",
		ObservedAt:    t0.Add(6 * time.Minute),
	}
	if err := mgr.Dispatch(context.Background(), alertDiffCam); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 2 {
		t.Fatalf("expected dispatch count to become 2 for different camera, got %d", ch.SentCount())
	}

	// Alert for first camera at T+31m: past 30m window, dispatched
	alert3 := FloodAlert{
		CameraID:      "cam-debounce",
		FloodSeverity: "FLOODED_DEEP",
		ObservedAt:    t0.Add(31 * time.Minute),
	}
	if err := mgr.Dispatch(context.Background(), alert3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 3 {
		t.Fatalf("expected dispatch count to become 3 after 30m quiet period, got %d", ch.SentCount())
	}
}

func TestManager_Dispatch_ChannelErrorAggregation(t *testing.T) {
	debouncer := NewDebouncer(30 * time.Minute)
	mgr := NewManager(debouncer)

	ch1 := newMockChannel("ch-good")
	ch2 := newMockChannel("ch-bad")
	ch2.errToRet = errors.New("network failure")

	mgr.Register(ch1)
	mgr.Register(ch2)

	alert := FloodAlert{
		CameraID:      "cam-err",
		FloodSeverity: "FLOODED_SHALLOW",
	}

	err := mgr.Dispatch(context.Background(), alert)
	if err == nil {
		t.Fatalf("expected error from ch-bad")
	}

	// ch1 should still have received the alert despite ch2 failing
	if ch1.SentCount() != 1 {
		t.Fatalf("expected ch-good to have sent 1 alert, got %d", ch1.SentCount())
	}
}

func TestManager_DispatchCorridor_FilteringAndDebounce(t *testing.T) {
	mgr := NewManager(nil)
	ch := newMockChannel("mock-corridor")
	mgr.Register(ch)

	t0 := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)

	// 1. Non-hazard status (CLEAR, 5cm depth) MUST be skipped
	d5 := 5.0
	clearAlert := CorridorAlert{
		RoadKey:     "vo_van_kiet",
		Passability: "CLEAR",
		MaxDepthCm:  &d5,
		ObservedAt:  t0,
	}
	if err := mgr.DispatchCorridor(context.Background(), clearAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCorridorCount() != 0 {
		t.Fatalf("expected 0 corridor dispatches for CLEAR, got %d", ch.SentCorridorCount())
	}

	// 2. Actionable hazard status (IMPASSABLE, 35cm depth) at t0 MUST be dispatched
	d35 := 35.0
	hazardAlert1 := CorridorAlert{
		RoadKey:            "vo_van_kiet",
		RoadName:           "Đại lộ Võ Văn Kiệt",
		Passability:        "IMPASSABLE",
		WorstFloodSeverity: "FLOODED_DEEP",
		MaxDepthCm:         &d35,
		ObservedAt:         t0,
	}
	if err := mgr.DispatchCorridor(context.Background(), hazardAlert1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCorridorCount() != 1 {
		t.Fatalf("expected 1 corridor dispatch, got %d", ch.SentCorridorCount())
	}

	// 3. Second event for same corridor at t0 + 2m: MUST be suppressed by 15-minute debounce
	hazardAlert2 := CorridorAlert{
		RoadKey:            "vo_van_kiet",
		RoadName:           "Đại lộ Võ Văn Kiệt",
		Passability:        "IMPASSABLE",
		WorstFloodSeverity: "FLOODED_DEEP",
		MaxDepthCm:         &d35,
		ObservedAt:         t0.Add(2 * time.Minute),
	}
	if err := mgr.DispatchCorridor(context.Background(), hazardAlert2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCorridorCount() != 1 {
		t.Fatalf("expected corridor count to remain 1 due to 15m debounce, got %d", ch.SentCorridorCount())
	}

	// 4. Event for a different corridor (mai_chi_tho) at t0 + 3m: MUST be dispatched
	d25 := 25.0
	diffCorridorAlert := CorridorAlert{
		RoadKey:            "mai_chi_tho",
		RoadName:           "Đường Mai Chí Thọ",
		Passability:        "CAUTION",
		WorstFloodSeverity: "FLOODED_SHALLOW",
		MaxDepthCm:         &d25,
		ObservedAt:         t0.Add(3 * time.Minute),
	}
	if err := mgr.DispatchCorridor(context.Background(), diffCorridorAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCorridorCount() != 2 {
		t.Fatalf("expected corridor count to become 2 for different corridor, got %d", ch.SentCorridorCount())
	}

	// 5. Third event for vo_van_kiet at t0 + 16m: past 15-minute quiet period, MUST be dispatched
	hazardAlert3 := CorridorAlert{
		RoadKey:            "vo_van_kiet",
		RoadName:           "Đại lộ Võ Văn Kiệt",
		Passability:        "IMPASSABLE",
		WorstFloodSeverity: "FLOODED_DEEP",
		MaxDepthCm:         &d35,
		ObservedAt:         t0.Add(16 * time.Minute),
	}
	if err := mgr.DispatchCorridor(context.Background(), hazardAlert3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCorridorCount() != 3 {
		t.Fatalf("expected corridor count to become 3 after 15m debounce window, got %d", ch.SentCorridorCount())
	}
}

func TestManager_Dispatch_EscalationBypassesDebounce(t *testing.T) {
	mgr := NewManager(nil)
	ch := newMockChannel("mock-ch")
	mgr.Register(ch)

	t0 := time.Now()

	// 1. Initial shallow flood
	shallowAlert := FloodAlert{
		CameraID:      "cam-esc",
		FloodSeverity: "FLOODED_SHALLOW",
		ObservedAt:    t0,
	}
	if err := mgr.Dispatch(context.Background(), shallowAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 1 {
		t.Fatalf("expected 1 dispatch for initial shallow flood, got %d", ch.SentCount())
	}

	// 2. Repeat shallow flood at T+3m: suppressed by debounce window
	shallowRepeat := FloodAlert{
		CameraID:      "cam-esc",
		FloodSeverity: "FLOODED_SHALLOW",
		ObservedAt:    t0.Add(3 * time.Minute),
	}
	if err := mgr.Dispatch(context.Background(), shallowRepeat); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 1 {
		t.Fatalf("expected repeat shallow flood to be suppressed, got %d", ch.SentCount())
	}

	// 3. Escalation to FLOODED_DEEP at T+5m: MUST bypass debounce and dispatch immediately
	deepAlert := FloodAlert{
		CameraID:      "cam-esc",
		FloodSeverity: "FLOODED_DEEP",
		ObservedAt:    t0.Add(5 * time.Minute),
	}
	if err := mgr.Dispatch(context.Background(), deepAlert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.SentCount() != 2 {
		t.Fatalf("expected deep escalation to bypass debounce and dispatch (count 2), got %d", ch.SentCount())
	}
}
