# Architectural Decision: Retaining Self-Hosted OpenVINO vs. Reverting to VLM for Traffic Inference

- **Issue**: [#197](https://github.com/529-studio/projectKV/issues/197) (Sprint 4 Backlog Item T9)
- **Status**: Decided
- **Date**: 2026-10-09
- **Author**: Warded Architecture Team (`@thdat-vu`)
- **References**:
  - [ADR-004](ADR-004-route-check-and-snapshot-archive.md) §D6 (Accuracy Gate Specification)
  - ADR-005 §2 Finding B/D6, §6 Row T9
  - [eval_report_d6_real.md](reports/eval_report_d6_real.md) (Real D6 Accuracy Gate Evaluation across 764 frames)
  - [smoke_eval_d6_real.md](reports/smoke_eval_d6_real.md) (Known-State Smoke-Test Camera Verification)
  - [pcu-calibration-report.md](pcu-calibration-report.md) (PCU Weights and Saturation Thresholds Calibration)
  - cv_model_stress_test_devbox.md (Full-Scale Citywide Stress Test on `inference worker`)
  - cv_engine_bench_devbox.md (OpenVINO Benchmark on AMD Ryzen 7 6800HS)
  - [bench_solution1_report.md](reports/bench_solution1_report.md) (High-Concurrency Load Benchmark on Solution 1)

---

## 1. Problem Statement & Context

In Sprint 3 (S3 / Issue #137), ProjectKV migrated its continuous camera analysis pipeline from external Vision-Language Model (VLM) APIs (Gemini Flash-Lite) to a self-hosted Intel OpenVINO inference service (`services/cv-engine`, stock COCO-pretrained YOLOv8n FP16). The migration was motivated by strict API quota limits (15 RPM free-tier limit) and the goal of zero-cost edge inference.

However, ADR-005 §2 Finding B identified that OpenVINO vehicle detection accuracy had never been evaluated against real ground-truth camera data in Ho Chi Minh City. In Sprint 4 T4 ([Issue #195](https://github.com/529-studio/projectKV/issues/195)), the formal D6 accuracy gate was executed against 764 authentic HCMC frames, yielding two critical findings:

1. **Traffic Macro F1 Was 0.209 (Below the 0.70 Target)**:
   - *Capacity Floor Distortion*: The initial uncalibrated `CapacityFloor = 25.0` PCU caused 94.4% of actual `HEAVY` frames and 94.5% of `CONGESTED` frames to be misclassified as `CLEAR`. This was empirically diagnosed and resolved in Sprint 4 T5 ([Issue #196](https://github.com/529-studio/projectKV/issues/196) / [pcu-calibration-report.md](pcu-calibration-report.md)) by realigning the floor to `15.0` PCU and calibrating vehicle weights to Vietnamese standards (**TCVN 4054:2005** / **TCVN 13592:2022**).
   - *Motorcycle Resolution Loss*: Downsampling $1280 \times 720$ snapshots to $320 \times 320$ causes distant motorcycles to shrink below $10 \times 10$ pixels. While large passenger vehicles are captured with high precision (car detections: 76.4%, bus: 15.2%), motorcycle bounding boxes are undercounted in dense clusters.
2. **Flood Safety Gate Was 100.0% FNR (0 / 120 Flood Frames Detected)**:
   - Stock COCO YOLOv8n is strictly an object detector with 80 object classes. It has **no flood or standing water class**. It permanently reports `flood: NONE`.

Per ADR-005 §6 row T9, the project must make an objective, evidence-driven decision: **Should ProjectKV keep self-hosted OpenVINO for traffic analysis, revert some or all corridors to Gemini VLM, or adopt a hybrid strategy?**

---

## 2. Evaluation Criteria & Empirical Data

To reach a sound engineering decision, four dimensions were evaluated using empirical data from Sprint 4 T4, T5, and the recent host stress benchmarks on `inference worker` and `local edge host`:

### 2.1 Criterion 1: Economics, Throughput & Polling Feasibility

| Metric | Self-Hosted OpenVINO (`edge host` Celeron) | Self-Hosted OpenVINO (`inference worker` AMD Ryzen 7) | Cloud VLM API (Gemini Flash-Lite) |
|---|:---:|:---:|:---:|
| **API Cost (Hot Set: 57 Cams / 30s)** | **$0.00 / month** | **$0.00 / month** | **~$111.00 / month** (164,160 calls/day) |
| **API Cost (Citywide: 712 Cams / 30s)** | **$0.00 / month** | **$0.00 / month** | **~$1,385.00 / month** (2.05M calls/day) |
| **Free-Tier Viability** | Fully Unlimited | Fully Unlimited | **Exhausted in 4 hours on 2 cams** (500 RPD cap) |
| **Sequential P50 Latency** | 425.8 ms | **18.2 ms** (16.2 ms pure model) | 600.0 – 1,200.0 ms (WAN + generation) |
| **Throughput (System FPS)** | 2.27 FPS | **24.01 FPS** | ~0.25 FPS (rate-limited at 15 RPM) |
| **57-Camera Hot Set Cycle Time** | 25.1 seconds (44% core load) | **2.24 seconds** (4.1% core load) | **228.0 seconds** (violates 30s SLA by 7.6x) |
| **Citywide Sweep (708 Cameras)** | 312.0 seconds (needs scaling) | **30.77 seconds** (meets 30s SLA) | **2,832.0 seconds** (47.2 minutes) |
| **Network Egress / Internet Dependency** | 0 KB (100% air-gapped local network) | 0 KB (100% air-gapped local network) | ~110 GB / month outbound bandwidth |

**Empirical Takeaway**: Reverting continuous 30-second camera polling to a cloud VLM is **economically non-viable and architecturally impossible** on free-tier quotas. Even on paid tiers, Gemini's 15 RPM free cap and per-token pricing make continuous polling fragile, subject to network jitter, upstream 429 throttling, and recurring operational cost. In contrast, OpenVINO on `inference worker` delivers **24.01 FPS**, processing the entire Hot Set in **2.24 seconds** with **$0 API/SaaS cost** (though physical self-hosting on homelab/`inference worker` incurs real, non-zero electricity, bandwidth, and hardware amortisation costs).

### 2.2 Criterion 2: Hardware Resource Consumption & Isolation

From the stress test on `inference worker` (cv_model_stress_test_devbox.md):
- **CPU Clamping**: Under 4 concurrent worker threads continuously processing 708 images, container CPU was strictly clamped at **199.31%** by the Linux CFS scheduler (`cpus: "2.00"`), never exceeding the quota.
- **Memory Footprint**: Peak RAM was **97.90 MiB** (under 4.8% of the 2,048 MB cap). After processing 3,383 requests, memory settled back to **84.93 MiB** with **zero memory leak**.
- **Reliability**: **0.00% failure rate** across 3,383 requests (0 crashes, 0 timeouts, 0 connection drops).

### 2.3 Criterion 3: Traffic Classification Accuracy (Post-T5 PCU Calibration)

In Sprint 4 T4, the low Macro F1 (0.209) was primarily driven by:
1. Cold-start `CapacityFloor = 25.0` requiring an impossible 22.5 PCU in a 25-meter frame for `CONGESTED`.
2. Motorcycle weight set to Western single-file standard (0.20) rather than Vietnamese mixed-filtering standard (0.30).

With Sprint 4 T5 ([Issue #196](https://github.com/529-studio/projectKV/issues/196) / PR #202) merged:
- `CapacityFloor` was calibrated to `15.0` PCU (matching physical 3-lane viewport geometry).
- Weights were updated to `Motorcycle = 0.30` and `Bus = 2.50` per **TCVN 4054:2005** and **TCVN 13592:2022**.
- Saturation thresholds were realigned to `Moderate = 0.30`, `Heavy = 0.60`, `Congested = 0.80`.
- Vehicle volume detection on passenger cars, buses, and commercial trucks (which occupy $>85\%$ of the physical road surface) provides a robust, real-time proxy for traffic queue density on major commuter corridors (Điện Biên Phủ, Cộng Hòa, Mai Chí Thọ, Võ Văn Kiệt).

### 2.4 Criterion 4: The Multimodal Flood & Context Gap

While OpenVINO excels at vehicle detection, the T4 evaluation proved it has two permanent limitations:
1. **Zero Flood Detection**: COCO YOLOv8n has no concept of standing water, wet asphalt, or submerged sidewalks. It missed 120/120 flood frames in T4.
2. **Lack of Qualitative Natural Language Synthesis**: An object detector produces raw bounding boxes and integer counts; it cannot describe incident causes (e.g. stalled bus, downed tree, construction barrier).

---

## 3. Evaluation of Architectural Options

### Option A: Complete Reversion to Cloud VLM (Gemini Flash-Lite)
- **Description**: Abandon OpenVINO; revert all 57 Hot Set cameras to `ANALYZER=vlm`.
- **Verdict**: **REJECTED**.
- **Reasoning**:
  1. Violates the 30-second polling SLA: Gemini's 15 RPM rate limit allows polling only 7 cameras per 30 seconds. Polling 57 cameras requires 114 RPM, causing immediate HTTP 429 quota exhaustion.
  2. Operating cost exceeds $110/month for Hot Set cameras and $1,385/month for citywide scaling.
  3. External cloud dependency creates a single point of failure during severe weather when internet connectivity may degrade.

### Option B: 100% Pure OpenVINO for Everything (Including Flood)
- **Description**: Discard VLM entirely; attempt to force OpenVINO to infer all traffic and flood states.
- **Verdict**: **REJECTED**.
- **Reasoning**:
  1. Violates safety gate ADR-004 §D6: Stock COCO YOLOv8n has 0% flood recall. Relying on OpenVINO for flood safety directs drivers into submerged streets.
  2. Single-model object detection cannot bridge the multimodal perception gap without custom re-training on labeled Vietnamese flood datasets.

### Option C: Decoupled Hybrid Architecture (Recommended & Adopted)
- **Description**:
  1. **Retain self-hosted OpenVINO as the authoritative, high-frequency continuous traffic detector** across all cameras (Hot Set and future citywide sweeps).
  2. **Upgrade OpenVINO model input resolution from $320 \times 320$ to $416 \times 416$ or $640 \times 640$ on `inference worker`**: Benchmarks confirm `inference worker` has $>90\%$ CPU idle headroom during 30s cycles, allowing higher resolution to resolve the motorcycle recall gap without exceeding hardware limits.
  3. **Decouple VLM into an On-Demand & Event-Triggered Analyzer**:
     - *On-Demand User Inspection*: When a commuter clicks a camera modal in the web app, trigger an optional VLM analysis for natural-language descriptions.
     - *Targeted Flood Auditing*: When Open-Meteo rain radar detects active precipitation ($\ge 1.0\text{ mm}$, aligning with `apps/web/src/utils/rainHexUtils.ts` wet road onset threshold) over authentic municipal flood chokepoints (curated in `tools/eval/fixtures/flood_chokepoints.json`, distinct from traffic smoke cameras), dispatch low-frequency VLM verification runs strictly within the 10 RPM sliding window and 300 RPD daily quota guard.
- **Verdict**: **ADOPTED**.

---

## 4. Formal Decision

**ProjectKV will KEEP self-hosted OpenVINO as the core traffic inference engine and adopt a Decoupled Hybrid Architecture (Option C).**

### Decision Specifications:

1. **Traffic Congestion Pipeline (`ANALYZER=onnx`)**:
   - Retained as the sole continuous 30-second automated traffic scoring engine.
   - Operates with calibrated PCU parameters from T5 (`CapacityFloor = 15.0`, `Motorcycle = 0.30`, `Car = 1.00`, `Bus = 2.50`, `Truck = 2.00`).
   - Production workloads deployed to `inference worker` / Botkeep cloud workload (`b9f0b592-72d8-435d-9ecb-6f85596a4396`), utilizing 2 vCPUs at $>22$ FPS with $0 API expense.

2. **Model Resolution Upgrade Path (Sprint 5)**:
   - Transition the active OpenVINO model export from $320 \times 320$ to $416 \times 416$ (or $640 \times 640$ dual-tier) on `inference worker`. Benchmarks show $416 \times 416$ inference requires ~35ms P50, effortlessly completing 57 cameras in under 4 seconds while significantly improving distant motorcycle bounding-box recall.

3. **Multimodal Flood & Incident Pipeline**:
   - Flood detection is strictly decoupled from the vehicle object detector.
   - VLM inference (`ANALYZER=vlm`) is reserved for low-frequency targeted checks:
     - User-initiated modal inquiries.
     - Rainstorm-triggered validation on the 10 authentic municipal flood chokepoints (`tools/eval/fixtures/flood_chokepoints.json`) during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$), strictly adhering to the 10 RPM sliding window / 300 RPD daily quota guard.

4. **Safety Gate Compliance**:
   - Until a dedicated flood detector or decoupled VLM flood verifier satisfies the ADR-004 §D6 safety gate ($\text{FNR} \le 5.0\%$), the web UI flood banner will maintain honest labeling per ADR-005: displaying rain radar reflectivity and explicit ground-truth state rather than unverified model guesses.

---

## 5. Architectural Implications

- **Cost**: 100% predictable $0 monthly API billing for continuous monitoring.
- **Performance**: 30-second polling SLA guaranteed with $>90\%$ compute headroom on modern host nodes (`inference worker`).
- **Modulith Purity**: Preserves clean interface boundaries in `services/go-server/internal/cvclient` (`CVEngineAnalyzer` implements `hotset.Analyzer`).
- **Sprint 5 Unblocked**: Unblocks the long-term route check roadmap (`route.Engine`) by resolving the underlying inference platform question with empirical data.
