# Architectural Decision: Resolving Container Vehicle Class Gap

**Issue**: [#186](https://github.com/529-studio/projectKV/issues/186) (Sprint 4 Backlog Item T6)  
**Status**: Decided  
**Date**: 2026-10-08  
**Author**: Warded Architecture Team  
**References**:
- [ADR-004](ADR-004-route-check-and-snapshot-archive.md) §9 D15 (PCU Saturation Index Specification)
- [ADR-005](ADR-005-sprint3-audit-and-sprint4-plan.md) §2 Finding D and §6 Row T6
- [Spike #104](spike-cv-runtime.md) (Computer Vision Runtime Evaluation on Non-AVX Hardware)
- [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md) §2 (Modulith Import Boundaries)

---

## 1. Problem Statement

In [ADR-004](ADR-004-route-check-and-snapshot-archive.md) §9 D15, starting baseline Passenger Car Unit (PCU) weights were defined as:
```text
Motorcycle = 0.2, Car = 1.0, Bus = 2.0, Truck = 2.0, Container = 3.0
```

During Sprint 3, the project migrated from cloud VLM inference to self-hosted OpenVINO inference using stock COCO-pretrained YOLOv8n (`yolov8n_openvino_320`). Standard COCO includes only 80 classes, covering 4 vehicle classes:
- `car` (COCO class 2)
- `motorcycle` (COCO class 3)
- `bus` (COCO class 5)
- `truck` (COCO class 7)

There is **no dedicated `container` class** in standard COCO. Consequently, `services/cv-engine/server.py` always returns `container: 0`. In `services/go-server/internal/cvclient/pcu.go`, the calculation `float64(counts.Container) * c.weights.Container` was permanently multiplying by zero. The distinct `Container = 3.0` weight existed as dead code.

Per ADR-005 Finding D and Issue #186, an explicit architectural decision must be evaluated and recorded with written technical rationale before implementing code changes.

---

## 2. Evaluation of Options

### Option (a) — Drop `Container` as a distinct PCU weight and fold into `Truck` (Recommended)

- **Description**: Treat container vehicles under the unified heavy freight vehicle class (`Truck = 2.0`). Any incoming `Container` count is folded into `Truck` or weighted equally to `Truck` (`w.Truck`).
- **Technical & Traffic Engineering Rationale**:
  1. **Ground truth alignment with COCO**: In the COCO dataset, large commercial freight vehicles (including semi-trailers, articulated trucks, and flatbeds carrying shipping containers) are labeled under the `truck` class. The detector already detects these vehicles; it simply labels them as `truck`.
  2. **Corridor traffic regulations in HCMC**: Under HCMC People's Committee Decision No. 23/2018/QĐ-UBND, heavy commercial vehicles and container semi-trailers are banned during daytime peak hours (06:00–09:00 and 16:00–20:00) on inner-city arterial corridors. The 57 Hot Set cameras are located on commuter corridors (Võ Văn Kiệt, Mai Chí Thọ, Điện Biên Phủ, Cộng Hòa), not inside the Cát Lái port terminal itself.
  3. **Traffic engineering standards**: According to Vietnamese road design standards (TCVN 10380:2014) and Highway Capacity Manual (HCM 2010), commercial heavy vehicles typically have a PCU equivalent of 2.0 to 2.5. A separate 3.0 multiplier for containers is unnecessary when heavy vehicles are uniformly captured under `Truck = 2.0`.
  4. **Code simplicity & zero dead code**: Completely eliminates dead code branches. If external payloads or legacy fixtures include `Container`, it is treated with the same weight as `Truck` (2.0), avoiding artificial divergence.
  5. **Operational overhead**: Zero. Does not increase CPU usage, memory, or model complexity.

### Option (b) — Add dedicated container detection heuristic for port corridors

- **Description**: Apply a secondary bounding box aspect-ratio or dimensional heuristic in `server.py` on detected `truck` bounding boxes to distinguish long tractor-trailers.
- **Why Rejected**:
  1. Camera tilt angles, perspective distortion, and optical foreshortening vary widely across HCMC traffic cameras without 3D extrinsic/intrinsic calibration matrices.
  2. Aspect-ratio heuristics on 320x320 low-resolution inputs produce severe false positives (e.g., box trucks viewed from side angles misclassified as containers, or containers viewed front-on misclassified as standard trucks).
  3. None of the 57 Hot Set cameras are within the Cát Lái port terminal. Introducing unverified heuristics on arterial commuter corridors introduces arbitrary noise.

### Option (c) — Deploy a specialized multi-class model for container corridors

- **Description**: Maintain a secondary custom model (e.g., DatapolisX 5-class model) for cameras near port corridors while running COCO YOLOv8n for other cameras.
- **Why Rejected**:
  1. **Hardware performance**: Spike #104 established that DatapolisX caused high inference latencies (>1.5s–2.5s per frame on non-AVX Celeron hardware), violating the 30-second Hot Set cycle budget.
  2. **Memory limits**: Staging containers on `local-edge host` are constrained to 512MB RAM (`docker-compose.stg.yml`). Loading two concurrent OpenVINO models risks out-of-memory crashes.
  3. **No validated model weights**: The repository currently only maintains `yolov8n.xml`/`.bin` for COCO. Fine-tuning and validating a new custom model contradicts Sprint 4's explicit mandate ("verification and closing debt; no speculative runtime expansions").

---

## 3. Decision

**Option (a) is selected.**

### Implementation Blueprint:

1. **PCU Calculation (`services/go-server/internal/cvclient/pcu.go`)**:
   - Update `DefaultPCUWeights()`: Set `Container: 2.0` (matching `Truck: 2.0`) with clear documentation that container vehicles are folded into the heavy freight vehicle weight.
   - In `Calculate()`: Fold `counts.Container` into `counts.Truck` or calculate as:
     ```go
     float64(counts.Truck)*c.weights.Truck + float64(counts.Container)*c.weights.Truck
     ```
     This guarantees that any container detection from future models or fixtures contributes to PCU as a heavy vehicle, and that `c.weights.Container` is never a divergent dead code path.
   - Retain `Container` in `VehicleCounts` and `PCUWeights` structs for JSON schema backward compatibility with existing API contracts.

2. **Configuration (`services/go-server/internal/config/config.go`)**:
   - Update `PCUWeightContainer` default to `2.0` (aligned with `PCUWeightTruck`), with documentation explaining the consolidation.

3. **Inference Service (`services/cv-engine/server.py`)**:
   - Clean up comments in `server.py` to document that standard COCO 4-class output (`motorcycle`, `car`, `bus`, `truck`) is authoritative for traffic density, with heavy freight captured under `truck`.

4. **Testing (`services/go-server/internal/cvclient/pcu_test.go`)**:
   - Update unit tests to verify that `Container` counts are weighted with the heavy vehicle weight (`2.0`), confirming that no dead code remains and all tests pass.
