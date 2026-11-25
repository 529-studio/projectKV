package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/corridor"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/notify"
)

type transitionMockChannel struct {
	mu             sync.Mutex
	floodAlerts    []notify.FloodAlert
	corridorAlerts []notify.CorridorAlert
}

func (m *transitionMockChannel) Name() string {
	return "mock-transition"
}

func (m *transitionMockChannel) Send(ctx context.Context, alert notify.FloodAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.floodAlerts = append(m.floodAlerts, alert)
	return nil
}

func (m *transitionMockChannel) SendCorridor(ctx context.Context, alert notify.CorridorAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.corridorAlerts = append(m.corridorAlerts, alert)
	return nil
}

func (m *transitionMockChannel) FloodCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.floodAlerts)
}

func (m *transitionMockChannel) CorridorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.corridorAlerts)
}

type staticVerdictProvider struct {
	mu       sync.RWMutex
	verdicts map[string]hotset.Verdict
}

func (p *staticVerdictProvider) GetLatest() []hotset.Verdict {
	p.mu.RLock()
	defer p.mu.RUnlock()
	list := make([]hotset.Verdict, 0, len(p.verdicts))
	for _, v := range p.verdicts {
		list = append(list, v)
	}
	return list
}

func (p *staticVerdictProvider) Set(v hotset.Verdict) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verdicts[v.CameraID] = v
}

func TestTransitionAlertIntegration_EndToEnd(t *testing.T) {
	// Setup camera catalog with 1 corridor camera
	camID := "cam-transition-01"
	roadKey := "nguyen_huu_canh"
	cams := []camera.Camera{
		{
			ID:          camID,
			DisplayName: "Nguyen Huu Canh - Cau Thu Thiem",
			RoadKey:     roadKey,
			Zone:        "Binh Thanh",
			PhuongName:  "Phuong 22",
			Latitude:    10.795,
			Longitude:   106.715,
		},
	}
	catalog := camera.NewCatalog(cams)

	chokepoints := map[string]flood.FloodChokepoint{
		camID: {
			CameraID:       camID,
			CameraName:     "Nguyen Huu Canh Underpass",
			TypicalDepthCm: 30,
		},
	}

	mockChan := &transitionMockChannel{}
	notifyMgr := notify.NewManager(nil)
	notifyMgr.Register(mockChan)

	verdictProv := &staticVerdictProvider{
		verdicts: make(map[string]hotset.Verdict),
	}
	corridorSvc := corridor.NewService(catalog, verdictProv, "test", 5*time.Minute)

	dispatcher := makeFloodAlertDispatcher(catalog, chokepoints, notifyMgr, corridorSvc)

	t0 := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

	// Scenario 1: Initial CLEAR verdict -> 0 flood alerts dispatched
	vClear := hotset.Verdict{
		CameraID:      camID,
		Severity:      "CLEAR",
		FloodSeverity: "CLEAR",
		ObservedAt:    t0,
		Timestamp:     t0,
	}
	verdictProv.Set(vClear)
	dispatcher(vClear)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 0 {
		t.Fatalf("Scenario 1 failed: expected 0 flood alerts for CLEAR, got %d", mockChan.FloodCount())
	}
	if notifyMgr.StateTracker().GetCameraState(camID) != "CLEAR" {
		t.Fatalf("Scenario 1 failed: expected camera state CLEAR, got %s", notifyMgr.StateTracker().GetCameraState(camID))
	}

	// Update corridor verdict provider with shallow flood (15cm)
	d15 := 15.0
	vShallow := hotset.Verdict{
		CameraID:      camID,
		Severity:      "MODERATE",
		FloodSeverity: "FLOODED_SHALLOW",
		FloodDepthCm:  &d15,
		Confidence:    0.85,
		ObservedAt:    t0.Add(2 * time.Minute),
		Timestamp:     t0.Add(2 * time.Minute),
	}
	verdictProv.Set(vShallow)

	// Scenario 2: Transition from CLEAR to FLOODED_SHALLOW -> 1 flood alert and 1 corridor alert dispatched
	dispatcher(vShallow)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 1 {
		t.Fatalf("Scenario 2 failed: expected 1 flood alert, got %d", mockChan.FloodCount())
	}
	if mockChan.CorridorCount() != 1 {
		t.Fatalf("Scenario 2 failed: expected 1 corridor alert, got %d", mockChan.CorridorCount())
	}

	// Scenario 3: Repeated FLOODED_SHALLOW at T+5m -> suppressed by debounce
	vShallowRepeat := hotset.Verdict{
		CameraID:      camID,
		Severity:      "MODERATE",
		FloodSeverity: "FLOODED_SHALLOW",
		FloodDepthCm:  &d15,
		Confidence:    0.85,
		ObservedAt:    t0.Add(5 * time.Minute),
		Timestamp:     t0.Add(5 * time.Minute),
	}
	verdictProv.Set(vShallowRepeat)
	dispatcher(vShallowRepeat)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 1 {
		t.Fatalf("Scenario 3 failed: expected flood alerts to remain 1, got %d", mockChan.FloodCount())
	}

	// Update corridor verdict provider with deep flood (40cm)
	d40 := 40.0
	vDeep := hotset.Verdict{
		CameraID:      camID,
		Severity:      "HEAVY",
		FloodSeverity: "FLOODED_DEEP",
		FloodDepthCm:  &d40,
		Confidence:    0.95,
		ObservedAt:    t0.Add(8 * time.Minute),
		Timestamp:     t0.Add(8 * time.Minute),
	}
	verdictProv.Set(vDeep)

	// Scenario 4: Escalation to FLOODED_DEEP at T+8m -> bypasses debounce, dispatches alert #2
	dispatcher(vDeep)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 2 {
		t.Fatalf("Scenario 4 failed: expected deep flood escalation to dispatch alert #2, got %d", mockChan.FloodCount())
	}

	// Scenario 5: Recovery to CLEAR at T+60m, then re-flood at T+90m -> dispatches alert #3
	vRecover := hotset.Verdict{
		CameraID:      camID,
		Severity:      "CLEAR",
		FloodSeverity: "CLEAR",
		ObservedAt:    t0.Add(60 * time.Minute),
		Timestamp:     t0.Add(60 * time.Minute),
	}
	verdictProv.Set(vRecover)
	dispatcher(vRecover)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 2 {
		t.Fatalf("Scenario 5 failed: expected no new dispatch on CLEAR, got %d", mockChan.FloodCount())
	}

	vReflood := hotset.Verdict{
		CameraID:      camID,
		Severity:      "MODERATE",
		FloodSeverity: "FLOODED_SHALLOW",
		FloodDepthCm:  &d15,
		Confidence:    0.85,
		ObservedAt:    t0.Add(90 * time.Minute),
		Timestamp:     t0.Add(90 * time.Minute),
	}
	verdictProv.Set(vReflood)
	dispatcher(vReflood)

	time.Sleep(50 * time.Millisecond)
	if mockChan.FloodCount() != 3 {
		t.Fatalf("Scenario 5 failed: expected re-flood escalation to dispatch alert #3, got %d", mockChan.FloodCount())
	}
}
