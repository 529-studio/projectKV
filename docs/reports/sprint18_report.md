# Sprint 18 Completion, Technical Verification & Reality-Check Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0  
**Date:** 2026-11-24  
**Commit Range:** `2457666..9e0150a` (8 feature PRs, 6 maintenance PRs, 115 files, +13,651 / -147 lines)  

**Primary References & Ground Truth:**
- [`docs/HANDOFF-s17.md`](../HANDOFF-s17.md) and [`docs/HANDOFF-s18.md`](../HANDOFF-s18.md)
- Engineering Lessons: [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons 120–129 / Issues #523, #525, #526, #527, #528, #529, #530, #531, #533, #535)
- GitHub PR Ledger: PRs #524, #534, #536, #537, #538, #539, #540, #541, #542, #543, #544, #545, #546, #547

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Delivered Capabilities & Operational Hardening](#2-delivered-capabilities--operational-hardening)
3. [Infrastructure Resiliency & Maintenance Hotfixes](#3-infrastructure-resiliency--maintenance-hotfixes)
4. [Empirical Verification Evidence & Production Proofs](#4-empirical-verification-evidence--production-proofs)
5. [Engineering Lessons Learned (L120–L129)](#5-engineering-lessons-learned-l120l129)
6. [Settled Architecture Decisions & Horizon for Sprint 19](#6-settled-architecture-decisions--horizon-for-sprint-19)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 18 delivered the **Operational Resilience, Edge Optimization & Automated Intelligence** milestone for ProjectKV. Building directly on Sprint 17's commuter-centric features, Sprint 18 focused on turning Warded into an autonomous, self-healing, and production-hardened platform.

Key achievements in Sprint 18:
1. **Turn-by-Turn Hazard Intersections Along Corridors** (Issue #523): Sub-kilometer milestone hazard projections along route polylines alerting drivers to oncoming inundation adapted to their vehicle clearance.
2. **Pure Go VAPID Web Push Notifications** (Issue #525): Native RFC 8291 / RFC 8292 Web Push encryption and subscription management in pure Go stdlib with zero CGO or third-party crypto packages, backed by strict SSRF gateway allowlists.
3. **Corridor Flood Depth Trends & Tidal Correlation** (Issue #526): Multi-hour historical depth progression charts correlated against harmonic river tides, providing predictive context before flood peaks occur.
4. **Automated Crowd-Sourced Hazard Corroboration** (Issue #527): 300m spatial clustering and Bayesian multi-sensor confidence fusion linking citizen reports directly to nearby CCTV perception frames.
5. **INT8 Post-Training Static Quantization (PTQ)** (Issue #528): 4x reduction in ONNX model artifact footprint (42 MB down to 10.6 MB) and 2.1x inference acceleration while preserving FP32 detection heads and zero mAP degradation.
6. **Prometheus / OpenMetrics Telemetry Exporter** (Issue #529): Dedicated `/metrics` endpoint with zero-cyclic leaf collector architecture, CIDR subnet protection, and operational instrumentation for queues, SLAs, and saturation.
7. **Production CORS Gating & Trusted Proxies** (Issue #530): Complete elimination of SEC-04 (permissive CORS origins) and SEC-05 (untrusted proxy defaults) with fail-fast validation and client IP spoofing protection.
8. **Automated Feed Health Audit & Corridor Floor Culling** (Issue #531): Automated 14-day persistent dead camera culling protected by a strict arterial corridor coverage floor (>= 50%), preserving visibility on critical arteries.

### Quantitative Sprint Scorecard

| Metric / Dimension | Baseline (Sprint 17 End) | Delivered (Sprint 18 End) | Status |
| :--- | :--- | :--- | :--- |
| **Edge CV Model Footprint** | 42.1 MB (FP32 ONNX) | 10.6 MB (INT8 Quantized PTQ) | **4x Reduction** |
| **Inference Latency** | 68 ms / frame | 32 ms / frame | **2.1x Faster** |
| **Push Notification Stack** | External / Unimplemented | Pure Go stdlib RFC 8291/8292 | **Delivered** |
| **Crowd Report Trust** | Raw, unverified submissions | Monotonic Bayesian sensor fusion | **Delivered** |
| **Production CORS Gate** | Development fallbacks accepted | Fail-fast non-wildcard, non-loopback | **Hardened (SEC-04)** |
| **Proxy Spoofing Defense** | Gin trusted all CIDRs (`0.0.0.0/0`) | Explicit CIDR loopback configuration | **Hardened (SEC-05)** |
| **Observability Exposition** | Basic ad-hoc logging | Standard `/metrics` Prometheus scrapable | **Delivered** |
| **Dead Camera Ingestion** | 118 offline cameras queried | 89 culled, 29 corridor-floor protected | **Delivered** |
| **Open GitHub Issues** | 8 open issues | 0 open issues | **100% Closed** |

---

## 2. Delivered Capabilities & Operational Hardening

### S18-T1: Turn-by-Turn Hazard Intersections (PR #524 / Issue #523)
- Implemented `apps/web/src/components/NavigationAlerts.tsx` and `apps/web/src/utils/navigationAlertUtils.ts`.
- Projects upcoming flood zones sequentially along arterial polylines with vehicle clearance limits.
- Validated with comprehensive unit tests in `apps/web/src/utils/navigationAlertUtils.test.ts`.

### S18-T2: Pure Go VAPID Web Push Notifications (PR #538 / Issue #525)
- Implemented `services/go-server/internal/push/` featuring pure Go ECDSA P-256 signing, RFC 8291 ECE payload encryption, and automated dead subscription pruning (HTTP 404/410 handling).
- Built strict SSRF defense with constant browser push gateway domain allowlists.

### S18-T3: Historical Flood Trends & Tidal Correlation (PR #542 / Issue #526)
- Implemented `services/go-server/internal/corridor/` historical depth queries and `apps/web/src/components/CorridorFloodTrendsModal.tsx`.
- Integrated 12-hour historical water depth trends alongside astronomical tide curves.
- Enforced CodeQL integer overflow bounds (CWE-190).

### S18-T4: Crowd-Sourced Hazard Corroboration (PR #543 / Issue #527)
- Implemented multi-sensor spatial corroboration in `services/go-server/internal/feedback/`.
- Merges citizen hazard submissions within 300m of cameras into verified clusters.
- Added `VerifiedReportBadge.tsx` with monochromatic visual status hierarchy.

### S18-T5: INT8 Post-Training Static Quantization (PR #544 / Issue #528)
- Implemented `cv-engine/quantize_ptq.py` and static calibration pipeline over representative rain/flood frames.
- Preserved FP32 detection heads while quantizing convolutional backbones to INT8.
- Validated via `tools/eval/benchmark_quantized_inference.py`.

### S18-T6: OpenMetrics Telemetry Exporter (PR #545 / Issue #529)
- Implemented leaf collector modulith in `services/go-server/internal/metrics/` exposing `GET /metrics`.
- Exposes telemetry for corridor saturation, frame processing latency, queue drops, and SQLite replication lags.
- Guarded by `METRICS_ALLOWED_CIDR` IP filtering.

### S18-T7: Production CORS Gating & Trusted Proxies (PR #546 / Issue #530)
- Implemented `ValidateCORSOrigins` in `services/go-server/internal/middleware/cors.go` and `internal/config/config.go`.
- Startup aborts immediately if production environment lacks explicit non-wildcard, non-loopback CORS origins.
- Configured Gin `SetTrustedProxies` using `TRUSTED_PROXIES` environment variable, preventing client IP spoofing via `X-Forwarded-For`.

### S18-T8: Dead Feed Health Culling & Corridor Floor (PR #547 / Issue #531)
- Created `tools/eval/cull_dead_cameras.py` and runbook `docs/runbooks/camera_catalog_maintenance.md`.
- Evaluates 14-day harvest status; protects arterial corridors from dropping below 50% active cameras.
- Successfully culled 89 dead feeds while preserving 29 critical corridor feeds.

---

## 3. Infrastructure Resiliency & Maintenance Hotfixes

1. **Liveness vs. Readiness Health Separation (PR #534 / Issue #533)**:
   - Split `/healthz` (liveness: process alive) from `/readyz` (readiness: DB, CV engine connected).
   - Prevents container restart loops during transient peripheral mesh downtime.
2. **CUDA Execution Provider Serialization (PR #536 / Issue #535)**:
   - Added thread-safe mutex lock around ONNX Runtime CUDA session initialization and inference execution.
   - Eliminates CUDA driver race conditions under concurrent worker workloads.
3. **SQLite Hot Backup Convergence (PR #540)**:
   - Updated backup job to execute `VACUUM INTO` snapshots, ensuring replication convergence without locking active writers.
4. **CI Staging Serialization (PR #537)**:
   - Serialized staging deployment workflows to eliminate concurrent Portainer stack race conditions.

---

## 4. Empirical Verification Evidence & Production Proofs

- **Integrity Validation**: Evaluated with `python3 tools/ci/check_evidence_integrity.py`: 100% PASS across all artifacts.
- **Go Backend Verification**: `make test-go` and `make build-go` 100% green with zero compilation or race warnings.
- **Web Client Verification**: Full test suite (`apps/web/`) passing 100% green.
- **Documentation Verification**: `python3 tools/docs/docs_lint.py` passing with 0 new violations and strict compliance with the 20,480 B cap on `docs/lessons/INDEX.md`.
- **Security Scans**: 100% green status on all GitHub Advanced Security CodeQL analysis runs (Actions, Go, JavaScript, Python).

---

## 5. Engineering Lessons Learned (L120–L129)

- **L120 (Issue #523)**: Sequential Polyline Hazard Projection & Clearance Adaptation.
- **L121 (Issue #525)**: Pure Go VAPID Cryptography, ECE Encryption & Dead Push Pruning.
- **L122 (Issue #526)**: Historical Flood Depth Trend Aggregation & Query Bounding.
- **L123 (Issue #527)**: Spatial Proximity Clustering & Bayesian Corroboration.
- **L124 (Issue #528)**: INT8 Post-Training Static Quantization & Detection Head Preservation.
- **L125 (Issue #529)**: Standard OpenMetrics Exposition & Leaf Collector Modulith.
- **L126 (Issue #530)**: Strict Production CORS Gating & Gin Trusted Proxy Hardening.
- **L127 (Issue #531)**: Arterial Corridor Coverage Floor Protection During Camera Culling.
- **L128 (Issue #533)**: Decoupled Liveness and Readiness Probes in Edge Architecture.
- **L129 (Issue #535)**: Thread-Safe Session vs. Thread-Safe GPU Context Serialization.

---

## 6. Settled Architecture Decisions & Horizon for Sprint 19

### Settled Decisions
1. **Push Cryptography**: Standard library implementation (`crypto/ecdsa`, `crypto/aes`) remains permanent; no third-party wrapper dependencies.
2. **Model Serving**: INT8 quantized ONNX models serve as standard edge artifacts; FP32 models reserved for baseline training and validation.
3. **Security Invariants**: SEC-04 and SEC-05 are permanent release gates; wildcard origins and unbound proxies strictly fail-fast.
4. **Health Invariants**: Container orchestrators must probe `/healthz` for liveness and `/readyz` for traffic routing.

### Sprint 19 Horizon
1. Integrate Web Push localized flood alert trigger into backend event pipeline.
2. Production deployment of INT8 quantized models to secondary edge nodes.
3. Grafana dashboard templates consuming new `/metrics` Prometheus endpoint.
4. Automated weekly camera catalog health sync GitHub Action workflow.
