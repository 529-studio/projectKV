# PCU Weights & Saturation Thresholds Calibration Report

- **Issue**: [#196](https://github.com/529-studio/projectKV/issues/196) (Sprint 4 T5)
- **Related ADR**: ADR-005 (§2 Finding C, Finding D, §6 row T5)
- **Status**: Completed
- **Date**: 2026-10-09
- **Author**: Antigravity AI & `@thdat-vu`

---

## 1. Executive Summary

Prior to this calibration, the Passenger Car Unit (PCU) calculation formula in `services/go-server/internal/cvclient/pcu.go` relied on initial, uncalibrated placeholder defaults:
- `Motorcycle`: 0.20
- `Car`: 1.00
- `Bus`: 2.00
- `Truck`: 2.00
- `Container`: 2.00 (folded into Truck per ADR-005 T6)
- `CapacityFloor`: 25.0 PCU
- `Moderate Threshold`: 0.35
- `Heavy Threshold`: 0.70
- `Congested Threshold`: 0.90

During the Sprint 4 T4 benchmark ([Issue #195](https://github.com/529-studio/projectKV/issues/195) / [eval_report_d6_real.md](../docs/reports/eval_report_d6_real.md)), evaluation across 764 real labeled frames revealed severe classification distortion:
1. **Uncalibrated Capacity Floor Distortion**: A fixed camera viewport at 320x320 resolution captures an instantaneous 20–30 meter road segment, physically accommodating at most 12–16 PCU before bumper-to-bumper queue lock. A cold-start capacity floor of `25.0` required unrealistically high vehicle volumes (>17.5 PCU for `HEAVY`, >22.5 PCU for `CONGESTED`), causing 94.4% of actual `HEAVY` frames and 94.5% of `CONGESTED` frames to be misclassified as `CLEAR`.
2. **Vietnamese Traffic Engineering Discrepancy**: The Western default weight of 0.20 for motorcycles assumed disciplined single-file lane alignment. In Ho Chi Minh City's mixed urban traffic, motorcycles operate 3–4 abreast per lane, filtering through gaps, swerving, and generating significant lateral clearance friction. Under Vietnamese standards (**TCVN 4054:2005** and **TCVN 13592:2022**), motorcycle equivalents range from 0.25 to 0.33, and urban buses require 2.50 to reflect boarding dwell delays and multi-lane blockage.

This calibration updates the default constants in `services/go-server/internal/cvclient/pcu.go` and `services/go-server/internal/config/config.go` while preserving full backward compatibility with operator environment variable overrides.

---

## 2. Parameter Comparison Table

| Parameter | Old Default | Calibrated Default | Standard / Empirical Basis |
|---|:---:|:---:|---|
| **`PCU_WEIGHT_MOTORCYCLE`** | `0.20` | **`0.30`** | TCVN 4054:2005 Table 4 & TCVN 13592:2022 §6.2 (mixed urban motorcycle filtering & lateral clearance friction) |
| **`PCU_WEIGHT_CAR`** | `1.00` | **`1.00`** | Reference Passenger Car Unit (baseline standard) |
| **`PCU_WEIGHT_BUS`** | `2.00` | **`2.50`** | TCVN urban bus coefficient accounting for transit dwell stops, acceleration lag, and physical blind spots |
| **`PCU_WEIGHT_TRUCK`** | `2.00` | **`2.00`** | Standard medium/heavy commercial vehicle (unchanged) |
| **`PCU_WEIGHT_CONTAINER`** | `2.00` | **`2.00`** | Folded into Truck per ADR-005 T6 ([decision-container-class-gap.md](../docs/decision-container-class-gap.md)) |
| **`PCU_CAPACITY_FLOOR`** | `25.0` | **`15.0`** | Instantaneous physical queue capacity of a 20–30m CCTV viewport (3 lanes x 4–5 queue vehicles) |
| **`PCU_THRESHOLD_MODERATE`** | `0.35` | **`0.30`** | LOS C/D boundary: noticeable platoon queuing clearing within one green signal cycle (~4.5 PCU) |
| **`PCU_THRESHOLD_HEAVY`** | `0.70` | **`0.60`** | LOS E boundary: unstable flow, stop-and-go waves, approach saturation (~9.0 PCU) |
| **`PCU_THRESHOLD_CONGESTED`** | `0.90` | **`0.80`** | LOS F boundary: queue spillover, gridlock, multi-cycle clearance delays (~12.0 PCU) |

---

## 3. Mathematical & Empirical Methodology

### 3.1 Instantaneous Camera Viewport vs. Hourly Link Capacity

The previous `CapacityFloor = 25.0` was set under an assumption derived from hourly roadway link capacity (1,500–2,000 PCU/hour/lane). However:
- Traffic cameras take instantaneous still frame snapshots (or 30s sampled snapshots).
- The optical field of view of Ho Chi Minh City Department of Transport CCTV cameras covers a localized intersection approach (~20 to 30 meters).
- Physical road geometry in HCMC arterials (such as Điện Biên Phủ, Cộng Hòa, or Võ Văn Kiệt):
  $$\text{Visual Capacity} = N_{\text{lanes}} \times \left( \frac{\text{Viewport Length}}{\text{Mean Vehicle Space Headway}} \right)$$
  For a typical 3-lane arterial segment ($L \approx 25\text{m}$, space headway in queue $\approx 6\text{m}$ for cars or $2\text{m}$ for motorcycles):
  $$\text{Max Car Queue} = 3 \times 4.2 \approx 12.6\text{ cars} \implies 12.6\text{ PCU}$$
  $$\text{Max Motorcycle Queue} = 3 \times 12.5 \approx 37.5\text{ motorcycles} \times 0.30 \implies 11.25\text{ PCU}$$
  $$\text{Mixed Flow Queue (3 cars + 30 motorcycles)} = (3 \times 1.0) + (30 \times 0.30) = 12.0\text{ PCU}$$
- Physical queue saturation occurs between **12.0 and 15.0 PCU**.
- Setting `CapacityFloor = 15.0` ensures cold-start cameras accurately measure saturation without requiring impossible vehicle volumes. When wide corridors (e.g. Mai Chí Thọ 12-lane boulevard) observe higher volumes (e.g. 40–50 PCU), the adaptive historical maximum mechanism (`histMax`) expands effective capacity dynamically:
  $$\text{EffectiveCapacity} = \max(\text{histMax}, \text{CapacityFloor})$$

### 3.2 Motorcycle Equivalent Calibration (0.20 to 0.30)

In Western traffic guidelines (e.g., Highway Capacity Manual HCM 2010), motorcycles receive $PCU \approx 0.20$ based on single-file lane following. In Vietnam:
- Under **TCVN 4054:2005** (Highway Specifications, Table 4), the passenger car equivalent for motorcycles is defined as:
  $$\text{PCU}_{\text{motorcycle}} = 0.30$$
- Under **TCVN 13592:2022** (Urban Road Design), mixed motorcycle streams with lateral weaving and filtering reduce lane throughput per unit area by approximately 30% compared to pure car streams.
- At 0.20, 20 motorcycles yielded only 4.0 PCU, severely under-representing intersection choke. At 0.30, 20 motorcycles yield 6.0 PCU, correctly representing the space consumption of two standard passenger cars.

### 3.3 Urban Bus Weight Calibration (2.00 to 2.50)

- Under **TCVN 13592:2022**, standard transit buses in urban corridors receive $PCU = 2.50$.
- Buses generate heavy friction at curbside stops, pulling in and out across motorcycle lanes, causing shockwave decelerations behind them. Elevating bus weight from 2.0 to 2.5 reflects this empirical reality.

### 3.4 Saturation Index (SI) Level of Service (LOS) Boundaries

The Saturation Index is defined as:
$$SI = \frac{PCU_{\text{total}}}{\text{EffectiveCapacity}}$$

The severity mappings are recalibrated as follows:
- **`CLEAR` (LOS A/B, $SI < 0.30$)**: Free-flow conditions. Vehicle count is low ($PCU < 4.5$).
- **`MODERATE` (LOS C/D, $0.30 \le SI < 0.60$)**: Steady flow with localized queues ($4.5 \le PCU < 9.0$). Queues clear within a single traffic light phase.
- **`HEAVY` (LOS E, $0.60 \le SI < 0.80$)**: Density approaches geometric lane capacity ($9.0 \le PCU < 12.0$). Speed is restricted; stop-and-go waves begin.
- **`CONGESTED` (LOS F, $SI \ge 0.80$)**: Breakdown / gridlock ($PCU \ge 12.0$). Queue spills backward into upstream intersections; multi-cycle clearance delays occur.

---

## 4. Backward Compatibility & Overrides

All updated defaults can be overridden at runtime via standard environment variables without code modification:

```bash
# Custom PCU vehicle class weights
PCU_WEIGHT_MOTORCYCLE=0.35
PCU_WEIGHT_CAR=1.00
PCU_WEIGHT_BUS=2.50
PCU_WEIGHT_TRUCK=2.00
PCU_WEIGHT_CONTAINER=2.00

# Capacity floor and bucket thresholds
PCU_CAPACITY_FLOOR=18.0
PCU_THRESHOLD_MODERATE=0.30
PCU_THRESHOLD_HEAVY=0.60
PCU_THRESHOLD_CONGESTED=0.80
```

Unit test [`TestLoadConfig_PCU_Custom`](../services/go-server/internal/config/config_test.go#L365-L415) rigorously validates that all custom environment variables override the defaults.

---

## 5. Verification Evidence

### 5.1 Unit Tests
All unit tests in `internal/cvclient` and `internal/config` pass 100%:

```
$ make test-pcu
cd services/go-server && go test ./internal/cvclient/... -run TestPCU -v
=== RUN   TestPCUCalculator_ZeroCounts
--- PASS: TestPCUCalculator_ZeroCounts (0.00s)
=== RUN   TestPCUCalculator_SingleClassDominance
--- PASS: TestPCUCalculator_SingleClassDominance (0.00s)
=== RUN   TestPCUCalculator_BucketBoundaries
--- PASS: TestPCUCalculator_BucketBoundaries (0.00s)
=== RUN   TestPCUCalculator_ColdStartAndFloorGuards
--- PASS: TestPCUCalculator_ColdStartAndFloorGuards (0.00s)
=== RUN   TestPCUCalculator_HistoricalCapacityTracking
--- PASS: TestPCUCalculator_HistoricalCapacityTracking (0.00s)
=== RUN   TestPCUCalculator_FallbackWhenPerClassMissing
--- PASS: TestPCUCalculator_FallbackWhenPerClassMissing (0.00s)
=== RUN   TestPCUCalculator_ThreadSafety
--- PASS: TestPCUCalculator_ThreadSafety (0.00s)
=== RUN   TestPCUCalculator_ContainerFoldedIntoTruck
--- PASS: TestPCUCalculator_ContainerFoldedIntoTruck (0.00s)
PASS
ok  	github.com/529-studio/projectKV/services/go-server/internal/cvclient	0.547s
```

Full Go test suite with race detector:
```
$ make test-go
cd services/go-server && go test ./... -race
ok  	github.com/529-studio/projectKV/services/go-server/cmd/server
ok  	github.com/529-studio/projectKV/services/go-server/internal/archiver
ok  	github.com/529-studio/projectKV/services/go-server/internal/camera
ok  	github.com/529-studio/projectKV/services/go-server/internal/config
ok  	github.com/529-studio/projectKV/services/go-server/internal/corridor
ok  	github.com/529-studio/projectKV/services/go-server/internal/cvclient
ok  	github.com/529-studio/projectKV/services/go-server/internal/hotset
ok  	github.com/529-studio/projectKV/services/go-server/internal/httpserver
ok  	github.com/529-studio/projectKV/services/go-server/internal/live
ok  	github.com/529-studio/projectKV/services/go-server/internal/middleware
ok  	github.com/529-studio/projectKV/services/go-server/internal/proxycache
ok  	github.com/529-studio/projectKV/services/go-server/internal/storage
ok  	github.com/529-studio/projectKV/services/go-server/internal/store
ok  	github.com/529-studio/projectKV/services/go-server/internal/tilecache
ok  	github.com/529-studio/projectKV/services/go-server/internal/weather
ok  	github.com/529-studio/projectKV/services/go-server/internal/ws
```

### 5.2 Formatting & Linting
- `gofmt -l .` reports zero files.
- `go vet ./...` completed with zero errors or warnings.
- `python3 -m unittest discover tools/eval/` (34 tests) passed with 100% success.
- Architecture Guardian review verified:
  - Modulith leaf package boundaries strictly respected.
  - Zero banned dependencies (no ORMs, no Viper, no message brokers).
  - Only standard libraries and approved dependencies used.
