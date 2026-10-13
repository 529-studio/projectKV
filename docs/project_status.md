# Project Status & AI Contributor Onboarding Guide

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.2.0  
**Last Updated:** 2026-10-13  


---

## 1. Executive Overview & System Purpose

ProjectKV provides a real-time, zero-click urban traffic and flood condition explorer for Ho Chi Minh City, Vietnam. Commuters explore live streaming camera snapshots, corridor traffic saturation levels, and rain/flood layers before traveling to avoid bottlenecks and submerged streets.

---

## 2. Microservice Architecture & Port Allocations

| Service | Technology | Port | Host & Node Role | Primary Responsibilities |
|---|---|:---:|---|---|
| **Web Client** (`apps/web`) | Next.js 15 (App Router), React 19, Tailwind CSS, Leaflet | `3001` | `homesrv` (Portainer Endpoint 3) | Map visualization, discrete camera marker dots, H3 hex overlays, horizontal drag-to-scroll pinned camera dock, bilingual UI (`vi` default with full diacritics, `en` secondary). |
| **Backend API** (`services/go-server`) | Go 1.24, Gin, SQLite (WAL mode) | `3000` | `homesrv` (Portainer Endpoint 3) | Snapshot proxy cache with 15s in-memory TTL, camera catalog, hotset scheduler (30s cycle), PCU Saturation Index calculator, SSE (`/api/live/stream`), WebSockets (`/ws/live`), Weather forecast (`/api/weather`), Swagger docs. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR runtime, YOLOv8n FP16 | `8000` | `dev-box` worker (Portainer Endpoint 4) | Automated vehicle detection (`POST /predict`), class counting (motorcycle, car, bus, truck), `GET /healthz` healthchecks, multi-threaded request processing, 2 vCPU / 2 GB RAM capped. |

---

## 3. Current Operational Metrics & Repository Status

- **Lifetime PR History**: Exactly **134 Pull Requests** processed across repository history:
  - **126 Merged** to `dev` (including PR #206 `docs/sprint4-report`, PR #211 `tooling/s5-t1-rerun-d6-benchmark`, PR #212 `feat/r2-storage-integration`, PR #213 `infra/s5-t4-tailscale-failover-drill`, PR #215 `tooling/s5-t2-cv-detection-audit`, PR #216 `feat/s5-t3-hybrid-flood-pipeline`, PR #217 `docs/pr-issue-linking-rule-and-template`, PR #218 `feat/s5-t4-r2-shared-team-labeler`, PR #220 `docs/sprint5-report`, PR #227 `docs/adr-007-sprint6-plan`, PR #229 `docs/sprint6-execution-guide`, and PR #231 `docs/readme-fullstack-dev-guide`)
  - **8 Closed** without merging
  - **0 Open** PRs pending
- **Staging Infrastructure Configuration (Stack 14)**:
  - `docker-compose.stg.yml` explicitly parameterizes `APP_ENV=${APP_ENV:-staging}` to prevent `GIN_MODE=release` from defaulting environment to `production` and triggering safety crashes during mock/staging runs.
  - Aligned with `.env.stg.example` template as the authoritative single source of truth for Portainer CD redeploy jobs.
- **Camera Catalog**:
  - **837 total cameras** loaded in `cameras_enriched.json`
  - **712 online cameras** (status `UP`, `isOnline: true`) and **125 offline** (status `NOT_IMAGE`) as of the latest city-wide health audit (2026-10-06)
  - Static liveness (`isOnline` / `status`) is verified by live upstream probing; runtime placeholder detection runs in `proxycache` and surfaces `503 GHOST_CAMERA`
  - **57 curated intersection cameras (Hot Set)** across 4 priority commuter corridors:
    1. Võ Văn Kiệt (`vo_van_kiet`): 23 cameras
    2. Mai Chí Thọ (`mai_chi_tho`): 13 cameras
    3. Điện Biên Phủ (`dien_bien_phu`): 11 cameras
    4. Cộng Hòa (`cong_hoa`): 10 cameras
  - Curated strictly via A6 rule: `len(road_keys) == 2` (n nút giao, descartes mid-block straight stretches).
- **Camera Health Audit & Catalog Resync (Issue #178, PR #179)**:
  - `tools/crawler/audit_all_cameras.js` probes all 837 snapshots against the live DOT upstream (15 workers, 5s timeout) and classifies each feed (`SUCCESS`, `GHOST_PLACEHOLDER`, `EMPTY_BODY`, `TIMEOUT`, `NETWORK_ERROR`, `HTTP_*`).
  - `tools/crawler/sync_catalog_from_audit.js` re-synchronizes `cameras_all.json` / `cameras_enriched.json` from the probe report (deterministic outcomes only; transient errors leave entries untouched). This replaced the former hardcoded `KnownGhostCameras` map, which had drifted out of sync, making the JSON catalog the single source of truth for static liveness.
  - `.github/workflows/camera-health-audit.yml` runs the audit daily (01:00 UTC) and opens a resync Pull Request against `dev` when drift is detected (never auto-merges).
  - The web client promotes a `503 GHOST_CAMERA` snapshot response into shared state (`useCameras.markCameraGhost`), immediately reflecting field signal loss across the map, sidebar, dock and modal.
- **Known-State Smoke-Test Camera Set (Sprint 4 T7, Issue #187, PR #193)**:
  - Curated 10 high-confidence ground-truth cameras in `tools/eval/fixtures/smoke_test_cameras.json` (5 `CLEAR` off-peak baselines, 5 `CONGESTED` peak rush-hour baselines) across 4 priority corridors (`vo_van_kiet`, `mai_chi_tho`, `dien_bien_phu`, `cong_hoa`).
  - Implemented automated verification script `tools/eval/verify_smoke_set.py` and unit test suite `tools/eval/verify_smoke_set_test.py` to objectively gate model predictions without relying on anecdotal staging UI spot-checks.
  - Detailed physical and operational rationale documented in `tools/eval/smoke_test_cameras.md`.
- **Evaluation Dataset & Progress Tracking (Sprint 4 T3, Issue #194, PR #199)**:
  - Strictly excluded all pre-10-06 mock frames (107 bytes) from ground-truth dataset.
  - Implemented `tools/eval/collect_real_frames.py` and `tools/eval/dataset_progress.py` to fetch, validate, and audit real frames against ADR-004 §D6 targets.
  - Completed ground-truth dataset via live passive environmental collection during the 2026-10-08 HCMC rainstorm: **764 real labeled frames** across 206 unique cameras, including **120 flood-positive frames** (98 shallow, 22 deep) and 468 night rain frames.
  - All ADR-004 §D6 composition quotas satisfied: Real frames 764/300 (254.7%), Flood 120/60 (200.0%), Clear/dry/day 145/100, Moderate 199/50, Heavy/congested 181/50, Rain 468/40, Night 468/30.
  - Dataset status: **READY FOR D6 ACCURACY GATE (T4)**.
- **Real D6 Accuracy Gate & OpenVINO Benchmark (Sprint 4 T4, Issue #195)**:
  - Executed `tools/eval/benchmark.py` against live self-hosted OpenVINO container across the full 764 real frames.
  - Traffic Macro F1: **0.209** (Weighted 0.364), highlighting uncalibrated cold-start capacity floors and dense occlusion under congestion (informing Sprint 4 T5 PCU calibration).
  - Flood Safety Gate: **FAIL (100.0% FNR > 5.0%)**, properly measuring that the stock vehicle-only COCO model does not detect standing water on the 120 flood frames (informing Sprint 4 T9 multimodal flood pipeline).
  - Known-state smoke-test validation: **2/10 PASS (20.0%)** on curated baselines (`docs/reports/smoke_eval_d6_real.md`).
  - Full empirical findings committed to `docs/reports/eval_report_d6_real.md` and `docs/reports/dataset_progress.md`.
- **PCU Calibration & Saturation Index Realignment (Sprint 4 T5, Issue #196, PR #202)**:
  - Calibrated default Passenger Car Unit (PCU) weights against empirical HCMC traffic data and Vietnamese urban transport standards (**TCVN 4054:2005** / **TCVN 13592:2022**): `Motorcycle = 0.30` (from 0.20), `Car = 1.00`, `Bus = 2.50` (from 2.00), `Truck = 2.00`, `Container = 2.00` (folded into Truck per ADR-005 T6).
  - Realigned cold-start `CapacityFloor` from `25.0` down to `15.0` PCU matching the physical queue geometry of a 20–30m CCTV viewport (3 lanes x 4–5 queue vehicles).
  - Recalibrated Saturation Index Level of Service (LOS) cutoffs: `Moderate = 0.30` (from 0.35), `Heavy = 0.60` (from 0.70), `Congested = 0.80` (from 0.90).
  - Preserved backward compatibility with custom environment variable overrides (`PCU_WEIGHT_*`, `PCU_CAPACITY_FLOOR`, `PCU_THRESHOLD_*`).
  - Full rationale, empirical distribution findings, and comparison tables documented in `docs/pcu-calibration-report.md`.
- **Architectural Decision: OpenVINO vs. VLM for Traffic (Sprint 4 T9, Issue #197, PR #205)**:
  - Evaluated self-hosted OpenVINO vs. cloud VLM (Gemini Flash-Lite) across throughput, hardware utilization, latency, and economics following Sprint 4 T4 real-world benchmarks.
  - Adopted **Option C: Decoupled Hybrid Architecture**: Retained self-hosted OpenVINO as the core continuous 30-second traffic telemetry engine ($0 API cost, 24.01 FPS on `dev-box`, 18.2ms P50 latency, 0.00% failure rate across 3,383 requests, vs. VLM's ~$111/mo cost and 228s latency for 57 cameras).
  - Outlined resolution scaling ($320 \times 320 \rightarrow 416 \times 416$) on `dev-box` to address motorcycle recall while reserving cloud VLM strictly for low-frequency on-demand modal checks and rain-triggered flood audits.
  - Formal Architectural Decision Record committed in `docs/decision-openvino-vs-vlm.md`.
  - All Sprint 4 backlog items (T1–T9) are now completed or decided, unblocking Sprint 5 long-term routing work (`route.Engine`).
- **Sprint 4 Completion & Reality-Check Audit Report**:
  - Full audit of all 9 Sprint 4 backlog tickets (T1–T9), empirical evaluation findings, staging self-hosting readiness audit, and Sprint 5 recommendations compiled in [`docs/sprint4_report.md`](sprint4_report.md).
  - Technical audit Q&A resolving all 13 critical verification inquiries from reviewers across 4 thematic groups with line-by-line codebase citations committed in [`docs/sprint4_audit_qa.md`](sprint4_audit_qa.md).
  - Ground-truth reality check and authoritative Sprint 5 execution roadmap adopted in [`docs/ADR-006-sprint4-audit-and-sprint5-plan.md`](ADR-006-sprint4-audit-and-sprint5-plan.md) (PR #206), establishing mandatory empirical gating (S5-T1) before feature expansion, upgrading detector resolution to $416\times 416$ (S5-T3), centralizing blob storage for team web labeling (S5-T4), and hardening network resilience (S5-T6).
- **Sprint 5 Backlog & Execution Guide**:
  - Four active GitHub issues created on `529-studio/projectKV`:
    - [#207](https://github.com/529-studio/projectKV/issues/207): S5-T1 — `[tooling] re-run D6 benchmark and smoke test with post-T5 weights` (**Completed / Verified**): Re-ran real D6 benchmark across all 764 frames with post-T5 weights (`CapacityFloor=15.0`). Traffic Macro F1 resulted in **0.206** (Weighted 0.351), confirming the ADR-006 Finding 2 mathematical paradox: lowering CapacityFloor doubled false-positive CONGESTED classifications on actual CLEAR frames (from 22 to 46 frames, +109%) without resolving missed bottlenecks (67/73 CONGESTED frames missed as CLEAR). Flood Safety Gate remained at 100.0% FNR. Smoke pass rate reached 50.0% (5/10), unblocking S5-T2 object detection audit.
    - [#208](https://github.com/529-studio/projectKV/issues/208): S5-T2 — `[cv-engine] object detection audit for false-positive/missed detections` (**Completed / Verified**): Diagnosed root causes for both perception failure modes (a) missed congestion queues and (b) clear-road hallucinations. Proved that missing Non-Maximum Suppression (NMS) in `cv-engine/server.py` caused a 2.7x–4.7x anchor duplication multiplying 3–5 cars into 28–39 detections, and 1.78x non-uniform aspect ratio stretch crushed detection confidence by ~35% on real queues. Identified ground-truth annotation discrepancy on `Mai Chí Thọ - Đồng Văn Cống 2` (active construction zone with physically empty road). Formulated 5 concrete resolution proposals (NMS with IoU 0.45, letterboxing, 416x416 resolution upgrade, cross-class NMS, and ground-truth correction). Committed automated audit tooling (`tools/eval/audit_cv_detections.py`), visual overlays (`docs/reports/audit_overlays/`), structured findings (`docs/reports/cv_detection_error_audit.json`), and comprehensive audit report (`docs/reports/cv_detection_error_audit.md`).
    - [#209](https://github.com/529-studio/projectKV/issues/209): S5-T3 — `[go-server + web] decoupled hybrid flood pipeline` (**Completed / Merged in PR #216**).
    - [#210](https://github.com/529-studio/projectKV/issues/210): S5-T4 — `[infra] physical Tailscale disconnection test for dev-box failover` (**Completed / Verified**): Executed live physical network partition and worker outage chaos drill on Staging (`homesrv` Stack 14 $\leftrightarrow$ `dev-box` Stack 17). Demonstrated 100% clean graceful degradation to `SeverityUnknown` (`source: "fallback"` then `source: "circuit_breaker"`) with 0 panics, 0 memory leaks (RAM bounded at 37.8 MB, 14.8%), and zero frozen stale states. Verified standby container `warded-model` reachability within `core-network`. Verified automated Circuit Breaker reset and live telemetry recovery upon worker restoration. Runbook documented in `docs/runbook_dev_box_failover.md` and empirical proof in `docs/reports/tailscale_failover_drill_report.md`.
    - [#214](https://github.com/529-studio/projectKV/issues/214): S5-T4 — `[infra + web] S5-T4 — R2 blob storage for archiver + shared team labeler` (**Completed / Verified**): Implemented storage `List` abstraction across Local and R2 drivers, backend archiver proxy streaming and label persistence endpoints, and built collaborative monochromatic Next.js team web labeler (`apps/web/src/app/labeler`).
  - Operational wave guide and `agy` CLI instructions compiled in [`docs/sprint5_execution_guide.md`](sprint5_execution_guide.md).
- **Sprint 6 Backlog Roadmap & Perception Hard Gate (ADR-007)**:
  - Formulated in [`docs/ADR-007-sprint5-audit-and-sprint6-plan.md`](ADR-007-sprint5-audit-and-sprint6-plan.md) with 5 tracked GitHub issues:
    - [#221](https://github.com/529-studio/projectKV/issues/221): S6-T1 — `[cv-engine + tooling] Implement NMS, Letterbox, 416px Scaling & Re-run 764-Frame D6 Benchmark` (**Completed / Verified**): Implemented aspect-preserving `letterbox` (16:9, neutral gray padding `(114, 114, 114)`) and `nms_cpu` ($\text{IoU} \le 0.45, \text{conf} \ge 0.25$) in `services/cv-engine/server.py`. Upgraded model input resolution to $416\times 416$ OpenVINO FP16 (`[1, 3, 416, 416]` input tensor, `[1, 84, 3549]` output tensor). Re-ran the complete 764-frame real benchmark: Traffic Macro F1 improved from **0.206 to 0.245** (+18.9%), Traffic Weighted F1 improved from **0.351 to 0.402** (+14.5%), CONGESTED false alarms on CLEAR frames dropped by **50.0%** (from 46 frames down to 23), and MODERATE correct predictions tripled (21 to 63, +200%). All 10 unit tests pass (`make test-model`). Hard Gate satisfied, unblocking downstream S6-T2 traffic calibration. Full report committed in [`docs/reports/eval_report_d6_sprint6.md`](reports/eval_report_d6_sprint6.md).
    - [#222](https://github.com/529-studio/projectKV/issues/222): S6-T2 — `[go-server] Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage` (Est: 1d)
    - [#223](https://github.com/529-studio/projectKV/issues/223): S6-T3 — `[web] CameraModal UI/UX Redesign (Tactile Affordance) & 15s Client-Side Cooldown` (Est: 1d)
    - [#224](https://github.com/529-studio/projectKV/issues/224): S6-T4 — `[ci + ops] Automated Secret Scanning in GitHub Actions CI (Gitleaks)` (Est: 0.5d)
    - [#225](https://github.com/529-studio/projectKV/issues/225): S6-T5 — `[go-server] Telegram Flood Alert Dispatcher (internal/notify, Subscription Store & Debounce)` (Est: 2–2.5d)
  - Explicit architectural deferral: Route A→B deferred pending perception ground-truth verification and dedicated routing engine spike.
  - Operational multi-wave guide and `agy` CLI instructions compiled in [`docs/sprint6_execution_guide.md`](sprint6_execution_guide.md).


- **Inference Runtime**:
  - Model: Stock COCO YOLOv8n OpenVINO IR 416x416 FP16 (~6.3 MB) with NMS ($\text{IoU} \le 0.45, \text{conf} \ge 0.25$) and aspect-preserving letterboxing
  - Latency: ~13–30ms per frame on host CPU, ~425ms P50 on non-AVX Celeron hardware; 57 cameras polled every 30s (~1.90 FPS, well within 2.27 FPS capacity).
  - Traffic Scoring: Passenger Car Unit (PCU) Saturation Index ($SI = PCU_{current} / PCU_{capacity}$), mapping into `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`.
  - Archiver Direct Frame Delivery (Issue #182): Archiver pipeline directly saves the analyzed frame (`Verdict.RawFrame`) forwarded from the inference analyzers (`cvengine`, `cloudvision`, `mock`), eliminating reliance on viewer proxy cache hits while preserving fallback to `proxycache` and mock frames.
  - Container Vehicle Class Resolution (Issue #186, ADR-005 T6): Resolved dead code gap where COCO model only outputs 4 vehicle classes. Per `docs/decision-container-class-gap.md`, Container is folded into heavy freight vehicle weight (`Truck = 2.0`), aligning with COCO ground truth and HCMC peak-hour freight restrictions.
- **Weather Service**:
  - Open-Meteo current & hourly forecast integrated with 5-minute in-memory cache and stale fallback.
  - Aligned to target hour bucket (`truncateToHour`) to resolve 15-minute `current.time` vs hourly `hourly.time` divergence (#172).
  - 48-hour upstream lookahead buffer (`forecast_days=2`) enabling rolling 12-hour forecast windows spanning past midnight.
- **Signal Separation**:
  - Independent layer toggles: Ground-truth camera-verified flood hexes (`HexOverlay.tsx`) are completely separated from Doppler weather radar reflectivity (`RainRadarLayer.tsx`).
- **GitHub Actions Compute (Oct 2026 Audit, #173)**:
  - Consolidated the standalone `go-ci.yml` into the `ci.yml` `verify-backend` job, removing duplicate Go verification on every backend PR.
- **Distributed Worker Architecture (`dev-box` on Endpoint 4)**:
  - Offloaded heavy OpenVINO YOLOv8n inference from edge server `homesrv` (Intel Celeron N2930, 4 cores, no AVX2) to dedicated worker VM `dev-box` (`100.118.7.57` via Tailscale, AMD Ryzen 7 6800HS, 8 vCPU, 8 GB RAM, strictly quota-capped at 20 GB disk).
  - Deployed container `warded-cv-engine` via Portainer Stack 17 (`docker-compose.cv.yml`) on Endpoint 4 with strict cgroup limits (`cpus: "2.00"`, `memory: "2048M"`).
  - Automated Disk Guard daemon (`/usr/local/bin/docker-disk-guard.sh`) installed via root cron, purging build cache and untagged images whenever root disk usage exceeds 80% (16 GB ceiling).
  - Comprehensive empirical stress tests:
    - Hot Set (50 cameras): Completed batch in 2.24s (22.29 FPS, 44.8ms/camera), 0 errors.
    - Citywide (708 cameras): Completed batch in 30.77s (23.01 FPS, 43.4ms/camera), 0 errors, container memory stabilized at 131 MB RAM (6.4% of 2 GB limit).
- **Decoupled CI/CD Pipeline (PR feat/distributed-cv-cicd-pipeline)**:
  - `ci.yml`: Added path filter `model` (`services/cv-engine/**`) and automated Python test job `verify-model` (`make test-model`).
  - `docker-publish.yml`: Independent build and deployment targeting:
    - Stack 14 on Portainer Endpoint 3 (`homesrv`) for backend/frontend changes.
    - Stack 17 on Portainer Endpoint 4 (`dev-box`) for CV engine changes.
    - Post-deploy verification checking both public web health (`https://warded-stg.529studio.site/health`) and remote Portainer container runtime state (`warded-cv-engine` healthy).
- **Cloudflare R2 Object Storage Integration (`warded-proofs`, PR `feat/r2-storage-integration`)**:
  - Implemented `R2Storage` satisfying `storage.Storage` interface using official AWS SDK for Go v2 (`s3.NewFromConfig`).
  - Mandatory S3 Path-Style addressing enabled (`UsePathStyle = true`) for compatibility with Cloudflare R2 (`https://bb67574163a065e4aad475fb3aa6bb92.r2.cloudflarestorage.com`).
  - Configurable storage driver via `STORAGE_DRIVER` (`local` default for local dev, `r2` / `s3` for staging and production).
  - Standardized snapshot object key path to Vietnam ICT (`Asia/Ho_Chi_Minh`, UTC+7): `YYYY-MM-DD/<camera_id>/<unix_timestamp>_<reason>.jpg`.
  - Public Edge CDN delivery integration via `https://file.529studio.site`, unblocking collaborative web labeling workflows for the entire engineering team.
  - Implemented and verified standalone CLI verification commands in Go (`services/go-server/cmd/verify-r2`) and Python (`tools/storage/verify_r2.py`), confirming 100% upload, head, get, edge CDN (HTTP 200), and cleanup success against live Cloudflare R2.
- **Decoupled Hybrid Flood Pipeline (Sprint 5 Task S5-T3, Issue #209)**:
  - Curated 10 authentic municipal flood hotspots in `tools/eval/fixtures/flood_chokepoints.json` based on official Sở Xây dựng TP.HCM and UDI flood monitoring bulletins, with zero overlap with the 10 traffic smoke cameras.
  - Reconciled rainfall threshold divergence across frontend (`rainHexUtils.ts`) and backend documentation at 1.0 mm (wet pavement onset), 4.0 mm (shallow flood risk), and 15.0 mm (deep flood risk). Corrected documentation discrepancies cited in ADR-006 Finding 3.
  - Implemented dual-trigger VLM dispatch subsystem under leaf modulith package `services/go-server/internal/flood`:
    1. *Trigger 1 (Environmental)*: Background polling loop inspecting flood chokepoints only during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$ or active rain WMO codes), consuming exactly 0 VLM requests during dry weather.
    2. *Trigger 2 (User On-Demand)*: Commuter-initiated verification via `POST /api/flood/verify?camera_id=...` action button in the camera modal.
  - Enforced strict in-process rate limiting (10 RPM sliding window) and daily quota budgeting (300 RPD calendar day rollover) via thread-safe `QuotaLimiter`.
  - Enhanced web client `CameraModal.tsx` with a monochromatic `[XÁC MINH NGẬP NƯỚC]` action button, loading spinner, and bilingual zero-emoji status badges (`[XÁC MINH VLM: KHÔ RÁO]`, `[XÁC MINH VLM: ĐƯỜNG ƯỚT]`, `[XÁC MINH VLM: NGẬP NÔNG]`, `[XÁC MINH VLM: NGẬP SÂU]`).
  - Regenerated Swagger API documentation via `make swagger` (`/api/flood/verify`, `/api/flood/chokepoints`, `/api/flood/quota`).
  - Verified 100% test coverage across Go backend (`./internal/flood/...`, `./...`) and web client (93 unit tests).
- **R2 Blob Storage Archiver & Shared Team Labeler (Sprint 5 Task S5-T4, Issue #214)**:
  - Extended core `storage.Storage` interface with object listing method `List(ctx, prefix) ([]Item, error)` implemented across both `LocalStorage` (filesystem walk) and `R2Storage` (`s3.ListObjectsV2` pagination).
  - Implemented secure backend archiver HTTP routes in `services/go-server/internal/archiver`:
    - `GET /api/archiver/dates`: returns sorted partition dates (e.g. `["2026-10-11", "2026-10-10"]`).
    - `GET /api/archiver/snapshots`: browses snapshot items with metadata, pagination, and camera filtering.
    - `GET /api/archiver/snapshots/raw`: streams raw JPEG binary with immutable cache headers and strict path traversal validation (`ErrInvalidPath`), bypassing Cloudflare Access Zero Trust redirects.
    - `POST /api/archiver/labels` and `GET /api/archiver/labels`: saves and aggregates ground-truth JSON annotations directly in storage (`labels/{date}/{camera}_{annotator}_{timestamp}.json`).
  - Built monochromatic collaborative Next.js web labeler at `apps/web/src/app/labeler/page.tsx` with high-contrast UI, bilingual support (`vi` default), zero emojis, and rapid-fire hotkeys (`1-3` flood, `Q-W-E-R` traffic, `A-S-D` weather, `Z-X-C` time of day, `Enter` save/next).
  - Enhanced standalone evaluator tool `tools/eval/labeler.html` with backend API synchronization bar (`Load from API`).
  - Adoption of R2 blob storage explicitly justified for collaborative team labeling friction elimination, **NOT** because system reached the ADR-004 §D7 disk capacity limit.
  - Implemented multi-tier Developer Access Token gatekeeper (`middleware.RequireDevAuth`) protecting `/labeler` and `/api/archiver/*` against data poisoning and unauthorized access on production:
    - Backend: Supports `X-Dev-Token`, `Authorization: Bearer`, and `?token=` / `?dev_token=` query params (for raw `<img>` binary streaming), validated via `crypto/subtle.ConstantTimeCompare`.
    - Fail-Closed: Automatically returns `503 Service Unavailable` on production/staging if `DEV_ACCESS_TOKEN` is unset or blank.
    - Frontend: In-place monochromatic lock screen at `/labeler`, session persistence via `sessionStorage`, and automatic token eviction upon receiving `401 Unauthorized`.

---


## 4. Local Development Quickstart

```bash
# 1. Start all core web and backend services:
make dev-all
# - Go Backend API:       http://localhost:3000
# - Next.js Frontend Dev: http://localhost:3001

# 2. Start OpenVINO vehicle detection container (requires Colima / Docker):
colima start --cpu 2 --memory 4
make dev-model
# - Healthcheck endpoint: http://localhost:8000/healthz

# 3. Run automated test suites:
make test-go       # Full Go backend test suite with -race
make test-pcu      # PCU Saturation Index boundary test suite
make test-enrich   # Geo-enrichment catalog filter unit tests
cd apps/web && npm test  # Web client unit test suite (92 tests)
```

---

## 5. Non-Negotiable AI Development Guardrails

1. **Grounded Single Source of Truth**: All progress, metrics, and architecture claims must be verified directly against git commit history, PR list, or repository files. Never fabricate numbers.
2. **Issue Estimation Mandate**: Always provide an explicit time and effort estimate before implementing any issue.
3. **Sound Solutions Over Shortcuts**: Once an issue is estimated, never shy away from implementation effort or choose sub-optimal shortcuts out of convenience. Always build the most robust and architecturally sound solution for the project.
4. **End-to-End Bug Reproduction**: Always simulate and reproduce bugs from a real user/network perspective before touching code. Unit tests passing in isolation is never acceptable proof of correctness.
5. **Git Flow**: Zero direct push to `main` or `dev`. Always branch (`feat/*`, `fix/*`, `docs/*`), push, and open PR against `dev`. AI agents must NEVER auto-merge PRs.
6. **Zero-Emoji Compliance**: No Unicode emojis in UI controls, chips, modal headers, notifications, or backend Go log messages. Use monochromatic SVGs and uppercase structured log prefixes (`[INIT]`, `[INFO]`, `[WARN]`, `[ERROR]`, `[FATAL]`).
7. **Documentation Synchronization**: Whenever a PR is created or merged, update `docs/project_status.md` and `docs/lesson_learn.md`.
8. **Zero Hardcoded Secrets**: Never hardcode credentials, tokens, or API keys in code or scripts. All secrets must be resolved dynamically from `.env`. Fallback parameters in `os.getenv` or configuration loaders must strictly be `""` (empty string) with fail-fast validation. Documentation templates must strictly use `<placeholder>` syntax.
9. **PR Issue Linking Mandate**: PR descriptions must strictly start with `Close #<issue-number>` format to link and auto-close issues upon merge. Never use non-linking phrases like `Addresses Issue #...` or `Implement #...`. Follow `.github/pull_request_template.md`.

