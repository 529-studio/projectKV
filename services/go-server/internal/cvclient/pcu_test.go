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
			Motorcycle: 0.2,
			Car:        1.0,
			Bus:        2.0,
			Truck:      2.0,
			Container:  3.0,
		},
		Thresholds: PCUBucketThresholds{
			Moderate:  0.35,
			Heavy:     0.70,
			Congested: 0.90,
		},
		CapacityFloor: 25.0,
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

	// 50 motorcycles * 0.2 = 10.0 PCU. With floor 25.0, SI = 10/25 = 0.40 -> MODERATE
	sev, pcu, si := calc.Calculate("cam-moto", VehicleCounts{Motorcycle: 50}, 0)
	if math.Abs(pcu-10.0) > 1e-6 {
		t.Errorf("expected PCU=10.0, got %f", pcu)
	}
	if math.Abs(si-0.40) > 1e-6 {
		t.Errorf("expected SI=0.40, got %f", si)
	}
	if sev != hotset.SeverityModerate {
		t.Errorf("expected MODERATE for 50 motorcycles, got %q", sev)
	}

	// 20 cars * 1.0 = 20.0 PCU. With floor 25.0, SI = 20/25 = 0.80 -> HEAVY
	sev, pcu, si = calc.Calculate("cam-car", VehicleCounts{Car: 20}, 0)
	if math.Abs(pcu-20.0) > 1e-6 {
		t.Errorf("expected PCU=20.0, got %f", pcu)
	}
	if math.Abs(si-0.80) > 1e-6 {
		t.Errorf("expected SI=0.80, got %f", si)
	}
	if sev != hotset.SeverityHeavy {
		t.Errorf("expected HEAVY for 20 cars, got %q", sev)
	}

	// 10 containers * 3.0 = 30.0 PCU. On cold start, max updates to 30.0 -> capacity = 30.0, SI = 30/30 = 1.00 -> CONGESTED
	sev, pcu, si = calc.Calculate("cam-container", VehicleCounts{Container: 10}, 0)
	if math.Abs(pcu-30.0) > 1e-6 {
		t.Errorf("expected PCU=30.0, got %f", pcu)
	}
	if math.Abs(si-1.00) > 1e-6 {
		t.Errorf("expected SI=1.00, got %f", si)
	}
	if sev != hotset.SeverityCongested {
		t.Errorf("expected CONGESTED for 10 containers, got %q", sev)
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
			name:       "Just below MODERATE (SI = 0.349)",
			counts:     VehicleCounts{Motorcycle: 1, Car: 34}, // 1*0.2 + 34 = 34.2 (0.342)
			expectedSI: 0.342,
			expected:   hotset.SeverityClear,
		},
		{
			name:       "Exactly at MODERATE threshold (SI = 0.35)",
			counts:     VehicleCounts{Car: 35}, // 35 * 1.0 = 35.0 (0.350)
			expectedSI: 0.350,
			expected:   hotset.SeverityModerate,
		},
		{
			name:       "Just below HEAVY threshold (SI = 0.698)",
			counts:     VehicleCounts{Motorcycle: 4, Car: 69}, // 4*0.2 + 69 = 69.8 (0.698)
			expectedSI: 0.698,
			expected:   hotset.SeverityModerate,
		},
		{
			name:       "Exactly at HEAVY threshold (SI = 0.70)",
			counts:     VehicleCounts{Car: 70}, // 70 * 1.0 = 70.0 (0.700)
			expectedSI: 0.700,
			expected:   hotset.SeverityHeavy,
		},
		{
			name:       "Just below CONGESTED threshold (SI = 0.898)",
			counts:     VehicleCounts{Motorcycle: 4, Car: 89}, // 4*0.2 + 89 = 89.8 (0.898)
			expectedSI: 0.898,
			expected:   hotset.SeverityHeavy,
		},
		{
			name:       "Exactly at CONGESTED threshold (SI = 0.90)",
			counts:     VehicleCounts{Car: 90}, // 90 * 1.0 = 90.0 (0.900)
			expectedSI: 0.900,
			expected:   hotset.SeverityCongested,
		},
		{
			name:       "Exceeding capacity (SI = 1.20)",
			counts:     VehicleCounts{Car: 120}, // 120 * 1.0 = 120.0 (capacity updates to 120, SI = 1.00 >= 0.90)
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
	calc := NewPCUCalculator(defaultTestConfig()) // floor = 25.0

	// Frame 1: Peak rush hour observed with 50 cars = 50 PCU.
	// Capacity adapts from 25.0 floor -> 50.0.
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

	// Fallback count = 10, empty VehicleCounts -> treats as 10 * weightCar(1.0) = 10.0 PCU.
	// With floor 25.0, SI = 10 / 25 = 0.40 -> MODERATE.
	sev, pcu, si := calc.Calculate("cam-fallback", VehicleCounts{}, 10)
	if pcu != 10.0 {
		t.Errorf("expected PCU=10.0, got %f", pcu)
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
