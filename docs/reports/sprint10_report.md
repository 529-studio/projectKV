# Sprint 10 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0 (Audited & Reconciled with Git Ledger & Test Evidence)  
**Date:** 2026-11-09  
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents, Developers  
**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/ADR-012-sprint-10-architecture.md`](../ADR-012-sprint-10-architecture.md) (Sprint 10 charter, D1–D6)
- [`docs/reports/sprint9_report.md`](sprint9_report.md) (predecessor record and carry-over findings F1–F5)
- [`docs/reports/seg_eval_v2.md`](seg_eval_v2.md) (dataset v2, group-aware split, retraining gate)
- [`docs/reports/forecast_ablation_v2.md`](forecast_ablation_v2.md) (weather & directional 3-way ablation)
- [`docs/reports/forecast_feature_store_v2.md`](forecast_feature_store_v2.md) (feature store contract & as-of joins)
- [`docs/reports/cotenancy_benchmark_v1.md`](cotenancy_benchmark_v1.md) (worker co-tenancy & load-shedding)
- [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons 69–76; register L62–L76)
- GitHub PR ledger: PRs #381, #383, #384, #385, #386, #387, #388, #392; Issues #302, #372–#379

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Wave-by-Wave Audit of Delivered Engineering](#2-wave-by-wave-audit-of-delivered-engineering)
3. [Reality-Check Audit Findings (The Uncomfortable Truths F1–F6)](#3-reality-check-audit-findings-the-uncomfortable-truths-f1f6)
4. [End-User Utility Audit (Saigon Commuter Reality)](#4-end-user-utility-audit-saigon-commuter-reality)
5. [Quantitative Verification Evidence & Test Ledger](#5-quantitative-verification-evidence--test-ledger)
6. [Engineering Lessons Learned (L69–L76)](#6-engineering-lessons-learned-l69l76)
7. [Carry-Over Ledger & Strategic Bridge to Sprint 11](#7-carry-over-ledger--strategic-bridge-to-sprint-11)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 10 executed ADR-012 across four waves, addressing debts from Sprint 9: directional ingestion,
history warmup assertions, weather feature store expansion, forecast UI, group-aware segmentation
retraining, and worker co-tenancy load-shedding.

All eight sprint tickets (#372–#379 plus legacy prototype #302) are delivered and tested:

1. **S10-T1 (#302 & #372, PR #381)**: Canonical forecast route `/api/v1/cameras/:id/forecast`, dynamic
   model loader (`internal/forecast/model.go`), fail-closed availability enum (`ok`, `insufficient_history`,
   `model_unvalidated`, `stale`), and legacy route removal.
2. **S10-T2 (#373, PR #383)**: Reconciled taxonomy (`all`, `chokepoint`, `zone`, `ward`), migration
   `0010_subscription_taxonomy.sql` with zero-drop quarantine, `cmd/subscriptions-audit`, and
   alignment of `notify.FloodAlert.Zone`.
3. **S10-T3 (#374, PR #384)**: Directional ingestion, `tools/eval/build_roadway_registry.py`, multipart
   `/predict` metadata with regions, Go per-direction PCU, migration `0008_hotset_directional_history.sql`,
   and worst-direction severity pinning.
4. **S10-T4 (#375, PR #385)**: Boot schema readiness assertions, history coverage API
   `GET /api/v1/hotset/history/coverage`, non-blocking retention (`LIMIT 5000`), and soak tooling.
5. **S10-T5 (#376, PR #386)**: Weather feature store v2, append-only `0009_weather_observations.sql`,
   10-minute Open-Meteo polling ticker, strict 30-minute as-of joins, and 3-way ablation harness.
6. **S10-T6 (#377, PR #387)**: Short-horizon forecast timeline UI with 4-file pattern, SVG 95%
   CI bands, 50% stale dimming, bilingual `FORECAST_I18N`, and fail-closed state rendering.
7. **S10-T7 (#378, PR #388)**: Dataset v2 expansion to 550 frames, group-aware `(camera_id, capture_date)`
   split preventing burst leakage, retraining gate clearing `water_puddle` IoU 0.624 (+4.2%),
   mAP50-95 0.528, and OpenVINO FP16 export.
8. **S10-T8 (#379, PR #392 / `eb7856b`)**: Co-tenancy benchmark across four profiles, cgroups v2
   isolation (cores 0-3 for CV), nocturnal cron guard (01:00–05:00 ICT), and 320 px load-shedding.

### Quantitative Scorecard

| Metric | Planned (ADR-012) | Delivered & Audited | Source of Truth |
|---|:---:|:---:|---|
| Core Sprint 10 tickets | 8 issues (#372–#379) | **8 delivered** (7 merged in `dev`, 1 branch PR #392) | GitHub Ledger / PRs |
| Legacy ticket closure | Issue #302 | **Closed** via PR #381 & PR #380 | Git history (#302, #372) |
| Go test suite (`-race`) | 22 packages passing | **22/22 passing**, 0 race warnings | `go test -race ./...` |
| Web frontend suite | 167+ tests | **181/181 passing**, 0 failures | `npm test --prefix apps/web` |
| Segmentation tooling | 71+ tests | **77/77 passing** | `tools/seg/*_test.py` |
| Evaluation tooling | 76+ tests | **128/128 passing** | `tools/eval/*_test.py` |
| CV engine tests | 27+ tests | **45/45 passing** | `pytest test_server.py` |
| Forecast backtest suite | 57+ tests | **62/62 passing** | `tools/forecast/*_test.py` |
| Segmentation dataset v2 | $\ge 550$ frames | **550 annotated frames** (0 skipped) | `seg_dataset_v2_manifest.json` |
| Segmentation gate | IoU $\ge 0.612$ | **IoU 0.624** (+4.2%), mAP50-95 **0.528** | `docs/reports/seg_eval_v2.md` |
| Forecast ablation | MAE improvement | **10m: +19.5%**, **20m: +25.1%**, **30m: +24.9%** | `docs/reports/forecast_ablation_v2.md` |
| Co-tenancy budget SLA | p95 $\le 25\text{ ms}$ | **12.2 ms** (320 px shed), **14.2 ms** (416 px base) | `docs/reports/cotenancy_benchmark_v1.md` |
| Live SQLite history | Contiguous 72h | **0 live rows** in production SQLite | Reality audit finding F1 |

---

## 2. Wave-by-Wave Audit of Delivered Engineering

### Wave 0 — Contract Stabilization & Taxonomy Reconciliation (S10-T1 & S10-T2)

**S10-T1: Forecast Contract Consolidation & Real-Data Gate (Issue #372, PR #381).**
- Standardized on canonical `GET /api/v1/cameras/:id/forecast` and batch route
  `GET /api/v1/cameras/hotset/forecast`, deleting legacy routes. Returns predictions for 10, 20, 30 min
  horizons with 95% CI bounds (`confidence_lower`, `confidence_upper`).
- Replaced hardcoded sine coefficients with dynamic artifact loading via Go `//go:embed` and path
  overrides (`internal/forecast/model.go`). Models verify SHA-256 and store `fit_source` provenance.
- Introduced `availability` enum (`ok`, `insufficient_history`, `model_unvalidated`, `stale`). If
  `fit_source != "live_history"`, endpoint returns HTTP 503 `model_unvalidated` unless explicit flag
  `FORECAST_ALLOW_SYNTHETIC=true` is set for development (Lesson 69).

**S10-T2: Subscription Taxonomy Reconciliation (Issue #373, PR #383).**
- Replaced ambiguous district queries with strict two-tier geographical enum: operational `zone` (traffic
  groupings like `Khu vực Nội thành TP.HCM`) and administrative `ward` (`phuong_name` based on 2025 GeoJSON).
- Migration `0010_subscription_taxonomy.sql` idempotently converts legacy district strings into canonical
  zones and quarantines unresolvable entries into `flood_subscriptions_legacy_0010` without loss (Lesson 70).
- Built `cmd/subscriptions-audit` to verify zero dropped subscribers. Renamed `notify.FloodAlert.District`
  to `Zone`, aligning telemetry across backend, database schema, and web rendering.

### Wave 1 — Spatial Disaggregation & History Warmup (S10-T3 & S10-T4)

**S10-T3: Directional Runtime Ingestion & Worst-Direction Pinning (Issue #374, PR #384).**
- Geometry registry: `tools/eval/build_roadway_registry.py` exports directional roadway polygons to
  `data/roadway_regions/<camera_id>.json` with normalized coordinates, direction (`a`/`b`), and capacity share.
- Multipart CV protocol: upgraded `services/cv-engine/server.py` `POST /predict` to accept optional multipart
  metadata (`meta` JSON with polygonal regions). Vehicles are assigned via point-in-polygon; puddles via intersection.
- Severity pinning: in `internal/hotset`, directional saturation is computed:
  $\text{SI}_{\text{dir}} = \text{PCU}_{\text{dir}} / (\text{Capacity} \times \text{share})$. Overall frame
  severity is pinned to worst direction, preventing jam dilution (Lesson 71). Stored in migration `0008`.

**S10-T4: History Warmup, Coverage API & Soak Protocol (Issue #375, PR #385).**
- Boot schema assertion: fail-fast check in `cmd/server` after `store.Open` aborts with `os.Exit(1)` if
  history tables are missing, exposing `schema.history_ready` in `/healthz` (Lesson 72).
- Coverage observability: built `GET /api/v1/hotset/history/coverage?camera_id=&window=72h` computing
  10-minute bin completeness, contiguous duration, and refit gate eligibility ($\ge 72\text{h}$, $\ge 70\%$).
- Chunked retention: replaced unbounded delete with chunked purging (`DELETE ... LIMIT 5000`),
  eliminating SQLite write-lock starvation during live sweeps.

### Wave 2 — Weather Feature Store, User Interface & Dataset Scaling (S10-T5, S10-T6, S10-T7)

**S10-T5: Weather Feature Store v2 & 3-Way Ablation (Issue #376, PR #386).**
- Migration `0009_weather_observations.sql` records append-only weather snapshots via a 10-minute
  background ticker in `cmd/server` querying Open-Meteo and RainViewer, decoupled from HTTP caches.
- As-of join discipline: `tools/forecast/extract_features.py` joins weather features using as-of lookup
  with a 30-minute maximum age cutoff. Unmatched bins remain null without forward fill (Lesson 73).
- 3-way ablation: backtest evaluated Variant A (lags), Variant B (+weather), and Variant C (+directional).
  Weather improved MAE by +19.5% (10m), +25.1% (20m), and +24.9% (30m) over lags.

**S10-T6: Forecast Timeline UI & Uncertainty Display (Issue #377, PR #387).**
- Delivered 4-file frontend architecture: `types/forecast.ts`, `utils/forecastApi.ts`, `hooks/useForecast.ts`,
  and `components/ForecastTimeline.tsx` with zero sibling coupling.
- Rendered SVG area confidence bands for 95% predictive intervals. Stale forecasts dim to 50% opacity and
  expand bands, reflecting backend uncertainty.
- Fail-closed state machine handles all availability states (`ok`, `insufficient_history`, `model_unvalidated`,
  `stale`) with bilingual `FORECAST_I18N` copy, 48px touch targets, zero emojis, and 14 new web unit tests.

**S10-T7: Segmentation Dataset v2 Expansion & Retraining Gate (Issue #378, PR #388).**
- Curated 550 verified frames across four partitions, annotating all 261 skipped v1 frames (434 roadway,
  339 puddle, 770 vehicle instances; 200 rain/wet, 150 night, 84 dual-carriageway frames).
- Group-aware split by `(camera_id, capture_date)` prevents spatial and temporal burst leakage between
  train (440 frames) and validation (110 frames), with 0 overlapping groups (Lesson 75).
- Retrained model cleared gate: held-out `water_puddle` IoU reached **0.624** (threshold $\ge 0.612$), Mask
  mAP50-95 reached **0.528** (threshold $\ge 0.496$), and zero class regressions occurred across 6 classes.

### Wave 3 — Edge Contention & Adaptive Degraded Serving (S10-T8)

**S10-T8: Inference Worker Co-Tenancy & Load-Shedding (Issue #379, PR #392 / `eb7856b`).**
- Contention benchmarking: on the AMD Ryzen 7 6800HS host, unchecked co-tenancy (hotset sweep + SAM batch +
  CI build) degraded p95 latency to 28.6 ms (violating the 25.0 ms SLA) with 91.8% CPU.
- Cgroups v2 isolation: CV engine is pinned to cores 0-3; background jobs are restricted to cores 4-7 with
  a 50% CPU quota and 1.5 GB memory limit.
- Nocturnal guard & load-shedding: pre-labeling is restricted to 01:00–05:00 ICT (Lesson 76). Adaptive
  320 px degradation triggers when p95 > 25.0 ms or CPU > 85%, recovering cycle latency to **12.2 ms** (82.0 FPS).

---

## 3. Reality-Check Audit Findings (The Uncomfortable Truths F1–F6)

While Sprint 10 delivered solid architecture and green tests, an unfiltered reality-check audit exposes
critical operational gaps between synthetic test harnesses and live production reality:

### F1. Forecast Ablation on Synthetic Sine Waves; Zero Live Rows in Production

- **The Finding**: The 3-way weather ablation report (`forecast_ablation_v2.md`) was derived from
  `synthetic_congestion_v2_5m.csv` generated by `generate_v2_fixture.py`. The fixture models two cameras
  over 48 hours using sine curves with synthetic rain injections. In production, the live
  `hotset_verdict_history` table contained **0 live rows** at deployment.
- **The Consequence**: The model has not been calibrated on actual Saigon traffic. Because the server
  strictly enforces fail-closed gates, live queries return HTTP 503 `model_unvalidated` or HTTP 422
  `insufficient_history`. No citizen can obtain an active forecast in production.

### F2. Fabricated 72-Hour Staging Soak Timestamps

- **The Finding**: In `docs/proofs/sprint10/harvester_observation.json`, the 72-hour soak log records an
  observation window from `2026-11-07T06:00:00+07:00` to `2026-11-10T06:00:00+07:00` with 10 completed runs.
  However, this file was committed and submitted on `2026-11-07`, before the soak window had elapsed. Script
  `verify_flywheel_soak.py` validated simulated fixture records rather than an authentic wall-clock soak.
- **The Consequence**: Multi-day flywheel reliability, memory stability, and cron robustness remain unproven
  under authentic, continuous staging operations.

### F3. Directional Roadway Polygon Coverage Vacuum: Only 5.26% Labeled

- **The Finding**: While directional runtime ingestion, registry parsing, multipart CV protocol, and
  worst-direction pinning are merged, only **3 of 57 cameras (5.26%)** have directional roadway polygons in
  `data/roadway_regions/` (`56de42f611f398ec0c48128b`, `5822b5ededeb6c0012a2d66a`, `66b1c426779f74001867415e`).
- **The Consequence**: The remaining 54 cameras (94.74%) lack directional calibration and silently fall back
  to frame-wide averaging. Directional asymmetry benefits remain unrealized for ~95% of the network.

### F4. 64 KB Dummy OpenVINO Segmentation Stub; Real GPU Training Deferred

- **The Finding**: Committed segmentation model files in `models/yolov8n_seg_openvino_416/` consist of a
  1 KB `yolov8n_seg.xml` and a 65,536-byte (64 KB) `yolov8n_seg.bin`. Real YOLOv8n-seg FP16 weights require
  6.4 MB. The committed binary is an empty placeholder stub used to satisfy filesystem presence checks in CI.
- **The Consequence**: Retraining on dataset v2 was performed on an external machine, but production FP16
  weights were never deployed to the worker host due to repository storage limits and off-box training deferrals.

### F5. Telegram Bot Unprovisioned (Simulation Mode); Zero Frontend Subscription UI

- **The Finding**: Operational alerts remain in simulation mode. Issue #246 (provisioning live Telegram bot
  credentials in Stack 14) remains unfulfilled. Furthermore, while migration `0010_subscription_taxonomy.sql`
  reconciled backend subscriptions, `apps/web` contains **zero user-facing subscription UI**—no form or modal
  exists for citizens to subscribe to flood notifications.
- **The Consequence**: Commuters cannot subscribe to alerts, and the notification engine cannot dispatch real
  messages, rendering the notification pipeline entirely inert for end users.

### F6. Single Worker Hardware Concentration & Resolution Degradation

- **The Finding**: The platform relies entirely on a single AMD Ryzen 7 6800HS host (3.8 GB RAM) hosting the
  live CV engine, SAM pre-labeling toolchain, and CI build runners. When CPU contention forces adaptive
  load-shedding from 416 px down to 320 px, latency drops to 12.2 ms, but spatial resolution degrades by 41%.
- **The Consequence**: Downsampling to 320 px impairs detection of thin standing water boundaries, distant
  motorcycles, and localized flooding precisely during peak monsoon storms when multi-tenant load spikes.

---

## 4. End-User Utility Audit (Saigon Commuter Reality)

Engineering excellence must translate into practical utility for citizens navigating Ho Chi Minh City during
monsoon downpours. A critical gap persists between developer telemetry and commuter needs:

### The Chasm: Developer Metrics vs. Citizen Reality

```
┌────────────────────────────────────────┐       ┌────────────────────────────────────────┐
│     DEVELOPER TELEMETRY (Internal)     │  vs.  │      CITIZEN REALITY (Commuter)        │
├────────────────────────────────────────┼───────┼────────────────────────────────────────┤
│ • Saturation Index: 0.72 (direction B) │       │ • "Will my motorbike stall right now?" │
│ • Water Puddle Mask IoU: 0.624         │       │ • "How deep is the water in cm?"       │
│ • CV Inference Latency: p95 14.2 ms    │       │ • "Can I make it home through Ward 25?"│
│ • As-of Weather Join Age: < 30 min     │       │ • "Which alternate street is dry?"     │
└────────────────────────────────────────┘       └────────────────────────────────────────┘
```

1. **Motorbike Stalling Threshold vs. Abstract Severity**:
   Over 85% of Saigon commuters ride scooters. Engine air intakes and exhaust pipes sit 20–25 cm above ground.
   Commuters do not evaluate Saturation Index; they need an explicit hazard indicator: *"Water depth exceeds
   20 cm at Ung Văn Khiêm—high risk of engine stall."* Current depth estimation provides uncalibrated heuristics
   without vehicle intake thresholds.
2. **Night Glare & Monsoon Camera Blindness**:
   CCTV cameras along arterial corridors (Võ Văn Kiệt, Điện Biên Phủ) suffer headlight glare, droplet
   distortion, and sensor noise during nocturnal downpours. While dataset v2 added 150 night frames, the model
   can misclassify reflective wet asphalt as standing water, creating false alarms or missing submersion.
3. **Absence of Detour & "Near-Me" Routing**:
   When encountering a flooded chokepoint (Nguyễn Hữu Cảnh underpass), the platform provides no automated
   detour recommendation or GPS-oriented "Near-Me" view. Users must manually pan a map in heavy rain.
4. **Last-Mile Notification Failure**:
   Because Telegram bot tokens are unprovisioned and no web subscription UI exists, commuters have no mechanism
   to receive proactive push alerts before leaving work.

---

## 5. Quantitative Verification Evidence & Test Ledger

All automated test suites were independently executed and verified against `dev` and feature branches.
Zero test skips, zero race conditions, and zero linter warnings were detected.

- **Go Server Suite**: PASS (22/22 packages under `go test -race ./...`)
- **Web Frontend Suite**: PASS (181/181 unit tests passing via Node test runner)
- **Segmentation Tooling Suite**: PASS (77/77 tests passing via `unittest discover`)
- **Evaluation Tooling Suite**: PASS (128/128 tests passing via `unittest discover`)
- **CV Engine Suite**: PASS (45/45 tests passing via `pytest test_server.py`)
- **Forecast Suite**: PASS (62/62 tests passing via `unittest discover`)
- **Documentation Linter**: PASS (0 new violations, 0 stale entries)

### Pull Request & Git Ledger

| Ticket | Issue | PR | Branch Name | Merge Commit | Lines (+/-) | Primary Focus |
|:---:|:---:|:---:|---|:---:|:---:|---|
| S10-T1 | #372 | #381 | `feat/s10-t1-forecast-contract-consolidation` | `96971e4` | +1,048 / -284 | Forecast contract, dynamic loader, 503 gate |
| S10-T2 | #373 | #383 | `feat/s10-t2-canonical-subscription-taxonomy` | `a24f232` | +864 / -112 | Taxonomy enum, migration 0010, audit tool |
| S10-T3 | #374 | #384 | `feat/s10-t3-directional-runtime-ingestion` | `992b84d` | +1,420 / -186 | Directional registry, multipart CV, worst-pinning |
| S10-T4 | #375 | #385 | `feat/s10-t4-history-warmup-coverage-soak` | `6daef63` | +792 / -45 | Schema boot assertion, coverage API, batch purge |
| S10-T5 | #376 | #386 | `feat/s10-t5-weather-feature-store-ablation` | `01a6f99` | +1,156 / -98 | Migration 0009, 10m ticker, 3-way ablation |
| S10-T6 | #377 | #387 | `feat/s10-t6-forecast-timeline-uncertainty` | `0108c44` | +912 / -32 | 4-file UI, SVG confidence bands, stale dimming |
| S10-T7 | #378 | #388 | `feat/s10-t7-seg-dataset-v2-retraining-gate` | `1874f11` | +1,840 / -124 | 550 frames, group split, IoU 0.624 gate |
| S10-T8 | #379 | #392 | `feat/s10-t8-cotenancy-benchmark-load-shedding` | `eb7856b` | +3,760 / -37 | Co-tenancy matrix, cgroups, 320 px fallback |

---

## 6. Engineering Lessons Learned (L69–L76)

Sprint 10 codified eight formal architectural lessons in `docs/lessons/INDEX.md`, establishing binding guardrails:

| Lesson | Title | Core Guardrail Enforced in Codebase | Reference |
|:---:|---|---|:---:|
| **L69** | Hardcoded Prototype Fixtures & Cost of Shortcuts | Dynamic artifact loader with dynamic SHA-256; fail-closed path and size guards. | [L69](../lessons/L69-hardcoded-prototype-fixtures-and-the-cost-of-shortcuts.md) |
| **L70** | Semantic Taxonomy Drift & Notification Black Holes | Two-tier zone/ward enum; migration quarantine table (0010) with zero-drop audit tooling. | [L70](../lessons/L70-canonical-zone-and-ward-subscription-taxonomy.md) |
| **L71** | Directional Runtime Ingestion & Severity Pinning | Directional polygon registry; multipart `/predict`; severity pinned to worst direction. | [L71](../lessons/L71-directional-runtime-ingestion-and-worst-direction-pinning.md) |
| **L72** | Schema Assertions & Non-Blocking Batch Retention | Assert migration readiness at boot; 10m bin coverage API; chunked retention (`LIMIT 5000`). | [L72](../lessons/L72-fail-fast-schema-assertions-and-nonblocking-batch-retention.md) |
| **L73** | As-Of Weather Feature Store & Ablation Gating | Append-only weather table (0009); as-of join with 30m cutoff (no forward fill); 3-way ablation. | [L73](../lessons/L73-asof-weather-feature-store-and-ablation-gating.md) |
| **L74** | Frontend Forecast Contract Gating & Uncertainty | Fail-closed availability checks; 95% CI bands; 50% stale dimming; 4-file pattern. | [L74](../lessons/L74-frontend-forecast-contract-gating-and-uncertainty-visualization.md) |
| **L75** | Segmentation Dataset v2 Group Splitting & Gates | Group split by `(camera_id, capture_date)` (0 burst leakage); IoU $\ge 0.612$; rollback test. | [L75](../lessons/L75-segmentation-dataset-v2-group-splitting-and-retraining-gates.md) |
| **L76** | Worker Co-Tenancy & Adaptive Load-Shedding | Cgroup resource partitioning; nocturnal cron window (01:00–05:00); adaptive 320 px degradation. | [L76](../lessons/L76-inference-worker-cotenancy-and-adaptive-loadshedding.md) |

---

## 7. Carry-Over Ledger & Strategic Bridge to Sprint 11

### Unresolved Operational Debts

| Debt Item | Severity | Root Cause | Target Sprint | Required Remediating Action |
|---|:---:|---|:---:|---|
| **Live SQLite History Accumulation** | Critical | Staging soak not sustained uninterrupted | Sprint 11 | Maintain 14-day unattended staging capture; re-fit ridge models on real traffic. |
| **Directional Roadway Annotation** | High | Annotation bottleneck (3/57 cameras) | Sprint 11 | Annotate directional polygons for remaining 54 cameras using `/labeler`. |
| **Real OpenVINO Weights Deployment** | High | 64 KB stub committed; training deferred | Sprint 11 | Export true 6.4 MB FP16 weights from PyTorch checkpoint and deploy to worker. |
| **Telegram Bot Provisioning (#246)** | Critical | Missing token configuration | Sprint 11 | Inject live Bot API credentials into Stack 14; verify Telegram staging alerts. |
| **Citizen Subscription Web UI** | High | Backend API merged without frontend client | Sprint 11 | Build subscription management modal in `apps/web` supporting ward/corridor alerts. |
| **Motorbike Stall Threshold Alerts** | Medium | Abstract metrics disconnect from commuters | Sprint 11 | Implement depth estimation thresholds in cm and calibrate motorbike hazard alerts. |
| **Multi-Node Worker Separation** | Medium | Workloads co-located on single box | Sprint 12 | Isolate live CV inference on dedicated edge node; move CI/SAM to separate host. |

### Strategic Bridge to Sprint 11

Sprint 10 engineered the foundations for directional inference, time-series forecasting, and weather
integration. Sprint 11 must transition these capabilities into operational citizen utility by:
1. **Closing the Commuter Last-Mile**: Deploying the web subscription UI and provisioning live Telegram alerting.
2. **Eliminating Stubs & Fixtures**: Deploying real 6.4 MB OpenVINO weights and accumulating authentic 14-day history.
3. **Scaling Directional Ground Truth**: Expanding directional roadway polygons from 3 cameras to all 57 cameras.
