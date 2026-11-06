package forecast

import (
	"math"
	"time"
)

const (
	ModelRidgeDelta       = "ridge_delta"
	ModelPersistence      = "persistence"
	ModelHistoricalMedian = "historical_median"

	SeverityClear     = "CLEAR"
	SeverityModerate  = "MODERATE"
	SeverityHeavy     = "HEAVY"
	SeverityCongested = "CONGESTED"

	ThresholdClear    = 0.30
	ThresholdModerate = 0.60
	ThresholdHeavy    = 0.80

	MinSpeedKmh = 3.0
	MaxSpeedKmh = 45.0
)

// SupportedHorizons lists the validated forecast horizons in minutes.
var SupportedHorizons = []int{10, 20, 30}

// IsSupportedHorizon returns true if horizon is 10, 20, or 30.
func IsSupportedHorizon(h int) bool {
	return h == 10 || h == 20 || h == 30
}

// ModelParams holds calibrated ridge regression parameters for a single horizon.
type ModelParams struct {
	HorizonMinutes int
	Intercept      float64
	Means          [6]float64
	Stds           [6]float64
	Coeffs         [6]float64 // standardized coefficients for [lag_5m, lag_10m, lag_15m, rolling_mean_15m, hour_of_day, day_of_week]
	MAE            float64
	RMSE           float64
	GlobalMedian   float64
}

// CalibratedRidgeParams maps horizon minutes (10, 20, 30) to empirical parameters
// calibrated in docs/reports/forecast_prototype_v1.json.
var CalibratedRidgeParams = map[int]ModelParams{
	10: {
		HorizonMinutes: 10,
		Intercept:      0.00107284,
		Means:          [6]float64{0.40375368, 0.40319124, 0.40262416, 0.40318968, 11.45477387, 2.49497487},
		Stds:           [6]float64{0.15215443, 0.15210521, 0.15206814, 0.15204149, 6.05392143, 0.49997475},
		Coeffs:         [6]float64{0.09378699, -0.00372577, -0.09066105, -0.00018257, -0.00130246, 0.00012606},
		MAE:            0.00440486,
		RMSE:           0.00508631,
		GlobalMedian:   0.439646,
	},
	20: {
		HorizonMinutes: 20,
		Intercept:      0.00205409,
		Means:          [6]float64{0.40313019, 0.40258085, 0.40202802, 0.40257967, 11.43181818, 2.49242424},
		Stds:           [6]float64{0.15224289, 0.15220401, 0.15217802, 0.15214032, 6.06054451, 0.49994260},
		Coeffs:         [6]float64{0.17912274, -0.00956642, -0.17129539, -0.00055492, -0.00239800, 0.00045848},
		MAE:            0.01016163,
		RMSE:           0.01172547,
		GlobalMedian:   0.4405125,
	},
	30: {
		HorizonMinutes: 30,
		Intercept:      0.00292070,
		Means:          [6]float64{0.40253359, 0.40199928, 0.40146153, 0.40199812, 11.40862944, 2.48984772},
		Stds:           [6]float64{0.15235608, 0.15232854, 0.15231368, 0.15226458, 6.06713930, 0.49989692},
		Coeffs:         [6]float64{0.25109996, -0.01712766, -0.23729677, -0.00108567, -0.00349264, 0.00096634},
		MAE:            0.01724040,
		RMSE:           0.01989593,
		GlobalMedian:   0.4415535,
	},
}

// Features contains the input feature vector for the Ridge regression delta model.
type Features struct {
	Lag5m          float64
	Lag10m         float64
	Lag15m         float64
	RollingMean15m float64
	HourOfDay      float64
	DayOfWeek      float64
}

// AsSlice returns features in the standardized order:
// [lag_5m, lag_10m, lag_15m, rolling_mean_15m, hour_of_day, day_of_week].
func (f Features) AsSlice() [6]float64 {
	return [6]float64{
		f.Lag5m,
		f.Lag10m,
		f.Lag15m,
		f.RollingMean15m,
		f.HourOfDay,
		f.DayOfWeek,
	}
}

// PredictRidgeDelta calculates predicted saturation index using the calibrated ridge model.
// Non-negative saturation index is enforced by flooring at 0.0.
func PredictRidgeDelta(currentSI float64, f Features, params ModelParams) float64 {
	if math.IsNaN(currentSI) || math.IsInf(currentSI, 0) {
		currentSI = 0.0
	}
	x := f.AsSlice()
	delta := params.Intercept
	for i := 0; i < 6; i++ {
		std := params.Stds[i]
		if std <= 0 || math.IsNaN(std) || math.IsInf(std, 0) {
			std = 1.0
		}
		val := x[i]
		if math.IsNaN(val) || math.IsInf(val, 0) {
			val = params.Means[i]
		}
		z := (val - params.Means[i]) / std
		delta += z * params.Coeffs[i]
	}
	pred := currentSI + delta
	if pred < 0.0 || math.IsNaN(pred) || math.IsInf(pred, 0) {
		return 0.0
	}
	return pred
}

// EstimateSpeed maps Saturation Index to vehicle speed in km/h using calibrated arterial curves.
// Based on Greenshields piecewise model calibrated for urban arterial corridors in HCMC
// (v_free = 45-50 km/h, v_jam = 3-5 km/h).
func EstimateSpeed(si float64) float64 {
	if si < 0.0 || math.IsNaN(si) || math.IsInf(si, 0) {
		si = 0.0
	}
	switch {
	case si < ThresholdClear:
		return 45.0 - 7.0*(si/ThresholdClear)
	case si < ThresholdModerate:
		return 38.0 - 18.0*((si-ThresholdClear)/(ThresholdModerate-ThresholdClear))
	case si < ThresholdHeavy:
		return 20.0 - 10.0*((si-ThresholdModerate)/(ThresholdHeavy-ThresholdModerate))
	default:
		speed := 10.0 - 7.0*((si-ThresholdHeavy)/0.20)
		if speed < MinSpeedKmh {
			speed = MinSpeedKmh
		}
		return speed
	}
}

// SaturationToSeverity maps Saturation Index to level of service category.
func SaturationToSeverity(si float64) string {
	if math.IsNaN(si) || math.IsInf(si, 0) || si < 0.0 {
		return SeverityClear
	}
	switch {
	case si < ThresholdClear:
		return SeverityClear
	case si < ThresholdModerate:
		return SeverityModerate
	case si < ThresholdHeavy:
		return SeverityHeavy
	default:
		return SeverityCongested
	}
}

// ComputeConfidenceIntervals calculates 95% confidence intervals for saturation index and speed.
// Since speed decreases monotonically with increasing saturation index, the upper SI bound
// corresponds to the lower speed bound.
func ComputeConfidenceIntervals(predictedSI float64, rmse float64) (siLower, siUpper, speedLower, speedUpper float64) {
	if math.IsNaN(predictedSI) || math.IsInf(predictedSI, 0) || predictedSI < 0.0 {
		predictedSI = 0.0
	}
	if math.IsNaN(rmse) || math.IsInf(rmse, 0) || rmse < 0.0 {
		rmse = 0.0
	}
	margin := 1.96 * rmse
	siLower = predictedSI - margin
	if siLower < 0.0 || math.IsNaN(siLower) || math.IsInf(siLower, 0) {
		siLower = 0.0
	}
	siUpper = predictedSI + margin
	if math.IsNaN(siUpper) || math.IsInf(siUpper, 0) {
		siUpper = 0.0
	}
	if siLower > siUpper {
		siLower, siUpper = siUpper, siLower
	}

	speedLower = EstimateSpeed(siUpper)
	speedUpper = EstimateSpeed(siLower)

	if speedLower > speedUpper {
		speedLower, speedUpper = speedUpper, speedLower
	}
	if speedLower < MinSpeedKmh {
		speedLower = MinSpeedKmh
	}
	if speedUpper > MaxSpeedKmh {
		speedUpper = MaxSpeedKmh
	}
	return
}

// BuildFeatures extracts regression features from a list of recent observations
// relative to baseTime (UTC). If fewer than 3 observations exist or lag values are
// missing, reasonable historical fallback interpolation is performed.
func BuildFeatures(observations []Observation, baseTime time.Time) (Features, float64, bool) {
	if len(observations) == 0 {
		return Features{}, 0.0, false
	}

	// Asia/Ho_Chi_Minh timezone (UTC+7)
	loc := time.FixedZone("ICT", 7*3600)
	localTime := baseTime.In(loc)

	hourOfDay := float64(localTime.Hour())
	// Convert Go weekday (Sunday=0, Monday=1, ..., Saturday=6) to Python weekday (Monday=0, ..., Sunday=6)
	dayOfWeek := float64((int(localTime.Weekday()) + 6) % 7)

	// Latest observation is currentSI
	currentSI := observations[0].SaturationIndex
	if math.IsNaN(currentSI) || math.IsInf(currentSI, 0) || currentSI < 0.0 {
		currentSI = 0.0
	}

	// Target lag times: 5m, 10m, 15m ago (strictly as-of: <= target, within 5m tolerance)
	target5m := baseTime.Add(-5 * time.Minute)
	target10m := baseTime.Add(-10 * time.Minute)
	target15m := baseTime.Add(-15 * time.Minute)

	lag5m := findAsOfSI(observations, target5m, 5*time.Minute, currentSI)
	lag10m := findAsOfSI(observations, target10m, 5*time.Minute, lag5m)
	lag15m := findAsOfSI(observations, target15m, 5*time.Minute, lag10m)

	// Rolling mean over half-open 15m window [baseTime-15m, baseTime), excluding baseTime sample
	cutoff15m := baseTime.Add(-15 * time.Minute)
	var sumSI float64
	var count int
	for _, obs := range observations {
		if obs.ObservedAt.After(cutoff15m) && obs.ObservedAt.Before(baseTime) {
			val := obs.SaturationIndex
			if !math.IsNaN(val) && !math.IsInf(val, 0) && val >= 0.0 {
				sumSI += val
				count++
			}
		}
	}
	var rollingMean float64
	if count > 0 {
		rollingMean = sumSI / float64(count)
	} else {
		rollingMean = (currentSI + lag5m + lag10m + lag15m) / 4.0
	}
	if math.IsNaN(rollingMean) || math.IsInf(rollingMean, 0) || rollingMean < 0.0 {
		rollingMean = currentSI
	}

	return Features{
		Lag5m:          lag5m,
		Lag10m:         lag10m,
		Lag15m:         lag15m,
		RollingMean15m: rollingMean,
		HourOfDay:      hourOfDay,
		DayOfWeek:      dayOfWeek,
	}, currentSI, true
}

func findAsOfSI(observations []Observation, target time.Time, maxAge time.Duration, fallback float64) float64 {
	if math.IsNaN(fallback) || math.IsInf(fallback, 0) || fallback < 0.0 {
		fallback = 0.0
	}
	var bestTime time.Time
	bestVal := fallback
	found := false
	for _, obs := range observations {
		// Strictly as-of: sample cannot be after target
		if obs.ObservedAt.After(target) {
			continue
		}
		age := target.Sub(obs.ObservedAt)
		if age > maxAge {
			continue
		}
		if !found || obs.ObservedAt.After(bestTime) {
			val := obs.SaturationIndex
			if !math.IsNaN(val) && !math.IsInf(val, 0) && val >= 0.0 {
				bestTime = obs.ObservedAt
				bestVal = val
				found = true
			}
		}
	}
	if !found {
		return fallback
	}
	return bestVal
}
