# Project Status & AI Contributor Onboarding Guide

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.0.2  
**Last Updated:** 2026-10-06  

---

## 1. Executive Overview & System Purpose

ProjectKV provides a real-time, zero-click urban traffic and flood condition explorer for Ho Chi Minh City, Vietnam. Commuters explore live streaming camera snapshots, corridor traffic saturation levels, and rain/flood layers before traveling to avoid bottlenecks and submerged streets.

---

## 2. Microservice Architecture & Port Allocations

| Service | Technology | Port | Primary Responsibilities |
|---|---|:---:|---|
| **Web Client** (`apps/web`) | Next.js 15 (App Router), React 19, Tailwind CSS, Leaflet | `3001` | Map visualization, discrete camera marker dots, H3 hex overlays, horizontal drag-to-scroll pinned camera dock, bilingual UI (`vi` default with full diacritics, `en` secondary). |
| **Backend API** (`services/go-server`) | Go 1.24, Gin, SQLite (WAL mode) | `3000` | Snapshot proxy cache with 15s in-memory TTL, camera catalog, hotset scheduler (30s cycle), PCU Saturation Index calculator, SSE (`/api/live/stream`), WebSockets (`/ws/live`), Weather forecast (`/api/weather`, 48h Open-Meteo buffer with hour alignment), Swagger docs. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR runtime, YOLOv8n FP16 | `8000` | Automated vehicle detection (`POST /predict`), class counting (motorcycle, car, bus, truck), `GET /healthz` healthchecks, multi-threaded request processing. |

---

## 3. Current Operational Metrics & Repository Status

- **Lifetime PR History**: Exactly **106 Pull Requests** processed across repository history:
  - **97 Merged** to `dev`
  - **8 Closed** without merging
  - **1 Open** PR currently pending (#179 targeting `dev`, camera catalog resync for Issue #178)
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
- **Inference Runtime**:
  - Model: Stock COCO YOLOv8n OpenVINO IR 320x320 FP16 (~6.3 MB)
  - Latency: ~425ms P50 per frame on non-AVX Celeron hardware; 57 cameras polled every 30s (~1.90 FPS, well within 2.27 FPS capacity).
  - Traffic Scoring: Passenger Car Unit (PCU) Saturation Index ($SI = PCU_{current} / PCU_{capacity}$), mapping into `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`.
- **Weather Service**:
  - Open-Meteo current & hourly forecast integrated with 5-minute in-memory cache and stale fallback.
  - Aligned to target hour bucket (`truncateToHour`) to resolve 15-minute `current.time` vs hourly `hourly.time` divergence (#172).
  - 48-hour upstream lookahead buffer (`forecast_days=2`) enabling rolling 12-hour forecast windows spanning past midnight.
- **Signal Separation**:
  - Independent layer toggles: Ground-truth camera-verified flood hexes (`HexOverlay.tsx`) are completely separated from Doppler weather radar reflectivity (`RainRadarLayer.tsx`).
- **GitHub Actions Compute (Oct 2026 Audit, #173)**:
  - Consolidated the standalone `go-ci.yml` into the `ci.yml` `verify-backend` job, removing duplicate Go verification on every backend PR.
  - `ci.yml` now triggers on `pull_request` only; the redundant post-merge `push` re-verification was removed (build/deploy remains covered by `docker-publish.yml`).
  - Added `concurrency` with `cancel-in-progress: true` to `ci.yml`, `branch-guard.yml`, and `auto-request-reviewers.yml` (`docker-publish.yml` already had it) to cancel superseded runs on rapid pushes.
  - Removed the informational `ci-gate` aggregation job: `dev` and `main` have no native branch protection on the Free-tier private repo, so it gated nothing.

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
