package cvclient

import (
	"context"
	"math"
	"sync"
	"time"

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
	Weights         PCUWeights
	Thresholds      PCUBucketThresholds
	CapacityFloor   float64 // default: 15.0 (calibrated from 25.0)
	HysteresisDelta float64 // default: 0.05 (Schmitt trigger deadband preventing state chattering)
}

// DefaultPCUConfig provides sensible defaults conforming to ADR-004 §9 D15 and calibrated under Issue #196.
func DefaultPCUConfig() PCUConfig {
	return PCUConfig{
		Weights:         DefaultPCUWeights(),
		Thresholds:      DefaultPCUBucketThresholds(),
		CapacityFloor:   15.0,
		HysteresisDelta: 0.05,
	}
}

// CapacityStore defines the persistence interface for learned camera capacities across restarts.
type CapacityStore interface {
	Save(ctx context.Context, cameraID string, learnedMax float64) error
	GetAll(ctx context.Context) (map[string]float64, error)
}

// PCUCalculator computes the PCU-weighted Saturation Index for camera frames
// and maps the result to traffic severity levels. It maintains thread-safe
// historical peak observations per camera to establish realistic capacity baselines.
type PCUCalculator struct {
	weights         PCUWeights
	thresholds      PCUBucketThresholds
	capacityFloor   float64
	hysteresisDelta float64

	mu             sync.RWMutex
	capacities     map[string]float64
	baseCapacities map[string]float64
	lastSeverities map[string]string
	store          CapacityStore
	persistWG      sync.WaitGroup
}

// NewPCUCalculator constructs a PCUCalculator initialized with cfg.
func NewPCUCalculator(cfg PCUConfig) *PCUCalculator {
	floor := cfg.CapacityFloor
	if floor <= 0 {
		floor = 1.0 // Strictly guard against divide-by-zero or zero floor
	}
	delta := cfg.HysteresisDelta
	if delta <= 0 {
		delta = 0.05
	}
	return &PCUCalculator{
		weights:         cfg.Weights,
		thresholds:      cfg.Thresholds,
		capacityFloor:   floor,
		hysteresisDelta: delta,
		capacities:      make(map[string]float64),
		baseCapacities:  make(map[string]float64),
		lastSeverities:  make(map[string]string),
	}
}

// SetBaseCapacity registers the physical corridor design capacity for cameraID
// (e.g., 40.0 - 50.0 PCU for 10-12 lane expressways, 25.0 - 30.0 for 4-6 lane arterials).
func (c *PCUCalculator) SetBaseCapacity(cameraID string, capacity float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if capacity > 0 {
		c.baseCapacities[cameraID] = capacity
	}
}

// GetBaseCapacity returns the corridor base capacity for cameraID, falling back
// to capacityFloor if no corridor-specific capacity is registered.
func (c *PCUCalculator) GetBaseCapacity(cameraID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap, ok := c.baseCapacities[cameraID]; ok && cap > 0 {
		return cap
	}
	return c.capacityFloor
}

// CapacityFloor returns the global fallback capacity floor.
func (c *PCUCalculator) CapacityFloor() float64 {
	return c.capacityFloor
}

// SetCapacityStore attaches a persistence store to save newly learned peak capacities.
func (c *PCUCalculator) SetCapacityStore(store CapacityStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
}

// LoadLearnedCapacities seeds the calculator with historical peak capacities from store.
func (c *PCUCalculator) LoadLearnedCapacities(ctx context.Context) error {
	c.mu.RLock()
	s := c.store
	c.mu.RUnlock()
	if s == nil {
		return nil
	}
	caps, err := s.GetAll(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, val := range caps {
		if val > c.capacities[id] {
			c.capacities[id] = val
		}
	}
	return nil
}

// WaitPersist blocks until all asynchronous capacity persistence writes finish.
// Primarily used in unit tests and graceful shutdown.
func (c *PCUCalculator) WaitPersist() {
	c.persistWG.Wait()
}

// Calculate computes the total PCU score and Saturation Index (SI) for cameraID,
// returning the corresponding traffic severity label (CLEAR, MODERATE, HEAVY, CONGESTED).
//
// Cold-Start & Divide-by-Zero Handling (ADR-004 §6 D12):
// Brand-new cameras lack historical maximum counts. The calculator uses the corridor-specific
// base capacity (or capacityFloor fallback) until a higher peak is observed, guaranteeing that
// effective capacity is strictly positive and proportional to physical lane geometry.
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

	// Step 2: Update historical peak capacity for this camera and retrieve previous severity.
	c.mu.Lock()
	histMax := c.capacities[cameraID]
	needsPersist := false
	if pcuTotal > histMax {
		histMax = pcuTotal
		c.capacities[cameraID] = histMax
		needsPersist = true
	}
	baseFloor := c.capacityFloor
	if base, ok := c.baseCapacities[cameraID]; ok && base > 0 {
		baseFloor = base
	}
	prevSev := c.lastSeverities[cameraID]
	store := c.store
	c.mu.Unlock()

	if needsPersist && store != nil {
		c.persistWG.Add(1)
		go func(id string, learnedMax float64) {
			defer c.persistWG.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = store.Save(ctx, id, learnedMax)
		}(cameraID, histMax)
	}

	// Step 3: Compute Saturation Index (SI).
	effectiveCapacity := math.Max(histMax, baseFloor)
	if effectiveCapacity < 1.0 {
		effectiveCapacity = 1.0
	}
	saturationIndex = pcuTotal / effectiveCapacity

	// Step 4: Map Saturation Index into severity bucket with Hysteresis (Schmitt Trigger).
	// Prevents rapid ping-pong / chattering around boundary thresholds.
	severity = c.mapSeverityWithHysteresis(saturationIndex, prevSev)

	c.mu.Lock()
	c.lastSeverities[cameraID] = severity
	c.mu.Unlock()

	return severity, pcuTotal, saturationIndex
}

// mapSeverityWithHysteresis applies a Schmitt trigger deadband so that state transitions
// do not flicker rapidly when traffic hovers right at threshold boundaries.
func (c *PCUCalculator) mapSeverityWithHysteresis(si float64, prev string) string {
	d := c.hysteresisDelta

	switch prev {
	case hotset.SeverityCongested:
		// Falling from CONGESTED: requires dropping below Congested - d (e.g. 0.75)
		if si < c.thresholds.Congested-d {
			if si < c.thresholds.Heavy-d {
				if si < c.thresholds.Moderate-d {
					return hotset.SeverityClear
				}
				return hotset.SeverityModerate
			}
			return hotset.SeverityHeavy
		}
		return hotset.SeverityCongested

	case hotset.SeverityHeavy:
		// Rising to CONGESTED:
		if si >= c.thresholds.Congested {
			return hotset.SeverityCongested
		}
		// Falling from HEAVY: requires dropping below Heavy - d (e.g. 0.55)
		if si < c.thresholds.Heavy-d {
			if si < c.thresholds.Moderate-d {
				return hotset.SeverityClear
			}
			return hotset.SeverityModerate
		}
		return hotset.SeverityHeavy

	case hotset.SeverityModerate:
		// Rising to CONGESTED or HEAVY:
		if si >= c.thresholds.Congested {
			return hotset.SeverityCongested
		}
		if si >= c.thresholds.Heavy {
			return hotset.SeverityHeavy
		}
		// Falling from MODERATE: requires dropping below Moderate - d (e.g. 0.25)
		if si < c.thresholds.Moderate-d {
			return hotset.SeverityClear
		}
		return hotset.SeverityModerate

	default: // SeverityClear or cold-start
		switch {
		case si >= c.thresholds.Congested:
			return hotset.SeverityCongested
		case si >= c.thresholds.Heavy:
			return hotset.SeverityHeavy
		case si >= c.thresholds.Moderate:
			return hotset.SeverityModerate
		default:
			return hotset.SeverityClear
		}
	}
}

// GetLastSeverity returns the last observed severity for cameraID.
func (c *PCUCalculator) GetLastSeverity(cameraID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSeverities[cameraID]
}

// GetCapacity returns the current effective capacity for cameraID.
func (c *PCUCalculator) GetCapacity(cameraID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	baseFloor := c.capacityFloor
	if base, ok := c.baseCapacities[cameraID]; ok && base > 0 {
		baseFloor = base
	}
	return math.Max(c.capacities[cameraID], baseFloor)
}

// SetCapacity overrides the learned capacity for cameraID (useful for testing or pre-calibration).
func (c *PCUCalculator) SetCapacity(cameraID string, capacity float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capacities[cameraID] = capacity
}
