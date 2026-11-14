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
	HorizonMinutes int       `json:"horizon_minutes"`
	Intercept      float64   `json:"intercept"`
	Means          []float64 `json:"means"`
	Stds           []float64 `json:"stds"`
	Coeffs         []float64 `json:"coeffs"`
	MAE            float64   `json:"mae"`
	RMSE           float64   `json:"rmse"`
	GlobalMedian   float64   `json:"global_median"`
}

// CalibratedRidgeParams provides backwards-compatible access to default ridge parameters,
// dynamically populated from the embedded artifact.
var CalibratedRidgeParams = DefaultArtifact().RidgeParams()

// DefaultModelMetadata carries provenance and validation metadata dynamically derived
// from the default artifact.
var DefaultModelMetadata = DefaultArtifact().DefaultMetadata()

// Features contains the input feature vector for the Ridge regression delta model.
// Includes 6 traffic lag/calendar features + 6 weather features (Issue #444 / S14-T4).
type Features struct {
	Lag5m          float64
	Lag10m         float64
	Lag15m         float64
	RollingMean15m float64
	HourOfDay      float64
	DayOfWeek      float64

	// Weather features (ADR-012 D3, Issue #444)
	PrecipMm15m      float64
	PrecipMm60m      float64
	RainFlag         float64
	HoursSinceRain   float64
	WeatherCodeClass float64
	RadarActive      float64
}

// AsSlice returns features in standardized order:
// [lag_5m, lag_10m, lag_15m, rolling_mean_15m, hour_of_day, day_of_week,
//
//	precip_mm_15m, precip_mm_60m, rain_flag, hours_since_rain, weather_code_class, radar_active]
func (f Features) AsSlice() []float64 {
	return []float64{
		f.Lag5m,
		f.Lag10m,
		f.Lag15m,
		f.RollingMean15m,
		f.HourOfDay,
		f.DayOfWeek,
		f.PrecipMm15m,
		f.PrecipMm60m,
		f.RainFlag,
		f.HoursSinceRain,
		f.WeatherCodeClass,
		f.RadarActive,
	}
}

// PredictRidgeDelta calculates predicted saturation index using the calibrated ridge model.
// Non-negative saturation index is enforced by flooring at 0.0.
// Dynamically supports both 6-feature baseline and 12-feature weather models.
func PredictRidgeDelta(currentSI float64, f Features, params ModelParams) float64 {
	if math.IsNaN(currentSI) || math.IsInf(currentSI, 0) {
		currentSI = 0.0
	}
	x := f.AsSlice()
	delta := params.Intercept
	n := len(params.Coeffs)
	if len(params.Means) < n {
		n = len(params.Means)
	}
	if len(params.Stds) < n {
		n = len(params.Stds)
	}
	if len(x) < n {
		n = len(x)
	}
	for i := 0; i < n; i++ {
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

// ClassifyWeatherCode maps WMO weather code (0-99) into discrete forecast classes (0-4).
// Matches tools/forecast/extract_features.py:classify_weather_code.
func ClassifyWeatherCode(code int) float64 {
	switch {
	case code == 0 || code == 1:
		return 0.0
	case code == 2 || code == 3 || code == 45 || code == 48:
		return 1.0
	case code == 51 || code == 53 || code == 55 || code == 56 || code == 57 || code == 61 || code == 80:
		return 2.0
	case code == 63 || code == 65 || code == 66 || code == 67 || code == 81 || code == 82:
		return 3.0
	case code == 95 || code == 96 || code == 99:
		return 4.0
	default:
		return 0.0
	}
}

// ComputeWeatherFeatures derives meteorological features as-of target time (ADR-012 D3, Lesson 67).
// Strict 30-minute cutoff: if no observation is within [target - 30m, target], default values are used.
func ComputeWeatherFeatures(weatherObs []WeatherObservation, target time.Time) (precip15m, precip60m, rainFlag, hoursSinceRain, weatherCodeClass, radarActive float64) {
	hoursSinceRain = 24.0 // Default dry history
	if len(weatherObs) == 0 {
		return
	}

	// Find the most recent observation at or before target time
	var latest *WeatherObservation
	var latestIdx = -1
	for i := len(weatherObs) - 1; i >= 0; i-- {
		obs := &weatherObs[i]
		if !obs.ObservedAt.After(target) {
			latest = obs
			latestIdx = i
			break
		}
	}
	if latest == nil {
		return
	}

	age := target.Sub(latest.ObservedAt)
	if age > 30*time.Minute || age < 0 {
		// Strict age cutoff: outside 30 minutes, do not forward-fill
		return
	}

	// Rolling precipitation over 15m and 60m
	var sum15m, sum60m float64
	var count15m int
	for i := latestIdx; i >= 0; i-- {
		obs := &weatherObs[i]
		if obs.ObservedAt.After(target) {
			continue
		}
		dt := target.Sub(obs.ObservedAt)
		if dt > 60*time.Minute {
			break
		}
		p := obs.PrecipitationMm
		if math.IsNaN(p) || p < 0 {
			p = 0
		}
		sum60m += p
		if dt <= 15*time.Minute {
			sum15m += p
			count15m++
		}
	}

	if count15m == 0 {
		p := latest.PrecipitationMm
		if math.IsNaN(p) || p < 0 {
			p = 0
		}
		sum15m = p
	}
	precip15m = sum15m
	precip60m = sum60m

	weatherCodeClass = ClassifyWeatherCode(latest.WeatherCode)
	if precip15m > 0 || latest.PrecipitationMm > 0 || weatherCodeClass >= 2 {
		rainFlag = 1.0
	}

	// Hours since last rain: search backwards
	for i := latestIdx; i >= 0; i-- {
		obs := &weatherObs[i]
		if obs.ObservedAt.After(target) {
			continue
		}
		p := obs.PrecipitationMm
		if math.IsNaN(p) || p < 0 {
			p = 0
		}
		pClass := ClassifyWeatherCode(obs.WeatherCode)
		if p > 0 || pClass >= 2 {
			hoursSinceRain = math.Max(0.0, target.Sub(obs.ObservedAt).Hours())
			break
		}
	}

	radarActive = float64(latest.RadarActive)
	return
}

// BuildFeatures extracts regression features from a list of recent observations
// relative to baseTime (UTC). Preserves backward compatibility.
func BuildFeatures(observations []Observation, baseTime time.Time) (Features, float64, bool) {
	return BuildFeaturesWithWeather(observations, baseTime, nil)
}

// BuildFeaturesWithWeather extracts regression features and weather observations
// relative to baseTime (UTC). If fewer than 3 observations exist or lag values are
// missing, reasonable historical fallback interpolation is performed.
func BuildFeaturesWithWeather(observations []Observation, baseTime time.Time, weatherObs []WeatherObservation) (Features, float64, bool) {
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

	p15, p60, rf, hsr, wcc, ra := ComputeWeatherFeatures(weatherObs, baseTime)

	return Features{
		Lag5m:            lag5m,
		Lag10m:           lag10m,
		Lag15m:           lag15m,
		RollingMean15m:   rollingMean,
		HourOfDay:        hourOfDay,
		DayOfWeek:        dayOfWeek,
		PrecipMm15m:      p15,
		PrecipMm60m:      p60,
		RainFlag:         rf,
		HoursSinceRain:   hsr,
		WeatherCodeClass: wcc,
		RadarActive:      ra,
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
