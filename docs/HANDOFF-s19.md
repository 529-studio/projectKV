# ProjectKV - Sprint 19 Handoff

Branch: `dev` at commit `0fc6fa4`
Date: `2026-11-25`
Report: `docs/reports/sprint19_report.md`

---

## 1. Where we are

Sprint 19 completed end-to-end alert activation, commuter push preferences, hydraulic sluice gate visibility, edge INT8 deployment tooling, Grafana dashboards, 60-camera concurrency stress hardening, and automated SQLite DR drills across PRs `#557`–`#565`. Live UI verification against running Go + Next.js services caught and resolved HUD stacking/overflow defects before sprint close. `dev` is green across CI, CodeQL, Docs Lint, and Evidence Integrity.

## 2. What sprint 19 shipped

| Issue | PR | Summary | Key Paths |
|---|---|---|---|
| `#549` | `#557` | Hazard state transitions, escalation debounce bypass, corridor push dispatch | `services/go-server/internal/notify/{transition.go,manager.go}` |
| `#550` | `#558` | Bilingual Web Push prompt banner & slide-over preferences drawer | `apps/web/src/components/{NotificationDrawer,NotificationPromptBanner}.tsx` |
| `#551` | `#559` | Tidal sluice gate status (`CLOSED_PUMPING`/`OPEN_GRAVITY`) & card badges | `services/go-server/internal/corridor/{corridor.go,routes.go}`, `apps/web/src/components/CorridorOverviewCard.tsx` |
| `#552` | `#560` | Daily `02:00 UTC` camera catalog sync & corridor-floor PR workflow | `.github/workflows/camera-catalog-sync.yml` |
| `#553` | `#561` | CPU-only edge INT8 Compose stack & entropy/ORT pre-flight verifier | `docker-compose.cv-edge.yml`, `tools/cv/deploy_edge_int8.py` |
| `#554` | `#562` | Prometheus-aligned Grafana dashboard template & metric parity test | `docs/ops/grafana/projectkv-operations-dashboard.json`, `tools/ops/test_grafana_dashboards.py` |
| `#555` | `#563` | 60-camera concurrent HTTP stress tool & `CVServer.request_queue_size = 128` | `tools/eval/benchmark_int8_stress.py`, `services/cv-engine/server.py` |
| `#556` | `#564` | Non-destructive SQLite WAL `VACUUM INTO` failover drill & test | `tools/dr/failover_drill.sh`, `tools/dr/test_failover_drill.py` |
| — | `#565` | Live UI fix: `NotificationDrawer` `z-[980]`, `max-h-[34vh]` scrollable cards | `apps/web/src/components/NotificationDrawer.tsx`, `apps/web/src/app/page.tsx` |

## 3. Decisions that must not be re-opened

- **Transition-driven alerts (`notify.StateTracker`)**: Escalations (`CLEAR` -> `FLOODED_*` -> `FLOODED_DEEP`, `PASSABLE` -> `CAUTION`/`IMPASSABLE`) bypass debounce immediately and refresh the quiet window; recoveries update state silently without alert spam. Do not revert to blind sliding-window suppression.
- **Zero-identity push preferences**: Commuter corridor/vehicle/rush-hour settings stay client-local in `kv_notification_preferences_v1` with `z-[980]` drawer stacking above `z-[800]` map HUD cards and >=48px touch targets.
- **Tidal gate threshold**: Mapped HCMC sluice gates evaluate `CLOSED_PUMPING` (`pump_active=true`) at >= 1.40 m Phu An / Nha Be river stage and `OPEN_GRAVITY` below 1.40 m.
- **CV server socket backlog**: `services/cv-engine/server.py` keeps `CVServer.request_queue_size = 128`; Python's default `5` drops concurrent connections under 60-camera bursts.
- **Context budget management**: `docs/lessons/INDEX.md` uses compact pipe formatting (`|col|`) to stay within its 20,480 B always-loaded ceiling (`total <= 36,864 B`). Do not pad table pipes with spaces.

## 4. Open bugs, technical debt, and CI watchpoints

- **`docs/lessons/INDEX.md` budget headroom**: Even with compact pipes, `INDEX.md` is near the 20,480 B cap; future sprints must tighten older row summaries or raise architectural archival policy rather than breaching `docs_lint.py`.
- **Pre-existing Next.js build warnings**: 5 `@next/next/no-img-element` warnings and 1 unused `_url` warning in `src/contexts/e2e_scenarios.test.ts` remain non-blocking.
- **Live Web Push E2E delivery**: Real browser Service Worker push delivery requires valid `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` in `.env` over HTTPS or `localhost`.
- **Mobile viewport regression check**: Live Playwright check in S19 verified `1280x720` desktop; also verify `390x844` mobile layout when adding Sprint 20 HUD features.

## 5. Horizon - Sprint 20

Prioritize high-value commuter usability improvements + staged rollout operations:

1. **P0 — Mobile (`390x844`) & Live Service Worker Web Push Verification**: End-to-end browser push opt-in, permission denial recovery state, and `390x844` responsive Playwright verification across `NotificationDrawer`, `MyRouteCard`, and `CorridorOverviewCard`.
2. **P0 — Multi-Point Detour & Alternative Dry-Route Suggestions (`MyRouteCard` / `corridor`)**: Suggest concrete dry parallel corridors when a commuter's saved route transitions to `CAUTION` or `IMPASSABLE`.
3. **P1 — Custom Multi-Segment Commute Builder (`origin -> waypoint -> destination`)**: Allow commuters to save 2–3 connected corridors (home -> bridge/intersection -> office) instead of a single road selection.
4. **P1 — Mobile Camera Snapshot Tap-to-Zoom & Time-Lapse Compare**: Pinch/zoom and before-flood vs current snapshot toggle inside `CameraModal.tsx` for quick water-level verification.
5. **P1 — Production/Staging Release Evidence Soak & Canary Promotion**: Run live Grafana + Prometheus scrape validation, CPU edge INT8 host soak (`tools/cv/deploy_edge_int8.py` + `benchmark_int8_stress.py`), and scheduled DR drill verification before release tag.

## 6. How to verify the starting point

Run at Sprint 20 start without reading archived files unless tasked:

1. `git checkout dev && git pull --ff-only origin dev`
2. `cd services/go-server && go test -race ./... && cd ../..`
3. `cd apps/web && npm test && cd ../..`
4. `python3 tools/docs/docs_lint.py`

## 7. Do not load unless the task requires it

- `docs/HANDOFF-s18.md`, `docs/reports/sprint18_report.md`, and `docs/lessons/1`-`129` unless investigating a specific historical regression.
- Full model artifacts, `docs/proofs/`, or archived soak payloads.
