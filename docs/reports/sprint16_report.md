# Sprint 16 Completion, Technical Verification & Reality-Check Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0  
**Date:** 2026-11-20  
**Commit Range:** `713088f..f135a9e` (10 merged PRs, 48 files, +3,478 / -2,190 lines)

**Primary References & Ground Truth:**
- [`docs/HANDOFF-s15.md`](../HANDOFF-s15.md) and [`docs/HANDOFF-s16.md`](../HANDOFF-s16.md)
- Engineering Lessons: [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lesson 114 / Issue #494)
- GitHub PR Ledger: PRs #491, #493, #494, #495, #496, #497, #498, #499, #500, #501, #502

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Delivered Capabilities & Audit Remediations](#2-delivered-capabilities--audit-remediations)
3. [Field Hardening, Infrastructure & DR Optimizations](#3-field-hardening-infrastructure--dr-optimizations)
4. [Empirical Verification Evidence & Production Proofs](#4-empirical-verification-evidence--production-proofs)
5. [Engineering Lessons Learned (L114)](#5-engineering-lessons-learned-l114)
6. [Settled Architecture Decisions & Strategic Bridge to Sprint 17](#6-settled-architecture-decisions--strategic-bridge-to-sprint-17)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 16 focused on critical security audit remediations, high-concurrency API performance,
mobile and desktop overlay layout integrity, GPU inference acceleration via ONNX Runtime CUDA,
disaster recovery storage optimization, and operational log hygiene.

Key achievements include resolving security audit findings SEC-01 through SEC-03 (production Swagger UI
gating and unified Nginx/Go HTTP security headers), eliminating client-side N+1 API query bottlenecks
via batch road status summaries, stabilizing the corridor card and bottom dock overlay stack into
a shared bottom-anchored flow column with zero overlap, introducing an ONNX Runtime CUDA backend
for CV Engine with resilient session self-healing, tightening Litestream WAL replication retention
to 48 hours to preserve R2 free-tier headroom, and reducing harvester noise while supporting configurable
snapshot fetch budgets.

### Quantitative Sprint Scorecard

| Target Dimension | Baseline (Sprint 15 End) | Delivered (Sprint 16 End) | Status |
| :--- | :--- | :--- | :--- |
| **API Attack Surface** | Publicly accessible Swagger UI & OpenAPI in production | Gated Swagger UI behind `APP_ENV != "production"` (SEC-01) | **Delivered** |
| **HTTP Security Headers** | Missing HSTS/CSP; Nginx `add_header` masking on static assets | Consolidated `nginx_security_headers.conf` & Go middleware (SEC-02, SEC-03) | **Delivered** |
| **Road Status Traffic** | N+1 individual HTTP requests on filter changes (HTTP 429 risk) | Batched `GET /api/roads/status-summary` in single HTTP round-trip | **Delivered** |
| **Overlay Stack Layout** | Card occluded by dock (187px overlap); 36px mobile overflow | Shared in-flow column; 16px/12px clean gap; zero overflow (Issue #494) | **Delivered** |
| **Corridor Route Geometry** | Quốc lộ 1 polyline rendered with zig-zag artifacts | Coordinate sequence reordered to proper directional linestring | **Delivered** |
| **CV Inference Runtime** | Pure PyTorch / OpenVINO CPU runtime | ONNX Runtime CUDA backend with self-healing dead sessions (PR #497, #498) | **Delivered** |
| **DR Cloud Storage** | 7-day WAL retention consuming ~7.08 GiB in Cloudflare R2 | Tightened to 48h retention; keeps 2-day recovery window with ~3 GB headroom | **Delivered** |
| **Telemetry & Log Noise** | Absent forecast model logged as error; fixed 10s fetch timeout | Startup missing artifact logged at INFO; configurable `SNAPSHOT_TIMEOUT` | **Delivered** |

---

## 2. Delivered Capabilities & Audit Remediations

### S16-T1: Security Audit Remediations & Swagger Gating (PR #493 / SEC-01, SEC-02, SEC-03)
- Gated `/swagger/*` route mounting behind `cfg.AppEnv != "production"` in `services/go-server/cmd/server/routes.go`.
- Added Go HTTP security header middleware (`services/go-server/internal/middleware/headers.go`) setting `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, and `Permissions-Policy`.
- Created `apps/web/nginx_security_headers.conf` and included it in root and static location blocks (`apps/web/nginx.conf`, `apps/web/Dockerfile`) to prevent Nginx header masking.

### S16-T2: Batch Road Status Summary & N+1 Elimination (PR #493)
- Implemented `GET /api/roads/status-summary` endpoint in `services/go-server/internal/corridor`.
- Refactored frontend road filtering (`apps/web/src/components/PhuongTreeFilter.tsx`, `useCorridor.ts`, `corridorUtils.ts`) to fetch all statuses in one batch query, preventing rate-limit drops.
- Reordered coordinate points for Quốc lộ 1 arterial corridor to eliminate polyline zig-zags.

### S16-T3: Corridor Overview Card & Bottom Dock Overlay Stack (PR #496 / Issue #494)
- Re-architected bottom overlay stack in `apps/web/src/app/page.tsx` and `PinnedDock.tsx` into a shared bottom-anchored flow column.
- Removed magic offsets (`bottom: calc(var(--safe-bottom) + 64px)`); the card and dock participate in normal flow with responsive widths (`w-full sm:w-[380px]`).
- Authored Lesson 494 and captured visual proof screenshots in `docs/proofs/494-corridor-card-overlay/`.

### S16-T4: CV Engine ONNX Runtime CUDA Backend & Session Recovery (PR #497, PR #498)
- Added ONNX Runtime CUDA execution provider in `services/cv-engine/server.py` and `requirements-ort.txt`.
- Built session recovery: rebuilds dead CUDA sessions automatically, fails fast if model weights are absent, and refuses silent degradation to CPU when CUDA is explicitly required.
- Verified test suite `services/cv-engine/test_server.py`.

---

## 3. Field Hardening, Infrastructure & DR Optimizations

1. **Litestream WAL Retention Pruning (PR #499, PR #500)**: Reduced SQLite WAL replica retention from 7 days to 48 hours in `deploy/litestream/litestream.yml` and `docker-compose.stg.yml`. Measured churn is ~1.2 GiB/day; 48 hours provides a reliable 2-day point-in-time recovery window while staying safely below the 10 GiB Cloudflare R2 free tier limit.
2. **Forecast Model Reloader & Snapshot Timeout Config (PR #501)**: Handled missing forecast model artifacts gracefully at startup (logged at `INFO` rather than alarmist `ERROR`), and introduced configurable `SNAPSHOT_TIMEOUT` (default 15s) in harvester configuration.
3. **Camera Catalog Audits (PR #491, PR #494, PR #502)**: Executed daily camera catalog resynchronizations and dropped dead unreferenced capture artifacts.

---

## 4. Empirical Verification Evidence & Production Proofs

- **Overlay Stack Visual Proofs**: Verified on desktop (1280x800 strip & grid) and mobile (390x844, 360x640) under `docs/proofs/494-corridor-card-overlay/`. Confirmed zero overlap and 16px/12px gap.
- **N+1 API Verification**: Batch `/api/roads/status-summary` verified returning aggregated statuses in a single round-trip.
- **Security Headers & Swagger Gating**: Verified Swagger UI returns 404 in production; HTTP response headers confirm presence of HSTS, CSP, and `nosniff`.
- **Litestream Retention Verification**: Confirmed retention window settings across manifests and runbooks.
- **Automated Test Suites**:
  - Go backend tests: passed 100%.
  - CV engine ORT tests: passed 100%.
  - Web lint and type checks: 0 warnings.
  - Docs lint: 0 new violations.

---

## 5. Engineering Lessons Learned (L114)

- **L114 (Issue #494)**: Overlay Stacks Need Shared Flow, Not Magic Offsets (`docs/lessons/494-corridor-card-overlay-stack.md`).
  An overlay only reserves space if it is in normal flow. Absolute and fixed children are invisible to a parent flex container. Wrapping floating panels in a shared bottom-anchored flow column ensures natural gap separation and responsive widths without brittle pixel constants.

---

## 6. Settled Architecture Decisions & Strategic Bridge to Sprint 17

1. **Production Surface Gating (SEC-01)**: Development tools and interactive contract inspectors (Swagger/OpenAPI) must be compiled or gated out of production runtimes.
2. **Nginx Header Consolidation (SEC-03)**: Never rely on root `add_header` inheritance when child location blocks set headers; always include a shared security headers snippet in every location block.
3. **Shared Flow Overlay Layouts**: Floating panels anchored near each other must participate in normal layout flow rather than computing ad-hoc pixel offsets based on sibling components.
4. **Litestream Retention Budget**: 48-hour WAL retention balances point-in-time disaster recovery capability with strict zero-cost R2 cloud storage budgets.
