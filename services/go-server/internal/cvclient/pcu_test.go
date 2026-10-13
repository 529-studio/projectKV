package cvclient

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

func defaultTestConfig() PCUConfig {
	return PCUConfig{
		Weights: PCUWeights{
			Motorcycle: 0.3,
			Car:        1.0,
			Bus:        2.5,
			Truck:      2.0,
			Container:  2.0,
		},
		Thresholds: PCUBucketThresholds{
			Moderate:  0.30,
			Heavy:     0.60,
			Congested: 0.80,
		},
		CapacityFloor: 15.0,
	}
}

// TestPCUCalculator_ZeroCounts verifies that when no vehicles are detected,
// PCU total is 0.0, Saturation Index is 0.0, and severity is CLEAR.
func TestPCUCalculator_ZeroCounts(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig())
	sev, pcu, si := calc.Calculate("cam-1", VehicleCounts{}, 0)

	if sev != hotset.SeverityClear {
		t.Errorf("expected CLEAR, got %q", sev)
	}
	if pcu != 0.0 {
		t.Errorf("expected PCU=0.0, got %f", pcu)
	}
	if si != 0.0 {
		t.Errorf("expected SI=0.0, got %f", si)
	}
}

// TestPCUCalculator_SingleClassDominance tests weight scaling for each class individually.
func TestPCUCalculator_SingleClassDominance(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig())

	// 20 motorcycles * 0.3 = 6.0 PCU. With floor 15.0, SI = 6/15 = 0.40 -> MODERATE
	sev, pcu, si := calc.Calculate("cam-moto", VehicleCounts{Motorcycle: 20}, 0)
	if math.Abs(pcu-6.0) > 1e-6 {
		t.Errorf("expected PCU=6.0, got %f", pcu)
	}
	if math.Abs(si-0.40) > 1e-6 {
		t.Errorf("expected SI=0.40, got %f", si)
	}
	if sev != hotset.SeverityModerate {
		t.Errorf("expected MODERATE for 20 motorcycles, got %q", sev)
	}

	// 10 cars * 1.0 = 10.0 PCU. With floor 15.0, SI = 10/15 = 0.6667 -> HEAVY
	sev, pcu, si = calc.Calculate("cam-car", VehicleCounts{Car: 10}, 0)
	if math.Abs(pcu-10.0) > 1e-6 {
		t.Errorf("expected PCU=10.0, got %f", pcu)
	}
	if math.Abs(si-(10.0/15.0)) > 1e-6 {
		t.Errorf("expected SI=0.6667, got %f", si)
	}
	if sev != hotset.SeverityHeavy {
		t.Errorf("expected HEAVY for 10 cars, got %q", sev)
	}

	// 4 buses * 2.5 = 10.0 PCU. With floor 15.0, SI = 10/15 = 0.6667 -> HEAVY
	sev, pcu, si = calc.Calculate("cam-bus", VehicleCounts{Bus: 4}, 0)
	if math.Abs(pcu-10.0) > 1e-6 {
		t.Errorf("expected PCU=10.0, got %f", pcu)
	}
	if math.Abs(si-(10.0/15.0)) > 1e-6 {
		t.Errorf("expected SI=0.6667, got %f", si)
	}
	if sev != hotset.SeverityHeavy {
		t.Errorf("expected HEAVY for 4 buses, got %q", sev)
	}

	// 5 containers * 2.0 = 10.0 PCU (folded into heavy vehicle weight). With floor 15.0, SI = 10/15 = 0.6667 -> HEAVY
	sev, pcu, si = calc.Calculate("cam-container", VehicleCounts{Container: 5}, 0)
	if math.Abs(pcu-10.0) > 1e-6 {
		t.Errorf("expected PCU=10.0, got %f", pcu)
	}
	if math.Abs(si-(10.0/15.0)) > 1e-6 {
		t.Errorf("expected SI=0.6667, got %f", si)
	}
	if sev != hotset.SeverityHeavy {
		t.Errorf("expected HEAVY for 5 containers, got %q", sev)
	}
}

// TestPCUCalculator_BucketBoundaries rigorously verifies exact boundary and epsilon cutoffs.
// Capacity floor is fixed to 100.0 for direct SI calculation: SI = PCU / 100.0.
func TestPCUCalculator_BucketBoundaries(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.CapacityFloor = 100.0

	tests := []struct {
		name       string
		counts     VehicleCounts
		expectedSI float64
		expected   string
	}{
		{
			name:       "Just below MODERATE (SI = 0.298)",
			counts:     VehicleCounts{Motorcycle: 6, Car: 28}, // 6*0.3 + 28 = 29.8 (0.298)
			expectedSI: 0.298,
			expected:   hotset.SeverityClear,
		},
		{
			name:       "Exactly at MODERATE threshold (SI = 0.30)",
			counts:     VehicleCounts{Car: 30}, // 30 * 1.0 = 30.0 (0.300)
			expectedSI: 0.300,
			expected:   hotset.SeverityModerate,
		},
		{
			name:       "Just below HEAVY threshold (SI = 0.598)",
			counts:     VehicleCounts{Motorcycle: 6, Car: 58}, // 6*0.3 + 58 = 59.8 (0.598)
			expectedSI: 0.598,
			expected:   hotset.SeverityModerate,
		},
		{
			name:       "Exactly at HEAVY threshold (SI = 0.60)",
			counts:     VehicleCounts{Car: 60}, // 60 * 1.0 = 60.0 (0.600)
			expectedSI: 0.600,
			expected:   hotset.SeverityHeavy,
		},
		{
			name:       "Just below CONGESTED threshold (SI = 0.798)",
			counts:     VehicleCounts{Motorcycle: 6, Car: 78}, // 6*0.3 + 78 = 79.8 (0.798)
			expectedSI: 0.798,
			expected:   hotset.SeverityHeavy,
		},
		{
			name:       "Exactly at CONGESTED threshold (SI = 0.80)",
			counts:     VehicleCounts{Car: 80}, // 80 * 1.0 = 80.0 (0.800)
			expectedSI: 0.800,
			expected:   hotset.SeverityCongested,
		},
		{
			name:       "Exceeding capacity (SI = 1.20)",
			counts:     VehicleCounts{Car: 120}, // 120 * 1.0 = 120.0 (capacity updates to 120, SI = 1.00 >= 0.80)
			expectedSI: 1.000,
			expected:   hotset.SeverityCongested,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calc := NewPCUCalculator(cfg)
			sev, _, si := calc.Calculate("cam-test", tc.counts, 0)
			if math.Abs(si-tc.expectedSI) > 1e-4 {
				t.Errorf("SI mismatch: expected %f, got %f", tc.expectedSI, si)
			}
			if sev != tc.expected {
				t.Errorf("Severity mismatch: expected %q, got %q", tc.expected, sev)
			}
		})
	}
}

// TestPCUCalculator_ColdStartAndFloorGuards verifies that a zero or negative floor
// cannot trigger divide-by-zero, panic, or NaN.
func TestPCUCalculator_ColdStartAndFloorGuards(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.CapacityFloor = 0.0 // Edge case: misconfigured zero floor

	calc := NewPCUCalculator(cfg)
	sev, pcu, si := calc.Calculate("cam-zero-floor", VehicleCounts{Car: 5}, 0)

	if math.IsNaN(si) || math.IsInf(si, 0) {
		t.Fatalf("expected finite SI, got NaN or Inf: %f", si)
	}
	if pcu != 5.0 {
		t.Errorf("expected PCU=5.0, got %f", pcu)
	}
	if sev == "" || sev == hotset.SeverityUnknown {
		t.Errorf("expected valid non-unknown severity, got %q", sev)
	}
}

// TestPCUCalculator_HistoricalCapacityTracking verifies that capacity expands
// when a higher PCU is observed, preventing false saturation spikes on busy wide corridors.
func TestPCUCalculator_HistoricalCapacityTracking(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig()) // floor = 15.0

	// Frame 1: Peak rush hour observed with 50 cars = 50 PCU.
	// Capacity adapts from 15.0 floor -> 50.0.
	_, pcu1, _ := calc.Calculate("cam-corridor-1", VehicleCounts{Car: 50}, 0)
	if pcu1 != 50.0 {
		t.Fatalf("expected PCU=50, got %f", pcu1)
	}
	if cap1 := calc.GetCapacity("cam-corridor-1"); cap1 != 50.0 {
		t.Fatalf("expected capacity to adapt to 50.0, got %f", cap1)
	}

	// Frame 2: Later, 20 cars observed.
	// Capacity remains 50.0 (historical max), so SI = 20 / 50 = 0.40 -> MODERATE.
	sev2, pcu2, si2 := calc.Calculate("cam-corridor-1", VehicleCounts{Car: 20}, 0)
	if pcu2 != 20.0 {
		t.Errorf("expected PCU=20, got %f", pcu2)
	}
	if math.Abs(si2-0.40) > 1e-6 {
		t.Errorf("expected SI=0.40 against learned capacity 50.0, got %f", si2)
	}
	if sev2 != hotset.SeverityModerate {
		t.Errorf("expected MODERATE against learned capacity 50.0, got %q", sev2)
	}
}

// TestPCUCalculator_FallbackWhenPerClassMissing verifies graceful handling
// when an analyzer returns only total vehicles detected without per-class breakdown.
func TestPCUCalculator_FallbackWhenPerClassMissing(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig())

	// Fallback count = 6, empty VehicleCounts -> treats as 6 * weightCar(1.0) = 6.0 PCU.
	// With floor 15.0, SI = 6.0 / 15.0 = 0.40 -> MODERATE.
	sev, pcu, si := calc.Calculate("cam-fallback", VehicleCounts{}, 6)
	if pcu != 6.0 {
		t.Errorf("expected PCU=6.0, got %f", pcu)
	}
	if math.Abs(si-0.40) > 1e-6 {
		t.Errorf("expected SI=0.40, got %f", si)
	}
	if sev != hotset.SeverityModerate {
		t.Errorf("expected MODERATE, got %q", sev)
	}
}

// TestPCUCalculator_ThreadSafety verifies concurrent calculations under the race detector.
func TestPCUCalculator_ThreadSafety(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig())

	const workers = 20
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			camID := "cam-shared"
			for j := 0; j < iterations; j++ {
				calc.Calculate(camID, VehicleCounts{
					Motorcycle: j,
					Car:        workerID,
				}, 0)
				_ = calc.GetCapacity(camID)
			}
		}(i)
	}

	wg.Wait()
}

// TestPCUCalculator_ContainerFoldedIntoTruck verifies the architectural resolution of
// Issue #186 (ADR-005 T6 / decision-container-class-gap.md):
// 1. Containers and Trucks are weighted equally under baseline configuration (weight = 2.0).
// 2. A custom Container weight override in PCUConfig is still respected if explicitly provided.
func TestPCUCalculator_ContainerFoldedIntoTruck(t *testing.T) {
	// Baseline config: both Truck and Container default to 2.0
	calc := NewPCUCalculator(DefaultPCUConfig())

	// 5 trucks + 5 containers = (5*2.0) + (5*2.0) = 20.0 PCU
	_, pcuMixed, _ := calc.Calculate("cam-mixed", VehicleCounts{Truck: 5, Container: 5}, 0)
	// 10 trucks = 10*2.0 = 20.0 PCU
	_, pcuTruckOnly, _ := calc.Calculate("cam-truck", VehicleCounts{Truck: 10}, 0)

	if math.Abs(pcuMixed-pcuTruckOnly) > 1e-6 {
		t.Errorf("expected mixed trucks and containers (%f) to match all-truck count (%f)", pcuMixed, pcuTruckOnly)
	}
	if math.Abs(pcuMixed-20.0) > 1e-6 {
		t.Errorf("expected 20.0 PCU, got %f", pcuMixed)
	}

	// Explicit override: if an operator explicitly overrides Container weight to 3.5, it is respected
	customCfg := DefaultPCUConfig()
	customCfg.Weights.Container = 3.5
	customCalc := NewPCUCalculator(customCfg)

	_, pcuCustom, _ := customCalc.Calculate("cam-custom", VehicleCounts{Container: 10}, 0)
	if math.Abs(pcuCustom-35.0) > 1e-6 {
		t.Errorf("expected custom container weight 3.5 to yield 35.0 PCU, got %f", pcuCustom)
	}
}

// TestPCUCalculator_CorridorBaseCapacity verifies that cameras on wide multi-lane
// corridors (e.g. Võ Văn Kiệt - Ký Con, 10-12 lanes) receive calibrated base capacities
// and correctly resolve moderate vehicle volumes as CLEAR instead of false-positive HEAVY.
func TestPCUCalculator_CorridorBaseCapacity(t *testing.T) {
	calc := NewPCUCalculator(defaultTestConfig()) // default floor = 15.0

	const voVanKietID = "56de42f611f398ec0c481287"
	const narrowStreetID = "cam-narrow-lane"

	// 1. Verify default fallback capacity for unconfigured cameras is 15.0
	if base := calc.GetBaseCapacity(narrowStreetID); base != 15.0 {
		t.Fatalf("expected narrow street base capacity 15.0, got %f", base)
	}

	// 2. Register corridor-specific base capacity for Võ Văn Kiệt (45.0 PCU)
	calc.SetBaseCapacity(voVanKietID, 45.0)
	if base := calc.GetBaseCapacity(voVanKietID); base != 45.0 {
		t.Fatalf("expected Vo Van Kiet base capacity 45.0, got %f", base)
	}

	// 3. Operational simulation (Bug Report from Staging / Issue #222):
	// Võ Văn Kiệt - Ký Con frame contains: 6 cars, 14 motorcycles (~10.2 PCU).
	// On a 10-12 lane expressway, 10.2 PCU is free-flowing traffic.
	counts := VehicleCounts{Car: 6, Motorcycle: 14} // 6*1.0 + 14*0.3 = 10.2 PCU

	// Calculate on Võ Văn Kiệt:
	// SI = 10.2 / 45.0 = 0.2267 < 0.30 -> CLEAR
	sevVVK, pcuVVK, siVVK := calc.Calculate(voVanKietID, counts, 0)
	if math.Abs(pcuVVK-10.2) > 1e-4 {
		t.Errorf("expected PCU=10.2, got %f", pcuVVK)
	}
	if math.Abs(siVVK-(10.2/45.0)) > 1e-4 {
		t.Errorf("expected SI=%.4f, got %.4f", 10.2/45.0, siVVK)
	}
	if sevVVK != hotset.SeverityClear {
		t.Errorf("expected CLEAR for Vo Van Kiet on 10.2 PCU, got %q (false positive regression!)", sevVVK)
	}

	// Calculate same counts on Narrow Street (15.0 floor):
	// SI = 10.2 / 15.0 = 0.6800 >= 0.60 -> HEAVY
	sevNarrow, pcuNarrow, siNarrow := calc.Calculate(narrowStreetID, counts, 0)
	if math.Abs(pcuNarrow-10.2) > 1e-4 {
		t.Errorf("expected PCU=10.2, got %f", pcuNarrow)
	}
	if math.Abs(siNarrow-(10.2/15.0)) > 1e-4 {
		t.Errorf("expected SI=%.4f, got %.4f", 10.2/15.0, siNarrow)
	}
	if sevNarrow != hotset.SeverityHeavy {
		t.Errorf("expected HEAVY for narrow street on 10.2 PCU, got %q", sevNarrow)
	}
}

type mockCapacityStore struct {
	mu   sync.Mutex
	data map[string]float64
}

func newMockCapacityStore() *mockCapacityStore {
	return &mockCapacityStore{data: make(map[string]float64)}
}

func (m *mockCapacityStore) Save(ctx context.Context, cameraID string, learnedMax float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[cameraID] = learnedMax
	return nil
}

func (m *mockCapacityStore) GetAll(ctx context.Context) (map[string]float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make(map[string]float64, len(m.data))
	for k, v := range m.data {
		copied[k] = v
	}
	return copied, nil
}

// TestPCUCalculator_CapacityStorePersistence verifies that peak traffic observations
// are saved to the persistent CapacityStore and successfully loaded on restart.
func TestPCUCalculator_CapacityStorePersistence(t *testing.T) {
	store := newMockCapacityStore()
	store.data["cam-preloaded"] = 35.0

	calc := NewPCUCalculator(defaultTestConfig())
	calc.SetCapacityStore(store)

	// Pre-load from store
	if err := calc.LoadLearnedCapacities(context.Background()); err != nil {
		t.Fatalf("failed to load learned capacities: %v", err)
	}
	if cap := calc.GetCapacity("cam-preloaded"); cap != 35.0 {
		t.Fatalf("expected preloaded capacity 35.0, got %f", cap)
	}

	// Observe new peak exceeding capacity floor
	calc.Calculate("cam-new-peak", VehicleCounts{Car: 55}, 0)
	calc.WaitPersist()

	store.mu.Lock()
	savedVal, ok := store.data["cam-new-peak"]
	store.mu.Unlock()

	if !ok || savedVal != 55.0 {
		t.Fatalf("expected store to record peak 55.0 for cam-new-peak, got %f (found=%v)", savedVal, ok)
	}

	// Verify next calculator instance boots with the learned peak
	calc2 := NewPCUCalculator(defaultTestConfig())
	calc2.SetCapacityStore(store)
	if err := calc2.LoadLearnedCapacities(context.Background()); err != nil {
		t.Fatalf("failed to load learned capacities in calc2: %v", err)
	}
	if cap := calc2.GetCapacity("cam-new-peak"); cap != 55.0 {
		t.Fatalf("expected calc2 to retain persistent capacity 55.0 across restart, got %f", cap)
	}
}

// TestPCUCalculator_ConcurrentBaseCapacityAndCalculations tests concurrent registrations
// and calculations under Go race detector.
func TestPCUCalculator_ConcurrentBaseCapacityAndCalculations(t *testing.T) {
	store := newMockCapacityStore()
	calc := NewPCUCalculator(defaultTestConfig())
	calc.SetCapacityStore(store)

	const workers = 15
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			camID := "cam-concurrent"
			for j := 0; j < iterations; j++ {
				if j%5 == 0 {
					calc.SetBaseCapacity(camID, 30.0+float64(workerID))
				}
				calc.Calculate(camID, VehicleCounts{
					Motorcycle: j,
					Car:        workerID,
				}, 0)
				_ = calc.GetCapacity(camID)
				_ = calc.GetBaseCapacity(camID)
			}
		}(i)
	}

	wg.Wait()
	calc.WaitPersist()
}

// TestPCUCalculator_HysteresisDeadband verifies that the Schmitt trigger prevents
// rapid chattering/jittering when vehicle count fluctuates right at threshold boundaries.
func TestPCUCalculator_HysteresisDeadband(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.CapacityFloor = 100.0 // 1 PCU = 0.01 SI for clean arithmetic
	cfg.HysteresisDelta = 0.05
	calc := NewPCUCalculator(cfg)
	camID := "cam-conghoa"

	// 1. Initial baseline: 20 cars = 20 PCU -> SI = 0.20 < 0.30 -> CLEAR
	sev1, _, _ := calc.Calculate(camID, VehicleCounts{Car: 20}, 0)
	if sev1 != hotset.SeverityClear {
		t.Fatalf("expected CLEAR at SI=0.20, got %q", sev1)
	}

	// 2. Rising transition: 30 cars = 30 PCU -> SI = 0.30 >= Moderate (0.30) -> MODERATE
	sev2, _, _ := calc.Calculate(camID, VehicleCounts{Car: 30}, 0)
	if sev2 != hotset.SeverityModerate {
		t.Fatalf("expected MODERATE at SI=0.30, got %q", sev2)
	}

	// 3. Fluctuation within deadband: 28 cars = 28 PCU -> SI = 0.28.
	// Without hysteresis, this would jitter down to CLEAR.
	// With hysteresis (deadband 0.05, threshold 0.30 - 0.05 = 0.25):
	// SI 0.28 >= 0.25 -> MUST RETAIN MODERATE!
	sev3, _, _ := calc.Calculate(camID, VehicleCounts{Car: 28}, 0)
	if sev3 != hotset.SeverityModerate {
		t.Fatalf("expected MODERATE retained within deadband (SI=0.28 >= 0.25), got %q", sev3)
	}

	// 4. Drop below deadband: 24 cars = 24 PCU -> SI = 0.24 < 0.25 -> TRANSITIONS TO CLEAR
	sev4, _, _ := calc.Calculate(camID, VehicleCounts{Car: 24}, 0)
	if sev4 != hotset.SeverityClear {
		t.Fatalf("expected CLEAR below deadband (SI=0.24 < 0.25), got %q", sev4)
	}

	// 5. Rising to HEAVY: 60 cars = 60 PCU -> SI = 0.60 >= Heavy (0.60) -> HEAVY
	sev5, _, _ := calc.Calculate(camID, VehicleCounts{Car: 60}, 0)
	if sev5 != hotset.SeverityHeavy {
		t.Fatalf("expected HEAVY at SI=0.60, got %q", sev5)
	}

	// 6. Fluctuation within HEAVY deadband: 58 cars -> SI = 0.58 >= (0.60 - 0.05 = 0.55) -> RETAINS HEAVY
	sev6, _, _ := calc.Calculate(camID, VehicleCounts{Car: 58}, 0)
	if sev6 != hotset.SeverityHeavy {
		t.Fatalf("expected HEAVY retained within deadband (SI=0.58 >= 0.55), got %q", sev6)
	}

	// 7. Drop below HEAVY deadband: 54 cars -> SI = 0.54 < 0.55 -> TRANSITIONS TO MODERATE
	sev7, _, _ := calc.Calculate(camID, VehicleCounts{Car: 54}, 0)
	if sev7 != hotset.SeverityModerate {
		t.Fatalf("expected MODERATE below HEAVY deadband (SI=0.54 < 0.55), got %q", sev7)
	}
}
