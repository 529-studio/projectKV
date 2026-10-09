# Project Status & AI Contributor Onboarding Guide

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.0.5  
**Last Updated:** 2026-10-09  

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

- **Lifetime PR History**: Exactly **116 Pull Requests** processed across repository history:
  - **108 Merged** to `dev`
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
- **Inference Runtime**:
  - Model: Stock COCO YOLOv8n OpenVINO IR 320x320 FP16 (~6.3 MB)
  - Latency: ~425ms P50 per frame on non-AVX Celeron hardware; 57 cameras polled every 30s (~1.90 FPS, well within 2.27 FPS capacity).
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
