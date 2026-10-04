package cvclient

import (
	"math"
	"sync"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// VehicleCounts represents detected vehicle counts broken down by classification.
// Output from S3's OpenVINO / YOLO object detector.
type VehicleCounts struct {
	Motorcycle int `json:"motorcycle"`
	Car        int `json:"car"`
	Bus        int `json:"bus"`
	Truck      int `json:"truck"`
	Container  int `json:"container"`
}

// Total returns the raw sum of all vehicle counts regardless of class.
func (v VehicleCounts) Total() int {
	return v.Motorcycle + v.Car + v.Bus + v.Truck + v.Container
}

// PCUWeights defines the passenger car equivalent units for each vehicle class.
//
// ==============================================================================
// CALIBRATION NOTE (ADR-004 §9 D15 / Issue #138):
// These weights are a starting baseline and MUST NOT be treated as fixed or final.
// Motorcycle PCU values in published Vietnamese and Southeast Asian urban traffic
// engineering studies vary significantly depending on lane discipline, lateral
// friction, and average speed (ranging from 0.20 to 0.35+). These values are fully
// configurable via environment variables and must be calibrated against the B4
// evaluation dataset once real human-labeled frames exist.
// ==============================================================================
type PCUWeights struct {
	Motorcycle float64 `json:"motorcycle"`
	Car        float64 `json:"car"`
	Bus        float64 `json:"bus"`
	Truck      float64 `json:"truck"`
	Container  float64 `json:"container"`
}

// DefaultPCUWeights returns the starting calibratable baseline weights from ADR-004 §9 D15.
func DefaultPCUWeights() PCUWeights {
	return PCUWeights{
		Motorcycle: 0.2,
		Car:        1.0,
		Bus:        2.0,
		Truck:      2.0,
		Container:  3.0,
	}
}

// PCUBucketThresholds defines the Saturation Index (SI) cutoffs for traffic severity.
//
// RATIONALE & BUCKET BOUNDARIES:
//   - CLEAR (SI < Moderate, default < 0.35):
//     Corresponds to Level of Service (LOS) A/B. Vehicle flow is unimpeded, road is
//     operating at low density, and travel speeds are near free-flow velocity.
//   - MODERATE (Moderate <= SI < Heavy, default 0.35 <= SI < 0.70):
//     Corresponds to LOS C/D. Stable flow with noticeable vehicle density. Minor
//     queues form at signalized intersections but clear within a single green cycle.
//   - HEAVY (Heavy <= SI < Congested, default 0.70 <= SI < 0.90):
//     Corresponds to LOS E. Unstable flow approaching physical roadway capacity.
//     Speed is significantly restricted; stop-and-go waves begin to propagate.
//   - CONGESTED (SI >= Congested, default SI >= 0.90):
//     Corresponds to LOS F (Breakdown / Gridlock). Traffic demand meets or exceeds
//     effective roadway capacity. Multi-cycle clearance delays and intersection blockages occur.
type PCUBucketThresholds struct {
	Moderate  float64 `json:"moderate"`  // default: 0.35
	Heavy     float64 `json:"heavy"`     // default: 0.70
	Congested float64 `json:"congested"` // default: 0.90
}

// DefaultPCUBucketThresholds returns the baseline bucket boundaries.
func DefaultPCUBucketThresholds() PCUBucketThresholds {
	return PCUBucketThresholds{
		Moderate:  0.35,
		Heavy:     0.70,
		Congested: 0.90,
	}
}

// PCUConfig packages all parameters required by the PCUCalculator.
type PCUConfig struct {
	Weights       PCUWeights
	Thresholds    PCUBucketThresholds
	CapacityFloor float64 // default: 25.0
}

// DefaultPCUConfig provides sensible defaults conforming to ADR-004 §9 D15.
func DefaultPCUConfig() PCUConfig {
	return PCUConfig{
		Weights:       DefaultPCUWeights(),
		Thresholds:    DefaultPCUBucketThresholds(),
		CapacityFloor: 25.0,
	}
}

// PCUCalculator computes the PCU-weighted Saturation Index for camera frames
// and maps the result to traffic severity levels. It maintains thread-safe
// historical peak observations per camera to establish realistic capacity baselines.
type PCUCalculator struct {
	weights       PCUWeights
	thresholds    PCUBucketThresholds
	capacityFloor float64

	mu         sync.RWMutex
	capacities map[string]float64
}

// NewPCUCalculator constructs a PCUCalculator initialized with cfg.
func NewPCUCalculator(cfg PCUConfig) *PCUCalculator {
	floor := cfg.CapacityFloor
	if floor <= 0 {
		floor = 1.0 // Strictly guard against divide-by-zero or zero floor
	}
	return &PCUCalculator{
		weights:       cfg.Weights,
		thresholds:    cfg.Thresholds,
		capacityFloor: floor,
		capacities:    make(map[string]float64),
	}
}

// Calculate computes the total PCU score and Saturation Index (SI) for cameraID,
// returning the corresponding traffic severity label (CLEAR, MODERATE, HEAVY, CONGESTED).
//
// Cold-Start & Divide-by-Zero Handling (ADR-004 §6 D12):
// Brand-new cameras lack historical maximum counts. The calculator uses capacityFloor
// until a higher peak is observed, guaranteeing that effective capacity is strictly positive.
func (c *PCUCalculator) Calculate(cameraID string, counts VehicleCounts, fallbackTotal int) (severity string, pcuTotal float64, saturationIndex float64) {
	// Step 1: Compute total PCU.
	if counts.Total() > 0 {
		pcuTotal = float64(counts.Motorcycle)*c.weights.Motorcycle +
			float64(counts.Car)*c.weights.Car +
			float64(counts.Bus)*c.weights.Bus +
			float64(counts.Truck)*c.weights.Truck +
			float64(counts.Container)*c.weights.Container
	} else if fallbackTotal > 0 {
		// Fallback for detectors without class separation: treat each vehicle as a standard car.
		pcuTotal = float64(fallbackTotal) * c.weights.Car
	} else {
		return hotset.SeverityClear, 0.0, 0.0
	}

	// Step 2: Update historical peak capacity for this camera.
	c.mu.Lock()
	histMax := c.capacities[cameraID]
	if pcuTotal > histMax {
		histMax = pcuTotal
		c.capacities[cameraID] = histMax
	}
	c.mu.Unlock()

	// Step 3: Compute Saturation Index (SI).
	effectiveCapacity := math.Max(histMax, c.capacityFloor)
	if effectiveCapacity < 1.0 {
		effectiveCapacity = 1.0
	}
	saturationIndex = pcuTotal / effectiveCapacity

	// Step 4: Map Saturation Index into severity bucket.
	switch {
	case saturationIndex >= c.thresholds.Congested:
		severity = hotset.SeverityCongested
	case saturationIndex >= c.thresholds.Heavy:
		severity = hotset.SeverityHeavy
	case saturationIndex >= c.thresholds.Moderate:
		severity = hotset.SeverityModerate
	default:
		severity = hotset.SeverityClear
	}

	return severity, pcuTotal, saturationIndex
}

// GetCapacity returns the current effective capacity for cameraID.
func (c *PCUCalculator) GetCapacity(cameraID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return math.Max(c.capacities[cameraID], c.capacityFloor)
}

// SetCapacity overrides the learned capacity for cameraID (useful for testing or pre-calibration).
func (c *PCUCalculator) SetCapacity(cameraID string, capacity float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capacities[cameraID] = capacity
}
