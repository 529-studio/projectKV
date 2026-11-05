package forecast_test

import (
	"math"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
)

func TestIsSupportedHorizon(t *testing.T) {
	for _, h := range []int{10, 20, 30} {
		if !forecast.IsSupportedHorizon(h) {
			t.Errorf("expected horizon %d to be supported", h)
		}
	}
	for _, h := range []int{0, 5, 15, 25, 45, -10} {
		if forecast.IsSupportedHorizon(h) {
			t.Errorf("expected horizon %d to be unsupported", h)
		}
	}
}

func TestEstimateSpeed(t *testing.T) {
	cases := []struct {
		name      string
		si        float64
		wantSpeed float64
	}{
		{
			name:      "zero SI returns free flow speed",
			si:        0.0,
			wantSpeed: 45.0,
		},
		{
			name:      "clear midpoint 0.15",
			si:        0.15,
			wantSpeed: 41.5, // 45.0 - 7.0*(0.15/0.30) = 41.5
		},
		{
			name:      "clear boundary 0.30 returns 38 km/h",
			si:        0.30,
			wantSpeed: 38.0, // at 0.30 boundary: 45.0 - 7.0*(0.30/0.30) = 38.0
		},
		{
			name:      "moderate midpoint 0.45",
			si:        0.45,
			wantSpeed: 29.0, // 38.0 - 18.0*(0.15/0.30) = 29.0
		},
		{
			name:      "heavy midpoint 0.70",
			si:        0.70,
			wantSpeed: 15.0, // 20.0 - 10.0*(0.10/0.20) = 15.0
		},
		{
			name:      "congested at 0.80 boundary",
			si:        0.80,
			wantSpeed: 10.0, // 10.0 - 7.0*(0.0/0.20) = 10.0
		},
		{
			name:      "congested severe at 1.0",
			si:        1.00,
			wantSpeed: 3.0, // 10.0 - 7.0*(0.20/0.20) = 3.0
		},
		{
			name:      "extreme congestion > 1.0 clamped at 3.0 km/h",
			si:        1.50,
			wantSpeed: 3.0,
		},
		{
			name:      "negative SI clamped to zero",
			si:        -0.5,
			wantSpeed: 45.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := forecast.EstimateSpeed(tc.si)
			if math.Abs(got-tc.wantSpeed) > 1e-4 {
				t.Errorf("EstimateSpeed(%v) = %v, want %v", tc.si, got, tc.wantSpeed)
			}
		})
	}
}

func TestSaturationToSeverity(t *testing.T) {
	cases := []struct {
		si   float64
		want string
	}{
		{0.0, forecast.SeverityClear},
		{0.29, forecast.SeverityClear},
		{0.30, forecast.SeverityModerate},
		{0.59, forecast.SeverityModerate},
		{0.60, forecast.SeverityHeavy},
		{0.79, forecast.SeverityHeavy},
		{0.80, forecast.SeverityCongested},
		{1.20, forecast.SeverityCongested},
	}
	for _, tc := range cases {
		got := forecast.SaturationToSeverity(tc.si)
		if got != tc.want {
			t.Errorf("SaturationToSeverity(%v) = %v, want %v", tc.si, got, tc.want)
		}
	}
}

func TestPredictRidgeDelta(t *testing.T) {
	params := forecast.CalibratedRidgeParams[10]
	f := forecast.Features{
		Lag5m:          0.40,
		Lag10m:         0.40,
		Lag15m:         0.40,
		RollingMean15m: 0.40,
		HourOfDay:      12.0,
		DayOfWeek:      2.0,
	}

	pred := forecast.PredictRidgeDelta(0.40, f, params)
	if pred < 0.0 {
		t.Errorf("expected non-negative prediction, got %v", pred)
	}
	// With features close to training mean, prediction should stay close to current
	if math.Abs(pred-0.40) > 0.05 {
		t.Errorf("expected prediction near 0.40, got %v", pred)
	}

	// Floor check with large negative current SI
	predFloored := forecast.PredictRidgeDelta(-1.0, f, params)
	if predFloored != 0.0 {
		t.Errorf("expected 0.0 floor for negative delta, got %v", predFloored)
	}
}

func TestComputeConfidenceIntervals(t *testing.T) {
	testSIs := []float64{0.0, 0.10, 0.299, 0.30, 0.301, 0.45, 0.599, 0.60, 0.799, 0.80, 0.90, 1.0, 1.5}
	for _, si := range testSIs {
		siLower, siUpper, speedLower, speedUpper := forecast.ComputeConfidenceIntervals(si, 0.02)
		if siLower > siUpper {
			t.Errorf("expected siLower <= siUpper for SI=%v, got [%v, %v]", si, siLower, siUpper)
		}
		if speedLower > speedUpper {
			t.Errorf("expected speedLower <= speedUpper for SI=%v, got [%v, %v]", si, speedLower, speedUpper)
		}
		if speedLower < forecast.MinSpeedKmh || speedUpper > forecast.MaxSpeedKmh {
			t.Errorf("speed bounds out of range [%v, %v] for SI=%v", speedLower, speedUpper, si)
		}
	}
}

func TestNonFiniteHandling(t *testing.T) {
	for _, val := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		speed := forecast.EstimateSpeed(val)
		if math.IsNaN(speed) || math.IsInf(speed, 0) || speed != 45.0 {
			t.Errorf("EstimateSpeed(%v) = %v, want 45.0", val, speed)
		}
		sev := forecast.SaturationToSeverity(val)
		if sev != forecast.SeverityClear {
			t.Errorf("SaturationToSeverity(%v) = %v, want CLEAR", val, sev)
		}
		siL, siU, spL, spU := forecast.ComputeConfidenceIntervals(val, 0.01)
		if math.IsNaN(siL) || math.IsInf(siL, 0) || math.IsNaN(siU) || math.IsInf(siU, 0) || math.IsNaN(spL) || math.IsInf(spL, 0) || math.IsNaN(spU) || math.IsInf(spU, 0) {
			t.Errorf("ComputeConfidenceIntervals(%v) produced non-finite values", val)
		}
		if spL > spU {
			t.Errorf("ComputeConfidenceIntervals(%v) produced spL > spU: %v > %v", val, spL, spU)
		}
	}
}

func TestBuildFeatures(t *testing.T) {
	baseTime := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	obs := []forecast.Observation{
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.45,
			ObservedAt:      baseTime.Add(-1 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.40,
			ObservedAt:      baseTime.Add(-5 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.35,
			ObservedAt:      baseTime.Add(-10 * time.Minute),
		},
		{
			CameraID:        "cam-1",
			SaturationIndex: 0.30,
			ObservedAt:      baseTime.Add(-15 * time.Minute),
		},
	}

	features, currentSI, ok := forecast.BuildFeatures(obs, baseTime)
	if !ok {
		t.Fatal("expected BuildFeatures to succeed")
	}
	if currentSI != 0.45 {
		t.Errorf("expected currentSI 0.45, got %v", currentSI)
	}
	if features.Lag5m != 0.40 {
		t.Errorf("expected Lag5m 0.40, got %v", features.Lag5m)
	}
	if features.Lag10m != 0.35 {
		t.Errorf("expected Lag10m 0.35, got %v", features.Lag10m)
	}
	if features.Lag15m != 0.30 {
		t.Errorf("expected Lag15m 0.30, got %v", features.Lag15m)
	}
	if features.RollingMean15m <= 0.0 {
		t.Errorf("expected positive rolling mean, got %v", features.RollingMean15m)
	}
}
