# Sprint 19 Completion Report — ProjectKV

**Period**: Sprint 19 (`6195fc6` -> `0fc6fa4` on `dev`)
**Date**: 2026-11-25
**Issues Closed**: `#549`, `#550`, `#551`, `#552`, `#553`, `#554`, `#555`, `#556`
**Pull Requests Merged**: `#557`, `#558`, `#559`, `#560`, `#561`, `#562`, `#563`, `#564`, `#565`

---

## 1. Executive Summary

Sprint 19 converted Sprint 18's foundational alert infrastructure, INT8 inference artifacts, OpenMetrics exporter, and SQLite backup tooling into an interactive commuter-ready system validated end-to-end against live browser rendering and high-concurrency load:

1. **Real-Time State-Transition Push & Commuter Alert Controls (`#549`, `#550`, `#565`)**: Replaced static time-window alert suppression with thread-safe hazard state tracking (`StateTracker`) that immediately bypasses debounce when depth or corridor passability escalates, wired arterial corridor dispatch into runtime verdict ingestion, and shipped a monochromatic, bilingual Web Push drawer (`NotificationDrawer`) + prompt banner (`NotificationPromptBanner`) supporting vehicle clearance thresholds and rush-hour filtering. Live Playwright inspection additionally caught and resolved z-index stacking and card-height overflow issues (`#565`).
2. **Hydraulic Sluice Gate Visibility (`#551`)**: Surfaced Ho Chi Minh City tidal sluice gate and pumping states (`CLOSED_PUMPING` at >= 1.40 m Phu An / Nha Be river stage vs `OPEN_GRAVITY`) directly on `/api/corridors/overview` and `CorridorOverviewCard`.
3. **Production Operations, Edge Scaling, Stress & DR Verification (`#552`–`#556`)**: Automated daily dead-camera culling via GitHub Actions, added CPU-only INT8 edge Compose & pre-flight entropy/runtime verification (`deploy_edge_int8.py`), provisioned Prometheus-aligned Grafana templates, eliminated TCP SYN queue drops under 60-camera bursts (`CVServer.request_queue_size = 128`, 300/300 OK at `P95=57.53ms`), and automated non-destructive SQLite WAL `VACUUM INTO` failover drills (`failover_drill.sh`).

---

## 2. Delivered Pull Requests

| Task | Issue | PR | Commit | Scope |
|---|---|---|---|---|
| S19-T1 | `#549` | `#557` | `3c6e098` | `StateTracker` transition evaluation, escalation debounce bypass, corridor push dispatch wiring |
| S19-T2 | `#550` | `#558` | `526db8e` | `NotificationDrawer`, `NotificationPromptBanner`, bilingual catalog, `kv_notification_preferences_v1` |
| S19-T3 | `#551` | `#559` | `222a742` | `TidalGate` backend evaluation, Swagger update, `CorridorOverviewCard` gate badges |
| S19-T4 | `#552` | `#560` | `6668e0e` | `.github/workflows/camera-catalog-sync.yml` (02:00 UTC catalog sync PR bot) |
| S19-T5 | `#553` | `#561` | `ed53605` | `docker-compose.cv-edge.yml`, `tools/cv/deploy_edge_int8.py` artifact & ORT verification |
| S19-T6 | `#554` | `#562` | `45039a5` | `docs/ops/grafana/projectkv-operations-dashboard.json`, PromQL metric parity test |
| S19-T7 | `#555` | `#563` | `9c7950e` | `tools/eval/benchmark_int8_stress.py`, `CVServer.request_queue_size = 128` fix |
| S19-T8 | `#556` | `#564` | `8941c63` | `tools/dr/failover_drill.sh`, integration test, SQLite restore runbook update |
| UI Fix | — | `#565` | `0fc6fa4` | Elevated `NotificationDrawer` to `z-[980]`, constrained corridor/commute cards to `max-h-[34vh]` |

---

## 3. Empirical Verification Highlights

- **Frontend (`apps/web`)**: After final UI fix (`#565`), `npm test` passed `516/516` tests across `105` suites and `npm run build` completed cleanly with zero TypeScript or static export errors.
- **Live Browser UI Verification (Playwright)**: Inspected desktop `1280x720` against live `go-server` (`:3000`) and `next dev` (`:3001`), verified `/api/corridors/overview` serving real `TidalGate` telemetry (`Cống Bến Nghé`: `CLOSED_PUMPING`, `downstream_level_m: 1.72`, `pump_active: true`), caught/fixed drawer z-index clipping and card overlap, and confirmed zero console errors.
- **Backend & CV Stress (`services/go-server`, `services/cv-engine`)**: `go test -race ./...` passed on S19-T1 (`#557`) and `go test -race ./internal/corridor/...` passed on S19-T3 (`#559`); `services/cv-engine` passed `146/146` tests; 60-camera 300-request stress run returned `300/300 OK` (`0` errors, `P50=13.62ms`, `P95=57.53ms`) after increasing `CVServer.request_queue_size` from `5` to `128`.
- **CI, Docs, DR & Ops**: All 9 PRs (`#557`–`#565`) passed 100% of GitHub Actions checks (unit suites, CodeQL across Go/JS/Python/Actions, Docs Lint, Evidence Integrity) prior to squash-merge. `tools/dr/test_failover_drill.py` (`1/1`) and `failover_drill.sh` confirmed zero-loss `51/51` `verdicts` and `10/10` `push_subscriptions` recovery with `PRAGMA integrity_check=ok`.

---

## 4. Lessons Indexed

Sprint 19 contributed 8 self-contained lesson notes indexed in `docs/lessons/INDEX.md` as Lessons 130–137 (`549` through `556`).
