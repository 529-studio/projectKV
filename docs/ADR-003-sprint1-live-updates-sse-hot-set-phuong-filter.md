# ADR-003: Sprint 1 Scope — Live Updates (SSE), Hot-Set-Scoped Congestion Colors, Phường-Based Filter

**Status**: Accepted
**Date**: 2026-09-27
**Supersedes**: the WebSocket decision for live push in ADR-002's consequences (`internal/ws` using `nhooyr.io/websocket`/`gorilla/websocket`).

## Context

Three corrections/refinements were made to the original design after reviewing the running staging build (`warded-stg.529studio.site`). These are scoped as **Sprint 1**, to be fully done (DoD met) before starting the route-check feature (tracked separately, needs its own ADR — see "Deferred" below).

## Decision 1 — Replace WebSocket with SSE for live push

`internal/ws` (WebSocket) is replaced by `internal/live` (Server-Sent Events). The traffic is one-directional (server → client); SSE runs over plain HTTP, the browser's native `EventSource` handles reconnect, and this removes a third-party dependency (`net/http` + `http.Flusher` is sufficient — no `nhooyr.io/websocket`/`gorilla/websocket` needed).

**Scope correction**: the primary target is **map-wide marker color updates for the whole Hot Set**, not just refreshing a single open camera modal. One shared SSE stream (`GET /api/live/stream`) broadcasts `camera_update` events for every Hot Set camera as `hotset.Scheduler`'s `OnVerdict` callback fires; the frontend subscribes once regardless of how many cameras are open/pinned.

**Explicit non-goal**: SSE does not reduce the underlying data freshness ceiling — updates still only arrive as often as the Hot Set Scheduler's interval (30–45s) and the upstream gov snapshot refresh allow. It only removes wasted polling and enables one-connection fan-out to many markers.

Update to `GO_BACKEND_ARCHITECTURE.md`: §1 library table — remove the WebSocket row, add SSE (stdlib only). §3 — rename `internal/ws` to `internal/live`. §4 API contract — `GET /ws/live` becomes `GET /api/live/stream` (SSE, `Content-Type: text/event-stream`).

## Decision 2 — Congestion/flood colored markers are Hot-Set-scoped only, not all 837 cameras

Per `SYSTEM_DESIGN.md`'s own prior sizing (a single CPU VPS via ONNX handles 50–100 Hot Set cameras), colored severity markers (green/yellow/red) apply **only** to: the fixed critical-point list (~30–50 cameras) plus any camera currently viewed or pinned (promoted into the Hot Set via `Registry.Touch`). All other cameras keep the existing white (active)/gray (offline) status only, until a viewer promotes them.

This is not a compute limitation to work around — it is the intended scope. Do not attempt to color all 837 cameras continuously; that silently discards the Hot Set optimization. If Phase 1 uses the Cloud Vision API adapter (not self-hosted), home-hosted compute is not the constraint at all (it's an HTTP call); the Hot-Set-only scope still applies to control ongoing API cost and stay within the request budget of a home connection.

## Decision 3 — Hierarchical filter uses Phường (not Quận), and corrects tier order

As of 1 July 2025, Vietnam's administrative structure is legally 2-tier: Tỉnh/Thành phố → Xã/Phường/Đặc khu — Quận/Huyện was abolished (Nghị quyết 202/2025/QH15, Luật Tổ chức chính quyền địa phương 2025). The current `cameras_all.json` still carries pre-reform Quận/Huyện labels (e.g. "Q.12 - Hóc Môn - Củ Chi"), inherited from the crawl source.

Filter tier order is corrected to **Phường → Đường/Trục đường (corridor) → Camera** (a street has many cameras, not the reverse) — this is the existing "Zone → Corridor → Intersection" chained filter from `SYSTEM_DESIGN.md`, with the top tier's data source corrected from ad-hoc Quận text to a properly computed Phường boundary.

**Phường assignment is computed once, offline, not manually and not at request time**: a batch point-in-polygon join of each camera's lat/lng against post-reform Phường boundary polygons (source TBD — check GSO open data and OSM community boundary updates; not yet confirmed available as a ready download, verify before estimating this task's size), producing a `phuong_id`/`phuong_name` enrichment field cached alongside `cameras_all.json`.

## Deferred — Issue #2 (route-based congestion/flood check)

Not in this sprint. Known open problems to resolve in its own ADR before scheping: reuse an existing routing engine (OSRM on OSM data) instead of hand-rolled Dijkstra; camera coverage along arbitrary routes will be far sparser than the fixed hotspot list, requiring an explicit "verified vs. no-data" segment disclosure rather than a binary verdict; verdict generation must go through `Registry.Touch` + the existing Hot Set cycle, never synchronous per-request inference.

---

## Sprint 1 Backlog

Dependency order — do not start a later group before its dependencies are merged to `dev`.

| # | Title | Side | Depends on | Core DoD |
|---|---|---|---|---|
| A1 | `tools/geo-enrich`: batch Phường point-in-polygon enrichment | tooling | none | Produces `phuong_id`/`phuong_name` per camera; run offline, output cached, not computed at request time; script re-runnable when boundaries change |
| B1 | `internal/live`: SSE hub + endpoint | backend | assumes base Go skeleton (`hotset`, `store`, a `cvclient` Analyzer — Mock is fine) already exists per `GO_BACKEND_ARCHITECTURE.md` §9 | `GET /api/live/stream` emits `camera_update` per Hot Set verdict change; heartbeat every ~15s; connection cleanup on `r.Context().Done()`; no leak after N connect/disconnect cycles (test) |
| C1 | `internal/camera`: expose Phường field + `/api/zones` | backend | A1 | `Camera` struct carries `phuong_id`/`phuong_name` from enriched data; `/api/zones` (or new `/api/phuong`) returns distinct Phường list |
| D1 | `apps/web`: EventSource client, single shared subscription | frontend | B1 | One `EventSource` connection regardless of open/pinned camera count; reconnects natively on drop; no per-camera polling remains for Hot Set cameras |
| D2 | `apps/web`: drive marker color from real severity | frontend | D1, B1 (Mock analyzer output is enough to verify wiring before a real analyzer is chosen) | Hot Set cameras render green/yellow/red per verdict; all other cameras stay white/gray; legend (already in UI) matches actual behavior |
| D3 | `apps/web`: Phường → Đường → Camera tree filter | frontend | A1, C1 | Replaces flat camera list; three collapsible tiers in the stated order; no manual Phường mapping in frontend code (reads the enriched field only) |
| E1 | Sprint 1 integration check | cross-cutting | D2, D3 | Manual smoke test: map loads, colors update live with no page refresh, tree filter expands/filters correctly, non-Hot-Set cameras confirmed white/gray, no console errors |

Only after E1 passes: open the ADR for issue #2 (route-check).

---

*Extracted verbatim from [`docs/ADR-001-flood-alert-architecture.md`](ADR-001-flood-alert-architecture.md),
where this decision had been appended below ADR-001 as a third `# ADR-` heading. The body above
is byte-identical to that slice; only this note is new.*
