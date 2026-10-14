package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockChannel struct {
	mu       sync.Mutex
	name     string
	sent     []FloodAlert
	errToRet error
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
