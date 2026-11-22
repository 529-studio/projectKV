# S17 Handoff & Sprint 18 Horizon

**Document**: `docs/HANDOFF-s17.md`  
**Date**: 2026-11-22  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 17 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `feat/515-sprint17-e2e-proofs-and-handoff`
- **Head commit**: `1a8567e` (`Merge pull request #521 from 529-studio/feat/514-mobile-kiosk-ergonomics`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 17**:
  - PR #516 (`feat/510-vehicle-clearance-profiles`, Issue #510)
  - PR #517 (`feat/511-hourly-tidal-rain-scrubber`, Issue #511)
  - PR #518 (`feat/512-my-route-journey-glance`, Issue #512)
  - PR #519 (`chore/camera-catalog-resync-20261121-42`)
  - PR #520 (`feat/513-high-speed-submersion-labeler`, Issue #513)
  - PR #521 (`feat/514-mobile-kiosk-ergonomics`, Issue #514)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :-------- | :------------------- |
| **#516** | **S17-T1** (#510) | `corridor`, `web` | **Vehicle Clearance Profiles**: 4 vehicle profiles (Scooter, Motorbike, Sedan, SUV) with adaptive hazard passability thresholds. |
| **#517** | **S17-T2** (#511) | `tide`, `forecast` | **Hourly Tidal Harmonic Engine**: 7-constituent tide model for Phú An & Nhà Bè with interactive hourly scrubber bar. |
| **#518** | **S17-T3** (#512) | `web`, `commuter` | **My-Route Personal Glance**: 3-second journey evaluation, local-first storage (`localStorage`), and automatic detour advice. |
| **#519** | **S17-T4** | `data`, `cameras` | **Camera Catalog Resync**: Daily catalog synchronizations and health audit alignment. |
| **#520** | **S17-T5** (#513) | `cv`, `labeler` | **20x Rapid Keyboard Labeler**: Hotkeys 1-5 for physical wheel submersion classification replacing manual 2D polygons. |
| **#521** | **S17-T6** (#514) | `web`, `ergonomics` | **Mobile Kiosk Ergonomics**: Enforced minimum 48px touch targets and `env(safe-area-inset)` offsets across mobile sheet and modals. |

---

## 3. Open Issues & Backlog

- **All Sprint 17 feature issues merged**: Issues #510, #511, #512, #513, #514 are 100% merged into `dev`.
- **Sprint 18 Backlog Candidates**:
  - Turn-by-turn navigation hazard intersection alerts along corridor polylines.
  - Web Push API subscription manager and service worker notifications (VAPID).
  - Historical flood depth trend charts & tidal inundation correlation dashboard.
  - Automated crowd-sourced hazard report verification against nearby camera feeds.
  - INT8 quantization for ONNX models to optimize GPU memory footprint.
  - Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs.
  - SEC-04 & SEC-05 hardening: disallow localhost origin fallback in production and configure trusted proxy CIDRs.

---

## 4. Settled Architectural Decisions

- **Adaptive Clearance over Static Severity**: Flood alerts must adapt to vehicle clearance; a 22 cm flood is impassable for scooters but passable for SUVs.
- **Pure Astronomical Harmonic Modeling**: 7-constituent harmonic tide modeling delivers zero-latency offline tidal projections without external paid APIs.
- **Local-First Privacy for Citizen Commutes**: Commuter route configuration is strictly client-side (`localStorage`), guaranteeing zero personal location telemetry.
- **Wheel Submersion Calibration Ladder**: Ground truth labeling permanently relies on wheel submersion classification rather than slow 2D puddle area tracing.
- **Kiosk 48px Minimum Hitbox**: All mobile touch controls must satisfy $\ge 48\text{px} \times 48\text{px}$ to prevent mis-taps in outdoor conditions.

---

## 5. Pitfalls Encountered

- **Strict Mode Selector Ambiguities**: Duplicate elements across desktop toolbars and mobile drawer headers trigger Playwright strict mode errors. Qualify with `.md:hidden` or unique accessible titles.
- **Stacking Context Interceptions**: Map container `z-0` trapped child controls behind bottom docks. Keep parent container flow transparent and hoist popups to `z-[1200]`.
- **ESLint `Unexpected any`**: Test mocks must type-cast explicitly via `as unknown as Type` rather than `as any` to satisfy strict CI linters.
- **Documentation Budget Tightness**: `docs/lessons/INDEX.md` cap (20,480 B) requires periodic pruning of verbose guardrails as new lesson rows are appended.

---

## 6. Sprint 18 Horizon: next tasks

1. Turn-by-turn navigation hazard intersection alerts along corridor polylines — `apps/web/src/components/NavigationAlerts.tsx`
2. Web Push API subscription manager and service worker notifications (VAPID) — `apps/web/src/service-worker.ts` & `services/go-server/internal/push`
3. Historical flood depth trend charts & tidal inundation correlation dashboard — `apps/web/src/components/CorridorTrends.tsx`
4. Automated crowd-sourced hazard report verification against nearby camera feeds — `services/go-server/internal/reports`
5. INT8 post-training quantization on ONNX models for GPU memory footprint reduction — `services/cv-engine/server.py`
6. Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs — `services/go-server/internal/metrics`
7. Production CORS and trusted proxy CIDR configuration hardening (SEC-04 & SEC-05) — `services/go-server/cmd/server/routes.go`
8. Camera catalog automated daily synchronizations and offline feed culling — `tools/eval/cull_dead_cameras.py`

---

## 7. Open Questions For The Human

- **Web Push vs. Telegram Bot Priority**: Should Web Push browser notifications take priority in Sprint 18, or should we expand Telegram bot capabilities with interactive corridor navigation menus?
- **Navigation Alerts Granularity**: Should turn-by-turn hazard alerts emit sound/haptic cues via the Web Audio API when approaching a flooded intersection?
