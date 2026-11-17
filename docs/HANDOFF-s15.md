# S15 Handoff & Sprint 16 Horizon

**Document**: `docs/HANDOFF-s15.md`  
**Date**: 2026-11-17  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 15 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s15-handoff`
- **Head commit**: `2e2fe13` (`Merge pull request #489 from 529-studio/feat/s15-t6-restore-drill-cron-webhook`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 15**:
  - PR #481 (`feat/s15-t3-mobile-touch-ergonomics-viewport`, Issue #477)
  - PR #482 (`feat/s15-t1-corridor-status-and-route-geojson`, Issue #475)
  - PR #483 (`docs: sync S15-T1 & S15-T3 lessons and documentation`)
  - PR #485 (`feat/s15-t7-address-search-sanitizer-house-number-normalizer`, Issue #484)
  - PR #486 (`feat/s15-t2-commuter-route-polyline-and-status-card`, Issue #476)
  - PR #487 (`feat/s15-t4-telegram-corridor-flooding-alerts`, Issue #478)
  - PR #488 (`feat/s15-t5-adaptive-cv-inference-load-shedding`, Issue #479)
  - PR #489 (`feat/s15-t6-restore-drill-cron-webhook`, Issue #480)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :-------- | :------------------- |
| **#482** | **S15-T1** (#475) | `backend`, `corridor` | **Arterial Corridor Hazards**: `GET /api/corridors` status aggregation and GeoJSON polylines. |
| **#486** | **S15-T2** (#476) | `web`, `map` | **Commuter Route Polylines**: Visual corridor polylines with hazard status drawer and focus. |
| **#481** | **S15-T3** (#477) | `web`, `ux` | **Mobile Touch Ergonomics**: Dynamic `100dvh` viewport, >=48px touch targets, smooth drawer snap. |
| **#487** | **S15-T4** (#478) | `backend`, `notify` | **Corridor Telegram Alerts**: Dedicated corridor flood subscriptions with 15m debouncing. |
| **#488** | **S15-T5** (#479) | `cv`, `engine` | **Adaptive Load Shedding**: Drop stale frames (>60s) with 429, dynamic 320px downscale, 24h soak verified. |
| **#489** | **S15-T6** (#480) | `ops`, `dr` | **Automated Restore Drill**: Weekly systemd cron (Sun 03:00 UTC) with universal webhook dispatcher. |
| **#485** | **S15-T7** (#484) | `web`, `search` | **Address Search Sanitizer**: Strips house numbers and alley prefixes ("34/55/67") from street queries. |

---

## 3. Open Issues & Backlog

- **All Sprint 15 issues closed**: Issues #475, #476, #477, #478, #479, #480, #484 are 100% merged and verified.
- **Sprint 16 Backlog Candidates**:
  - Commuter corridor travel duration estimation and real-time congestion delay calculation.
  - Turn-by-turn navigation hazard intersection alerts along corridor polylines.
  - Web Push API subscription manager and browser notification worker (VAPID).
  - Historical flood depth trend charts & tidal inundation correlation dashboard.
  - Automated crowd-sourced hazard report verification against nearby camera feeds.
  - Edge CV inference quantization (INT8 / ONNX runtime) for reduced memory footprint.
  - Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs.

---

## 4. Settled Architectural Decisions

- **AGENTS.md Rule 14 (CI & Security Scanning Gate)**: No PR is finished until CI is 100% green and GitHub Advanced Security reports zero open alerts or review comments.
- **Corridor Subscription Hierarchy**: Corridor-level alerts augment ward and camera subscriptions, allowing commuters to monitor entire daily commuting arteries without subscribing to dozens of individual cameras.
- **Fail-Closed Load Shedding**: CV Engine drops stale frames (>60s) rather than backlogging inference queues, preserving real-time accuracy during high-traffic storm surges.
- **Automated DR Verification Cadence**: Weekly Sunday 03:00 UTC cron restores prove Litestream replication durability continuously without human toil.
- **Lessons 107–113**: Indexed in `docs/lessons/INDEX.md`.

---

## 5. Pitfalls Encountered

- **CodeQL URL Sanitization**: Substring matching (`"api.telegram.org" in parsed.netloc.lower()`) allowed potential subdomain bypasses. Fixed with exact hostname check `(parsed.hostname or "").lower() == "api.telegram.org"`.
- **Mobile Viewport 100vh Occlusion**: Mobile Safari/Chrome dynamic toolbars cut off bottom navigation. Solved by standardizing on CSS `100dvh` and safe-area insets.
- **Virtualized CI Test Runner Starvation**: CV server concurrency tests experienced intermittent socket timeouts on 2-vCPU runners. Solved by catching all transport errors and adjusting join timeouts.

---

## 6. Sprint 16 Horizon: next tasks

1. Commuter corridor travel duration estimation and real-time delay calculation — `services/go-server/internal/corridor`
2. Turn-by-turn navigation hazard intersection alerts along corridor polylines — `apps/web/src/components/NavigationAlerts.tsx`
3. Web Push API subscription manager and browser notification worker (VAPID) — `apps/web/src/service-worker.ts` & `services/go-server/internal/push`
4. Historical flood depth trend charts & tidal inundation correlation dashboard — `apps/web/src/components/CorridorTrends.tsx`
5. Automated crowd-sourced hazard report verification against nearby camera feeds — `services/go-server/internal/reports`
6. Edge CV inference quantization (INT8 / ONNX runtime) for reduced memory footprint — `services/cv-engine/inference.py`
7. Prometheus / Grafana metrics exporter for corridor saturation, queue drops, and restore drill SLAs — `services/go-server/internal/metrics`

---

## 7. Open Questions For The Human

- **Travel Duration Telemetry**: Should corridor commute times be estimated via static road length + CV speed heuristics, or integrate external traffic telemetry as fallback?
- **Notification Channels**: Should Web Push notifications take priority over expanding Telegram bot commands (e.g. interactive corridor selection menus)?
