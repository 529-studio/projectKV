# S4: PCU-Weighted Saturation Index Traffic Severity Mapping — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a configurable, data-driven PCU (Passenger Car Unit) weighted Saturation Index computation in `services/go-server/internal/cvclient` that converts per-class vehicle counts from S3 analyzer output into `CLEAR`, `MODERATE`, `HEAVY`, and `CONGESTED` severity verdicts.

**Architecture:** A two-tier calculation pipeline:
1. Multi-class vehicle counts (motorcycle, car, bus, truck, container) are multiplied by configurable PCU weights to calculate `PCU_total`.
2. A thread-safe `PCUCalculator` normalizes `PCU_total` against the camera's effective capacity (`math.Max(historicalMax, capacityFloor)`) to determine the Saturation Index (SI), which maps into severity buckets. This severity is wired into `CVEngineAnalyzer.Analyze()` to flow directly into the existing verdict storage and SSE pipeline.

**Tech Stack:** Go stdlib (`sync`, `math`, `strconv`, `os`, `encoding/json`), Python (cv-engine output enrichment). Zero external Go dependencies.

**Spec:** https://github.com/529-studio/projectKV/issues/138 (ADR-004 §9 D15, §6 D12)

---

## Global Constraints

- PCU weights must be stored as configuration (env vars with defaults), strictly NOT hardcoded constants (calibratable against B4 eval set per §9 D15).
- Cold-start cameras with zero historical observations must use `CapacityFloor` (default 25.0) to strictly prevent division-by-zero.
- Codebase language: 100% English for code, comments, logs, and docs.
- Zero emojis in all Go code and logs.
- Modulith import graph: `cvclient -> hotset, camera`; `config` is a leaf. No new packages or external dependencies.
- `gofmt -l .` and `go vet ./...` must be 100% clean.

## Review Focus

1. **Cold-start divide-by-zero**: A brand-new camera with 0 historical observations and 0 capacity floor must never panic or produce `NaN`/`+Inf`. Tested with explicit zero-capacity floor test.
2. **Bucket boundary precision**: Exact boundary values (e.g. SI = 0.35, 0.70, 0.90) must map deterministically to the higher severity tier. Tested with epsilon test cases ($SI = \text{threshold} \pm 10^{-6}$).
3. **Single-class dominance**: Extreme distributions (e.g. 100% motorcycles or 100% heavy containers) must produce correct PCU totals according to their weights.
4. **Fallback when per-class counts are absent**: If `vehicle_counts` is missing (backward compatibility with legacy or raw detector), fallback smoothly to `vehicles_detected` without failing.
5. **Thread-safe historical max capacity**: Concurrent requests on the same camera must safely update and read historical capacity without data races (tested with `-race`).

---

## Task 1: Implement `PCUCalculator` & Data Models with Configurable Weights

**Files:**
- Create: `services/go-server/internal/cvclient/pcu.go`
- Create: `services/go-server/internal/cvclient/pcu_test.go`

**Interfaces:**
- Produces:
  - `type VehicleCounts struct { Motorcycle, Car, Bus, Truck, Container int }`
  - `type PCUWeights struct { Motorcycle, Car, Bus, Truck, Container float64 }`
  - `type PCUBucketThresholds struct { Moderate, Heavy, Congested float64 }`
  - `type PCUConfig struct { Weights PCUWeights; Thresholds PCUBucketThresholds; CapacityFloor float64 }`
  - `type PCUCalculator struct`
  - `NewPCUCalculator(cfg PCUConfig) *PCUCalculator`
  - `(c *PCUCalculator) Calculate(cameraID string, counts VehicleCounts, fallbackTotal int) (severity string, pcuTotal float64, saturationIndex float64)`

- [ ] **Step 1: Write failing unit tests in `services/go-server/internal/cvclient/pcu_test.go`**
  - Test zero counts -> `CLEAR`, SI = 0.
  - Test exact bucket boundaries ($SI = 0.35 \rightarrow \text{MODERATE}$, $SI = 0.70 \rightarrow \text{HEAVY}$, $SI = 0.90 \rightarrow \text{CONGESTED}$).
  - Test just below boundaries ($SI = 0.3499 \rightarrow \text{CLEAR}$, $0.6999 \rightarrow \text{MODERATE}$, $0.8999 \rightarrow \text{HEAVY}$).
  - Test single-class dominance (motorcycle 0.2, car 1.0, container 3.0).
  - Test cold-start camera without historical max (verifying floor handles no division by zero).
  - Test capacity expansion (when higher PCU observed, historical max updates).
  - Test concurrent calls with `-race`.

- [ ] **Step 2: Run tests to confirm compilation error / test failure**
  ```bash
  cd services/go-server && go test ./internal/cvclient/... -run TestPCU -v
  ```

- [ ] **Step 3: Implement `pcu.go` in `services/go-server/internal/cvclient/pcu.go`**
  - Implement `VehicleCounts`, `PCUWeights`, `PCUBucketThresholds`, `PCUConfig`.
  - Add documentation explaining weights are calibratable against the B4 evaluation dataset.
  - Implement `PCUCalculator` with `sync.RWMutex` guarding historical capacity per camera.
  - Implement `Calculate`:
    1. Calculate `pcuTotal = Σ(count_i * weight_i)`. If counts empty, fallback to `fallbackTotal * weightCar`.
    2. Lookup/update historical max. `effectiveCapacity = max(historicalMax, capacityFloor, 1.0)`.
    3. `saturationIndex = pcuTotal / effectiveCapacity`.
    4. Map `saturationIndex` against thresholds to return `CLEAR`, `MODERATE`, `HEAVY`, or `CONGESTED`.

- [ ] **Step 4: Run tests to verify all tests pass with race detector**
  ```bash
  cd services/go-server && go test ./internal/cvclient/... -run TestPCU -v -race
  ```

- [ ] **Step 5: Commit Task 1**
  ```bash
  git add services/go-server/internal/cvclient/pcu.go services/go-server/internal/cvclient/pcu_test.go
  git commit -m "feat(cvclient): implement PCUCalculator and bucket threshold mapping"
  ```

---

## Task 2: Add PCU Configuration to `config.ServerConfig`

**Files:**
- Modify: `services/go-server/internal/config/config.go`
- Modify: `services/go-server/internal/config/config_test.go`

**Interfaces:**
- Consumes: Environment variables:
  - `PCU_WEIGHT_MOTORCYCLE` (default 0.2)
  - `PCU_WEIGHT_CAR` (default 1.0)
  - `PCU_WEIGHT_BUS` (default 2.0)
  - `PCU_WEIGHT_TRUCK` (default 2.0)
  - `PCU_WEIGHT_CONTAINER` (default 3.0)
  - `PCU_CAPACITY_FLOOR` (default 25.0)
  - `PCU_THRESHOLD_MODERATE` (default 0.35)
  - `PCU_THRESHOLD_HEAVY` (default 0.70)
  - `PCU_THRESHOLD_CONGESTED` (default 0.90)
- Produces: `ServerConfig.PCUConfig() cvclient.PCUConfig` or fields in `ServerConfig`.

- [ ] **Step 1: Write test for PCU configuration defaults and overrides in `config_test.go`**
  - Test default values when env vars unset.
  - Test custom values when env vars provided.

- [ ] **Step 2: Implement PCU fields in `config.go`**
  - Add fields to `ServerConfig`.
  - Parse env vars in `LoadConfig()` with float parsing helper.

- [ ] **Step 3: Run config tests to verify pass**
  ```bash
  cd services/go-server && go test ./internal/config/... -v -race
  ```

- [ ] **Step 4: Commit Task 2**
  ```bash
  git add services/go-server/internal/config/config.go services/go-server/internal/config/config_test.go
  git commit -m "feat(config): add calibratable PCU weights, capacity floor, and threshold configs"
  ```

---

## Task 3: Wire PCU Calculator into `CVEngineAnalyzer` and `main.go`

**Files:**
- Modify: `services/go-server/internal/cvclient/cvengine.go`
- Modify: `services/go-server/internal/cvclient/cvengine_test.go`
- Modify: `services/go-server/cmd/server/main.go`
- Modify: `services/cv-engine/server.py` (add `vehicle_counts` in predict response)

**Interfaces:**
- Consumes: `cvPredictResponse.VehicleCounts`
- Produces: `Verdict.Severity` computed from PCU Saturation Index and wired into Hot Set pipeline.

- [ ] **Step 1: Update `services/cv-engine/server.py`**
  - Ensure `POST /predict` response includes `vehicle_counts` dict alongside `vehicles_detected`.

- [ ] **Step 2: Update `cvPredictResponse` in `cvengine.go`**
  - Add `VehicleCounts VehicleCounts json:"vehicle_counts,omitempty"` to `cvPredictResponse`.
  - Add `calculator *PCUCalculator` to `CVEngineAnalyzer` and option `WithPCUCalculator(calc *PCUCalculator)`.
  - In `Analyze()`, compute `severity` via `calculator.Calculate(cam.ID, resp.VehicleCounts, resp.VehiclesDetected)`.

- [ ] **Step 3: Update `cvengine_test.go`**
  - Test that `Analyze()` computes severity through PCU calculator when `vehicle_counts` are returned.

- [ ] **Step 4: Wire in `cmd/server/main.go`**
  - In `case "onnx":`, instantiate `pcuCalc := cvclient.NewPCUCalculator(cfg.PCUConfig())` and pass it to `NewCVEngineAnalyzer(cfg.CVEngineURL, nil, cvclient.WithPCUCalculator(pcuCalc))`.

- [ ] **Step 5: Run full test suite with race detector**
  ```bash
  cd services/go-server && go test ./... -race
  ```

- [ ] **Step 6: Commit Task 3**
  ```bash
  git add services/cv-engine/server.py services/go-server/internal/cvclient/cvengine.go services/go-server/internal/cvclient/cvengine_test.go services/go-server/cmd/server/main.go
  git commit -m "feat(cvclient): wire PCU-weighted saturation severity into CVEngineAnalyzer"
  ```

---

## Task 4: Architecture Compliance Review & PR Creation

- [ ] **Step 1: Run Architecture Guardian checklist**
  - Check import graph compliance (`cvclient -> hotset, camera`).
  - Confirm zero banned dependencies.
  - Run `gofmt -l .` and `go vet ./...`.
- [ ] **Step 2: Push branch `feat/s4-pcu-saturation-index` and open PR against `dev`**
  - Title: `feat(go-server): S4 PCU-weighted Saturation Index traffic severity mapping — closes #138`
  - Document bucket boundaries, rationale, and calibratable weights in PR description.
  - Keep PR open in review.
