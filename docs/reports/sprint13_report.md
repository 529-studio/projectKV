# Sprint 13 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0  
**Date:** 2026-11-12  
**Commit range:** `610e20e..9418d29` (3 merges, 20 files, +5,534 / -284)

**Primary References & Ground Truth:**
- [`docs/HANDOFF-s12.md`](../HANDOFF-s12.md) and [`docs/HANDOFF-s13.md`](../HANDOFF-s13.md)
- Specification: [`docs/sprints/s13_t1_spec.md`](../sprints/s13_t1_spec.md)
- Engineering Lesson: [`docs/lessons/438-moore-boundary-tracing-jacobs-criterion.md`](../lessons/438-moore-boundary-tracing-jacobs-criterion.md)
- GitHub PR ledger: #438 (Issue), #439 (Catalog Resync), #440 (PR / Close #438)

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Wave-by-Wave Audit of Delivered Engineering](#2-wave-by-wave-audit-of-delivered-engineering)
3. [Reality-Check Audit Findings (The Uncomfortable Truths F1-F5)](#3-reality-check-audit-findings-the-uncomfortable-truths-f1-f5)
4. [End-User Utility Audit (Saigon Commuter Reality)](#4-end-user-utility-audit-saigon-commuter-reality)
5. [Quantitative Verification Evidence & Test Ledger](#5-quantitative-verification-evidence--test-ledger)
6. [Engineering Lessons Learned (L96 / Issue #438)](#6-engineering-lessons-learned-l96--issue-438)
7. [Carry-Over Ledger & Strategic Bridge to Sprint 14](#7-carry-over-ledger--strategic-bridge-to-sprint-14)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 13 focused on solving the fundamental data materialization gap identified at the end of
Sprint 12, creating the off-box GPU retraining harness, and conducting independent audits across
the frontend, backend, and machine learning pipelines.

### Delivered capability in Sprint 13 (S13-T1 / Issue #438, PR #440)

1. **Moore Boundary Tracing Root-Cause Resolution**: Diagnosed premature 3-step contour termination
   collapsing vehicle masks into 2-vertex degenerate polylines. Implemented pure NumPy state-based
   Jacob's stopping criterion `(pixel, backtrack)` and widened run-merging to `[s-1, e+1]` for true
   8-connectivity. Restored proposal extraction yield from 370 to **6,386 polygons across 744 live frames**
   (0 empty payloads; headroom +194 over the 550 threshold).
2. **Off-Box GPU Retraining Pipeline**: Built reproducible Google Colab / cloud GPU runner
   (`colab_train_yolov8_seg.ipynb` + `colab_pipeline.py`) enforcing $\ge 50$ epochs, mask-pixel IoU
   calculation, run manifest hash binding, and OpenVINO FP16 export.
3. **Containerized Linux x86-64 AVX2 Latency Soak Harness**: Delivered pinned `deploy/benchmark/Dockerfile.avx2`
   and `tools/train/bench_avx2_docker.sh` with host refusal guards against non-AVX2 hosts and anti-spoofing
   timestamp validation.

### Sprint Scorecard

| Dimension | Sprint 12 State | Sprint 13 State | Verdict |
|:---|:---|:---|:---|
| Segmentation candidate yield | 370 polygons (40% empty) | **6,386 polygons (0% empty)** | **PASS** |
| GPU training automation | None (manual instructions) | Complete Colab runner + test suite | **PASS** |
| AVX2 benchmark harness | Host-gated CLI only | Pinned container + Docker runner | **PASS** |
| Automated test suite pass | 445 tests | **525 tests** (+80 new tests) | **PASS** |
| Live data plane continuity | 3.5% history coverage | 3.5% history coverage | **DEFICIT (F1)** |
| SQLite disaster recovery | Zero backups / restore drill | Single unbacked volume | **DEFICIT (F3)** |

---

## 2. Wave-by-Wave Audit of Delivered Engineering

### Wave 1: Pure-NumPy Boundary Tracing Fix (`tools/seg/`)

The failure of SAM vehicle masks to produce polygons was traced to `tools/seg/image_geometry.py:183`.
The tracer stopped on `cur == start and len(path) > 2`. When a mask has a 1-pixel protrusion tip or
clipped corner, Moore neighbor tracing re-visits the start cell after only 3 steps. This collapsed the
contour to 4 points, which RDP simplified to 2 vertices, causing `build_candidate` to drop the entire
annotation.

Implementing Jacob's stopping criterion—terminating only when revisiting `(pixel, backtrack)`—and expanding
`largest_component` run-merge bounds to `[s-1, e+1]` resolved the defect. Contract tests in
`image_geometry_test.py` expanded from 17 to 23 tests, with zero external dependencies added.

### Wave 2: Automated Off-Box GPU Retraining Pipeline (`tools/train/`)

To fulfill AC10–AC16 without violating AGENTS.md Rule 10 (no fake weights or unmeasured constants):
- `tools/train/colab_pipeline.py` provides an 8-stage fail-closed orchestration engine.
- Derives true **mask-pixel IoU** from Ultralytics batch masks (never the bounding box confusion matrix).
- `tools/train/colab_train_yolov8_seg.ipynb` provides an 18-cell self-contained Colab notebook.
- Added 36 contract tests in `colab_pipeline_test.py` covering validation and packaging.

### Wave 3: Pinned Containerized AVX2 Latency Benchmark (`deploy/benchmark/`)

To fulfill AC17–AC19 across heterogeneous development machines:
- Built `deploy/benchmark/Dockerfile.avx2` pinned with OpenVINO 2026.4.1, Python 3.12.7, and NumPy 2.1.3.
- `bench_avx2_docker.sh` executes the 3 x 200 runs with non-future timestamp enforcement.
- Proves Darwin arm64 refusal empirically (exit code 2). Added 38 contract tests in `bench_avx2_docker_test.py`.

---

## 3. Reality-Check Audit Findings (The Uncomfortable Truths F1-F5)

Two independent subagent audits (`audit-web` and `audit-backend` via OpenCode DeepSeek v4.1) evaluated
the live application. Their findings represent the primary input for Sprint 14 planning:

### F1: The Live Data Plane is Dark
Despite passing unit tests, the production Go server data plane is severely starved:
- Out of 837 registered cameras, only 29 (3.5%) have continuous 24-hour observation history.
- Live flood status is currently **100% UNKNOWN** in production because stalled camera polling loops
  drop feeds before inference completes.
- Feeds that refresh slowly (60–90 s) are tagged stale at the 30 s polling window and rejected from history.

### F2: Hazard Depth Verdicts are Invisible Where Riders Actually Look
- Physical cm flood depth (#401) is rendered exclusively inside `CameraModal`.
- Map markers, tooltips, `PinnedDock` tiles, and `MobileCameraDrawer` display zero depth information.
- 657 markers rendered without clustering at zoom 11–12 collapse into an unreadable cloud.
- Pale OSM raster basemap has poor sunlight contrast against dark UI under outdoor Saigon glare.

### F3: SQLite Has Zero Automated Backup or Verified Restore Drill
- The entire system state (`camera-view.db`, subscriptions, traffic history) sits on a single Docker volume.
- Litestream replication is not deployed, and no restore drill has ever been executed.
- Unprivileged sidecars (`10001:10001`) fail to write state directories on fresh root-owned volumes.

### F4: Weather Feature Store Ingestion Has Zero Serving Parity
- `tools/forecast/train_live_model.py` and the weather sampler log rainfall, temperature, and wind to SQLite.
- Online serving models in `services/go-server/internal/forecast/model.go:236` completely omit weather inputs.
- Backtest gate allows models with negligible skill (0.05% win on a single horizon) to pass.

### F5: Off-Box Compute Remains Blocked on Execution Hosts
- The training notebook and AVX2 Docker harness are complete, tested, and pushed.
- However, the actual CUDA GPU run (generating `best.pt` and `val_metrics.json`) and Linux AVX2 latency soak
  require execution on external GPU and AVX2 host environments.

---

## 4. End-User Utility Audit (Saigon Commuter Reality)

Evaluating the product through the eyes of a Saigon motorbike commuter riding in heavy rain:

1. **3-Second Glanceability at 30 km/h**: Currently unachievable. The user must dismiss a marketing
   splash modal, zoom into dense unclustered dots, tap a small marker, and open a dialog to see if a road
   is flooded.
2. **Offline Resilience**: The app serves 1.35 MB uncompressed JS with zero PWA service worker; if a rider
   loses 4G in an underpass or storm, the screen goes blank.
3. **Stale Data Honesty**: When the SSE connection drops, stale traffic colors remain on the map indefinitely
   without visual indication of disconnection.

Sprint 14 must make the safety decision ubiquitous, high-contrast, and clustered.

---

## 5. Quantitative Verification Evidence & Test Ledger

All automated test suites, linting gates, and evidence validators pass 100%:

| Test Suite / Gate | Test Count / Scope | Execution Time | Result |
|:---|:---|:---:|:---|
| `make test-seg` | 83 unit & contract tests | 0.53s | **PASS** |
| `make test-train` | 137 unit & contract tests | 0.23s | **PASS** |
| `make test-eval` | 220 unit & contract tests | 1.73s | **PASS** |
| `make test-ci` | 58 unit & contract tests | 0.99s | **PASS** |
| `make test-docs` | 27 unit & contract tests | 1.33s | **PASS** |
| **Total Automated Tests** | **525 tests** | **4.81s** | **100% PASS** |
| `make docs-lint` | Context budget & prose checks | 0.12s | **PASS** (20 baseline, 0 new) |
| `make evidence-lint` | 77 release artifacts evaluated | 0.25s | **PASS** (Zero anti-spoofing flags) |
| `make lint-deploy` | K8s & Compose manifest lint | 0.08s | **PASS** |
| `check_readme_vi_parity.py` | 6/6 bilingual parity rules | 0.05s | **PASS** |

---

## 6. Engineering Lessons Learned (L96 / Issue #438)

- **Lesson 438 (Moore Boundary Tracing & Jacob's Stopping Criterion)**:
  Contour tracing must track the state `(pixel, backtrack)`, not merely the visit of the start pixel.
  Single-pixel antenna protrusions and diagonally clipped corners cause premature loop termination in 3 steps
  when tracking pixel identity alone. Documented in [`docs/lessons/438-moore-boundary-tracing-jacobs-criterion.md`](../lessons/438-moore-boundary-tracing-jacobs-criterion.md).

---

## 7. Carry-Over Ledger & Strategic Bridge to Sprint 14

Sprint 13 resolved the polygon extraction defect and delivered complete off-box harnesses. Sprint 14
shifts focus directly to fixing data plane darkness, disaster recovery, and commuter usability.

| ID | Issue | Target Subsystem | Priority Objective |
|:---|:---|:---|:---|
| **S14-T1** | **#441** | `services/go-server`, `cvc` | **Fix Live Inference Pipeline Darkness**: Unstick CV polling loop, segregate dead feeds, achieve $\ge 95\%$ 24h history continuity on Hot Set. |
| **S14-T2** | **#442** | `deploy/litestream`, `go-server` | **SQLite HA Replication & Restore Drill**: Stream WAL to Cloudflare R2; fix non-root sidecar permissions; implement `make restore-drill` (RTO $\le 60$s, RPO $\le 10$s). |
| **S14-T3** | **#443** | `apps/web` | **Commuter Map Usability & Visible Hazard Depth**: High-contrast dark basemap, Leaflet marker clustering (< zoom 14), ubiquitous cm depth badges, and $\ge 48$px touch targets. |
| **S14-T4** | **#444** | `tools/forecast`, `cv-engine` | **Forecast Feature Parity & Off-box Model Promotion**: Ingest weather into serving models; require $\ge 5\%$ MAE improvement over persistence; execute Colab GPU retraining run. |

### Sprint 13 Close Directive
Per AGENTS.md Rule 12, Sprint 13 is officially closed with the recording of this report and [`docs/HANDOFF-s13.md`](../HANDOFF-s13.md). A fresh conversation bootstrapped from the handoff will execute Sprint 14.
