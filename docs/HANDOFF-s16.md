# S16 Handoff & Sprint 17 Horizon

**Document**: `docs/HANDOFF-s16.md`  
**Date**: 2026-11-20  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 16 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s16-handoff`
- **Head commit**: `f135a9e` (`Merge pull request #502 from 529-studio/chore/camera-catalog-resync-20261119-40`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 16**:
  - PR #491 (`chore/camera-catalog-resync-20261117-38`)
  - PR #493 (`fix/s16-audit-security-headers-and-swagger`, Issues #492, #475, SEC-01..03)
  - PR #494 (`chore/camera-catalog-resync-20261118-39`)
  - PR #495 (`giangdq202/fix-web-nginx-headers`)
  - PR #496 (`fix/s16-ui-drawer-layout-regression`, Issue #494)
  - PR #497 (`feat(cv-engine): ONNX Runtime CUDA backend for the GPU inference host`)
  - PR #498 (`fix(cv-engine): self-heal a dead CUDA session; fail fast without a model`)
  - PR #499 (`chore(litestream): shorten replica retention to 72h`)
  - PR #500 (`chore(litestream): tighten replica retention to 48h`)
  - PR #501 (`fix/log-noise-forecast-harvester`)
  - PR #502 (`chore/camera-catalog-resync-20261119-40`)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :-------- | :------------------- |
| **#493** | **S16-T1** | `security`, `backend` | **Swagger Gating & Security Headers**: Gated Swagger UI in production; added HSTS, CSP, and Go security middleware (`headers.go`). |
| **#493** | **S16-T2** | `backend`, `web` | **Batch Road Status Summary**: Single `GET /api/roads/status-summary` endpoint eliminates client N+1 HTTP 429 requests. |
| **#493** | **S16-T3** | `backend`, `corridor` | **Route Geometry Alignment**: Fixed coordinate ordering for Quốc lộ 1 corridor preventing map polyline zig-zags. |
| **#495** | **S16-T4** | `web`, `nginx` | **Nginx Security Header Inheritance**: Bundled `nginx_security_headers.conf` inside Docker image preventing header masking on static chunks. |
| **#496** | **S16-T5** (#494) | `web`, `ux` | **Overlay Stack Normal Flow**: Shared bottom-anchored column eliminates 187px card occlusion and 36px mobile overflow. |
| **#497** | **S16-T6** | `cv`, `engine` | **ONNX Runtime CUDA Backend**: Accelerated GPU inference provider with explicit CUDA 12.4 runtime base. |
| **#498** | **S16-T7** | `cv`, `engine` | **CV Session Self-Healing**: Automated recreation of dead CUDA sessions with fail-fast missing model enforcement. |
| **#499**, **#500** | **S16-T8** | `ops`, `dr` | **Litestream Retention Pruning**: Reduced WAL retention to 48h, preserving 2-day recovery while staying under 10 GB R2 free tier. |
| **#501** | **S16-T9** | `forecast`, `ops` | **Log Hygiene & Snapshot Budget**: Demoted absent model log to INFO and made harvester snapshot timeout configurable. |
| **#491**, **#494**, **#502** | **S16-T10** | `data`, `cameras` | **Camera Catalog Resync**: Daily catalog synchronizations and purged unreferenced root capture artifacts. |

---

## 3. Open Issues & Backlog

- **All Sprint 16 tasks merged**: All security remediations, layout fixes, and CV engine upgrades are 100% merged into `dev`.
- **Sprint 17 Backlog Candidates**:
  - Commuter corridor travel duration estimation and real-time congestion delay calculation.
  - Turn-by-turn navigation hazard intersection alerts along corridor polylines.
  - Web Push API subscription manager and browser notification worker (VAPID).
  - Historical flood depth trend charts & tidal inundation correlation dashboard.
  - Automated crowd-sourced hazard report verification against nearby camera feeds.
  - INT8 quantization for ONNX models to optimize GPU memory footprint.
  - Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs.
  - SEC-04 & SEC-05 hardening: disallow localhost origin fallback in production and configure trusted proxy CIDRs.

---

## 4. Settled Architectural Decisions

- **Normal Flow for Overlays**: Floating UI elements that anchor near one another must participate in normal layout flow rather than using fragile pixel-based offsets.
- **Production Surface Minimization**: Internal contract documentation (Swagger/OpenAPI) is strictly blocked in production (`APP_ENV=production`).
- **Nginx Header Inheritance Defeat**: Every Nginx location block defining `add_header` must include `nginx_security_headers.conf` to avoid dropping security headers.
- **Fail-Closed GPU Inference**: When CUDA is configured, the CV engine refuses to silently fall back to CPU and instead rebuilds the CUDA session or terminates.
- **WAL Retention Budget**: 48-hour Litestream WAL retention provides sufficient disaster recovery safety while keeping Cloudflare R2 storage within zero-cost limits.
- **Lesson 114**: Indexed in `docs/lessons/INDEX.md` (Issue #494).

---

## 5. Pitfalls Encountered

- **Nginx `add_header` Masking**: Specifying `add_header Cache-Control` inside `location /_next/static` silently stripped all parent-level security headers (HSTS, CSP) from JavaScript assets. Solved by importing a shared include.
- **Floating Overlays Inside Flex Containers**: Placing `absolute` or `fixed` children inside a flex container fails to reserve flow space. Components must participate in normal flow (`w-full`, `w-fit`) to guarantee collision-free stacking.
- **Silent CPU Fallbacks**: Attempting fallback to CPU when CUDA sessions die can silently degrade throughput by 10x without raising alerts. Recovery must restart the CUDA provider or fail fast.

---

## 6. Sprint 17 Horizon: next tasks

1. Commuter corridor travel duration estimation and congestion delay calculation — `services/go-server/internal/corridor`
2. Turn-by-turn navigation hazard intersection alerts along corridor polylines — `apps/web/src/components/NavigationAlerts.tsx`
3. Web Push API subscription manager and browser notification worker (VAPID) — `apps/web/src/service-worker.ts` & `services/go-server/internal/push`
4. Historical flood depth trend charts & tidal inundation correlation dashboard — `apps/web/src/components/CorridorTrends.tsx`
5. Automated crowd-sourced hazard report verification against nearby camera feeds — `services/go-server/internal/reports`
6. INT8 post-training quantization on ONNX models for GPU memory footprint reduction — `services/cv-engine/server.py`
7. Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs — `services/go-server/internal/metrics`
8. Production CORS and trusted proxy CIDR configuration hardening (SEC-04 & SEC-05) — `services/go-server/cmd/server/routes.go`

---

## 7. Open Questions For The Human

- **Corridor Travel Duration**: Should travel duration calculations rely on static distance + heuristic speed bands from camera density, or ingest external traffic duration telemetry?
- **Web Push Priority**: Should Web Push notifications take precedence over expanding the Telegram bot with interactive corridor navigation menus?
