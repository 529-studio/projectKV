package cvclient

import (
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
