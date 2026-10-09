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
// CALIBRATION & RESOLUTION NOTE (ADR-004 §9 D15 / ADR-005 T6 / Issue #186):
// The active self-hosted detector is stock COCO-pretrained YOLOv8n, which detects
// vehicles across 4 classes: Motorcycle, Car, Bus, and Truck.
// Heavy freight and container trucks are detected under Truck. Per the architectural
// decision in docs/decision-container-class-gap.md, Container is folded into the
// heavy commercial vehicle weight (Truck = 2.0) to eliminate dead code while
// maintaining backward compatibility for API payloads that include the Container field.
// ==============================================================================
type PCUWeights struct {
	Motorcycle float64 `json:"motorcycle"`
	Car        float64 `json:"car"`
	Bus        float64 `json:"bus"`
	Truck      float64 `json:"truck"`
	Container  float64 `json:"container"`
}

// DefaultPCUWeights returns the calibrated weights per empirical HCMC traffic analysis
// and Vietnamese urban transport standards (TCVN 4054:2005 / TCVN 13592:2022).
// Container is folded into Truck (2.0) to align with COCO 4-class vehicle detection
// per decision-container-class-gap.md (Issue #186 / ADR-005 T6).
func DefaultPCUWeights() PCUWeights {
	return PCUWeights{
		Motorcycle: 0.3, // Calibrated from 0.2: accounts for HCMC mixed-traffic filtering, swerving, and dynamic lane occupancy
		Car:        1.0, // Standard passenger car baseline
		Bus:        2.5, // Calibrated from 2.0: TCVN urban bus standard accounting for frequent boarding stops and lane blockage
		Truck:      2.0, // Standard medium/heavy commercial vehicle
		Container:  2.0, // Folded into Truck (2.0) per ADR-005 T6
	}
}

// PCUBucketThresholds defines the Saturation Index (SI) cutoffs for traffic severity.
//
// RATIONALE & BUCKET BOUNDARIES (Calibrated for Issue #196):
//   - CLEAR (SI < Moderate, default < 0.30):
//     Corresponds to Level of Service (LOS) A/B. Vehicle flow is unimpeded, road is
//     operating at low density, and travel speeds are near free-flow velocity.
//   - MODERATE (Moderate <= SI < Heavy, default 0.30 <= SI < 0.60):
//     Corresponds to LOS C/D. Stable flow with noticeable vehicle density. Minor
//     queues form at signalized intersections but clear within a single green cycle.
//   - HEAVY (Heavy <= SI < Congested, default 0.60 <= SI < 0.80):
//     Corresponds to LOS E. Unstable flow approaching physical roadway capacity.
//     Speed is significantly restricted; stop-and-go waves begin to propagate.
//   - CONGESTED (SI >= Congested, default SI >= 0.80):
//     Corresponds to LOS F (Breakdown / Gridlock). Traffic demand meets or exceeds
//     effective roadway capacity. Multi-cycle clearance delays and intersection blockages occur.
type PCUBucketThresholds struct {
	Moderate  float64 `json:"moderate"`  // default: 0.30 (calibrated from 0.35)
	Heavy     float64 `json:"heavy"`     // default: 0.60 (calibrated from 0.70)
	Congested float64 `json:"congested"` // default: 0.80 (calibrated from 0.90)
}

// DefaultPCUBucketThresholds returns the calibrated baseline bucket boundaries.
func DefaultPCUBucketThresholds() PCUBucketThresholds {
	return PCUBucketThresholds{
		Moderate:  0.30,
		Heavy:     0.60,
		Congested: 0.80,
	}
}

// PCUConfig packages all parameters required by the PCUCalculator.
type PCUConfig struct {
	Weights       PCUWeights
	Thresholds    PCUBucketThresholds
	CapacityFloor float64 // default: 15.0 (calibrated from 25.0)
}

// DefaultPCUConfig provides sensible defaults conforming to ADR-004 §9 D15 and calibrated under Issue #196.
func DefaultPCUConfig() PCUConfig {
	return PCUConfig{
		Weights:       DefaultPCUWeights(),
		Thresholds:    DefaultPCUBucketThresholds(),
		CapacityFloor: 15.0,
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
	// Container count is weighted using c.weights.Container (defaults to c.weights.Truck = 2.0).
	if counts.Total() > 0 {
		containerWeight := c.weights.Container
		if containerWeight <= 0 {
			containerWeight = c.weights.Truck
		}
		pcuTotal = float64(counts.Motorcycle)*c.weights.Motorcycle +
			float64(counts.Car)*c.weights.Car +
			float64(counts.Bus)*c.weights.Bus +
			float64(counts.Truck)*c.weights.Truck +
			float64(counts.Container)*containerWeight
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
