package hotset_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

func TestStore_SetAndGet(t *testing.T) {
	st := hotset.NewStore(60 * time.Second)

	now := time.Now()
	v := hotset.Verdict{
		CameraID:        "cam-01",
		Severity:        hotset.SeverityModerate,
		Confidence:      0.85,
		Source:          "onnx",
		ObservedAt:      now,
		Timestamp:       now,
		SaturationIndex: 0.45,
		PCUTotal:        28.5,
	}

	st.Set(v)

	got, ok := st.Get("cam-01")
	if !ok {
		t.Fatalf("expected cam-01 in store, got false")
	}
	if got.CameraID != "cam-01" || got.Severity != hotset.SeverityModerate {
		t.Errorf("unexpected verdict: %+v", got)
	}
	if got.SaturationIndex != 0.45 || got.PCUTotal != 28.5 {
		t.Errorf("expected SI=0.45, PCU=28.5, got SI=%f, PCU=%f", got.SaturationIndex, got.PCUTotal)
	}
	if got.Stale {
		t.Errorf("expected fresh verdict (stale=false), got true")
	}
	if st.Size() != 1 {
		t.Errorf("expected size=1, got %d", st.Size())
	}
}

func TestStore_Staleness(t *testing.T) {
	st := hotset.NewStore(50 * time.Millisecond)

	oldTime := time.Now().Add(-100 * time.Millisecond)
	v := hotset.Verdict{
		CameraID:        "cam-old",
		Severity:        hotset.SeverityClear,
		ObservedAt:      oldTime,
		Timestamp:       oldTime,
		SaturationIndex: 0.15,
		PCUTotal:        10.0,
	}

	st.Set(v)

	got, ok := st.Get("cam-old")
	if !ok {
		t.Fatalf("expected cam-old in store")
	}
	if !got.Stale {
		t.Errorf("expected verdict to be marked stale after 50ms TTL")
	}

	all := st.GetLatest()
	if len(all) != 1 || !all[0].Stale {
		t.Errorf("expected GetLatest to return stale verdict, got %+v", all)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	st := hotset.NewStore(10 * time.Second)
	numGoroutines := 100
	var wg sync.WaitGroup

	// Concurrently write verdicts
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		camID := fmt.Sprintf("cam-%03d", i%20)
		go func(id string, idx int) {
			defer wg.Done()
			now := time.Now()
			st.Set(hotset.Verdict{
				CameraID:        id,
				Severity:        hotset.SeverityHeavy,
				Confidence:      0.9,
				Source:          "onnx",
				ObservedAt:      now,
				Timestamp:       now,
				SaturationIndex: float64(idx) / 100.0,
				PCUTotal:        float64(idx * 2),
			})
		}(camID, i)
	}

	// Concurrently read verdicts and call GetLatest
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		camID := fmt.Sprintf("cam-%03d", i%20)
		go func(id string) {
			defer wg.Done()
			_, _ = st.Get(id)
			_ = st.GetLatest()
			_ = st.Size()
		}(camID)
	}

	wg.Wait()

	if st.Size() == 0 {
		t.Errorf("expected non-zero store size after concurrent writes")
	}
}

type mockAnalyzerWithSI struct{}

func (m *mockAnalyzerWithSI) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	now := time.Now()
	return hotset.Verdict{
		CameraID:        cam.ID,
		Severity:        hotset.SeverityClear,
		Confidence:      0.95,
		Source:          "onnx",
		ObservedAt:      now,
		Timestamp:       now,
		SaturationIndex: 0.25,
		PCUTotal:        18.0,
	}, nil
}

func TestScheduler_WithStore(t *testing.T) {
	cam := hotset.Camera{ID: "cam-store-test", Name: "Test Cam"}
	reg := hotset.NewRegistry([]hotset.Camera{cam}, 60*time.Second)
	st := hotset.NewStore(60 * time.Second)

	sched := hotset.NewScheduler(reg, &mockAnalyzerWithSI{}, 50*time.Millisecond, 2, nil, nil)
	sched.SetStore(st)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Run(ctx)

	// Wait for immediate first run
	time.Sleep(20 * time.Millisecond)

	v, ok := st.Get("cam-store-test")
	if !ok {
		t.Fatalf("expected camera verdict to be recorded in store")
	}
	if v.SaturationIndex != 0.25 || v.PCUTotal != 18.0 {
		t.Errorf("expected SI=0.25, PCU=18.0, got SI=%f, PCU=%f", v.SaturationIndex, v.PCUTotal)
	}
}
