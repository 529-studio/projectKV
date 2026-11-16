# S14 Handoff & Sprint 15 Horizon

**Document**: `docs/HANDOFF-s14.md`  
**Date**: 2026-11-16  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 14 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s14-handoff`
- **Head commit**: `3249eab` (`ci: restore the GitHub Actions deploy job (rollback host-driven deploy) (#473)`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 14**:
  - PR #446 (`feat/s14-t1-inference-health-and-coverage`, Issue #441)
  - PR #447 (`feat/s14-t2-litestream-replication-and-restore-drill`, Issue #442)
  - PR #448 (`feat/s14-t3-commuter-map-clustering-and-hazard-depth`, Issue #443)
  - PR #449 (`fix/staging-deploy-integrity-and-r2-public-url`)
  - PR #450 (`fix/cv-input-resolution-default`)
  - PR #451, #458, #467 (`chore/camera-catalog-resync`)
  - PR #455 (`feat/s14-macro-clustering-and-smart-lens`, Issue #454)
  - PR #457 (`fix/456-svg-forecast-dot-transform-origin-and-hit-targets`, Issue #456)
  - PR #459 (`feat/s14-t4-forecast-skill-gating-and-model-promotion`, Issue #444)
  - PR #461 (`fix/460-macro-cluster-zoomend-reactive-trigger`, Issue #460)
  - PR #462 (`fix/deploy-concurrency-and-test-ports`)
  - PR #464 (`feat/463-setup-lore-memory-opencode-agy`, Issue #463)
  - PR #466 (`fix/465-radar-virga-suppression-traffic-label`, Issue #465)
  - PR #468–#473 (Staging deployment, Litestream compose contract, immutable tags, Portainer over tailnet, deploy runner rollback)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :-------- | :------------------- |
| **#446** | **S14-T1** (#441) | `backend`, `cvc` | **Inference Health Observability**: Isolated 404/5xx feeds from breaker; exported 24h health metrics and `/api/health/coverage`. |
| **#447** | **S14-T2** (#442) | `infra`, `ops` | **Litestream R2 Replication & Drill**: Continuous WAL replication to Cloudflare R2; automated SQLite restore drill script. |
| **#448** | **S14-T3** (#443) | `web`, `map` | **Commuter Map & Ubiquitous Depth**: Dark Matter basemap; hazard clustering; worst-severity bubble-up; physical cm depth display. |
| **#459** | **S14-T4** (#444) | `forecast`, `cv` | **Weather Feature Parity & Skill Gating**: Go-Python feature store parity; mandatory >=5% skill gate over baseline; atomic model swap. |
| **#455** | Issue #454 | `web`, `ux` | **Adaptive Macro Clustering & Smart Lens**: Aggregated macro clusters (zoom 10–12); context-aware lens defaults; eliminated dock clutter. |
| **#457** | Issue #456 | `web`, `ui` | **SVG Geometry Anchor Fix**: Anchored SVG forecast dot transform origin; expanded hit target to eliminate hover jitter. |
| **#461** | Issue #460 | `web`, `map` | **Macro Cluster Zoom Lifecycle**: Rebuild clusters on `zoomend`; leap zoom +2 on macro cluster click to prevent freeze. |
| **#464** | Issue #463 | `tooling`, `lore` | **Lore Verifiable Persistent Memory**: Ingested git commit ontology into Lore; configured OpenCode & Antigravity MCP memory. |
| **#466** | Issue #465 | `web`, `radar` | **Doppler Radar Virga Gating & Badge**: Protected convective cores (>=30 dBZ) from NWP lag; disambiguated traffic label to `THÔNG THOÁNG`. |
| **#468-#473** | Ops/CI | `deploy`, `ci` | **Deployment Hardening**: Pinned immutable tags, moved non-secret config to compose, connected Portainer over Tailscale. |

---

## 3. Open Issues & Backlog

- **All Sprint 14 issues closed**: Issues #441, #442, #443, #444, #452, #454, #456, #460, #463, #465 are 100% merged and verified.
- **Sprint 15 Backlog Candidates**:
  - Commuter corridor trip duration and cumulative flood hazard path calculation.
  - Push notification infrastructure (Web Push API / Telegram subscriber alerts).
  - Adaptive CV input resolution scaling under heavy CPU load.
  - Automated weekly Litestream restore drill cron job reporting to Telegram/Slack.

---

## 4. Settled Architectural Decisions

- **ADR-014 (Litestream Replication Contract)**: SQLite database files must be streamed to Cloudflare R2 via Litestream WAL replication; automated restore drills are mandatory for backup qualification.
- **ADR-015 (Two-Tier Adaptive Clustering)**: Macro clustering at zoom 10–12 and street-level clustering at zoom >= 13 preserve sub-16ms Leaflet frame budgets across 837 cameras.
- **Rule 13 (Persistent Memory Protocol)**: Lessons and architectural decisions are indexed in Lore ontology for cross-session retrieval.
- **Lessons 97–106**: Detailed in `docs/lessons/INDEX.md`.

---

## 5. Pitfalls Encountered

- **Leaflet Macro Cluster Click Stall**: Clicking macro clusters zoomed into an intermediate level without splitting, leaving users stranded. Solved by leaping +2 zoom and hooking split logic to `zoomend`.
- **False Radar Virga Blanking**: Convective cloud cores were suppressed because NWP weather models lagged behind radar reflectivity. Solved by granting >=30 dBZ cores immunity from virga suppression.
- **Portainer 409 Collision**: Multiple CI jobs attempting stack deployment conflicted on Portainer lock. Solved by implementing exponential backoff retries and tailnet routing.

---

## 6. Sprint 15 Horizon: next tasks

1. Commuter corridor trip duration and cumulative hazard calculation — `services/go-server/internal/routing`
2. Web Push API subscription manager and browser notification worker — `apps/web/src/service-worker.ts`
3. Background notification dispatcher for arterial corridor alerts — `services/go-server/internal/notify`
4. Dynamic CV resolution downscaling under edge CPU load — `services/cv-engine/inference.py`
5. Weekly automated Litestream restore drill cron with webhook alerting — `tools/ops/litestream_restore_drill.sh`
6. Route polyline rendering on Leaflet map with depth-coded segments — `apps/web/src/components/RouteLayer.tsx`
7. Edge inference worker 24h continuous soak test and memory profiling — `deploy/benchmark`

---

## 7. Open Questions For The Human

- **Commuter Notification Channel**: Should Sprint 15 prioritize browser Web Push notifications via VAPID or direct Telegram bot broadcast channels for traffic alerts?
- **Corridor Routing Engine**: Should arterial route calculation be solved client-side using Leaflet Routing Machine or server-side via Go graph traversal?
