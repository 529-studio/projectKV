# ADR-001: Architecture Review & Flood-Alert Extension for CAMERA VIEW

**Status**: Proposed
**Date**: 2026-09-20
**Context**: Independent architecture review of the current `hcm-traffic-cameras` monorepo, plus feasibility assessment of extending the system to real-time flood detection & alerting for HCMC motorbike riders.

---

## 1. Summary of Current Architecture (as-is)

- **Frontend**: `apps/web` — vanilla JS + Leaflet, no framework, i18n (vi/en), chained hierarchical filter (Zone → Corridor → Intersection), multi-camera pin dock.
- **Backend**: `services/node-server` — Express.js, single process, in-memory snapshot proxy cache (10s TTL per camera), no database.
- **Data**: `cameras_all.json` — static catalog of 837 cameras. Live imagery is **polled snapshot JPEGs** from `giaothong.hochiminhcity.gov.vn`, refreshed every 10s — **not** an RTSP/WebRTC live video stream.
- **Caching**: 3-tier (browser 10s → backend in-memory buffer → CDN edge 15s). Guarantees at most 1 upstream fetch per camera per 10s regardless of concurrent viewers.
- **Planned CV Engine** (`services/cv-engine`, not yet implemented): Python worker analyzing a "Hot Set" (actively viewed/pinned cameras + ~30 fixed choke points) on a 30–45s fixed schedule, decoupled from user request volume. Pluggable adapter: Mock → Cloud Vision API (Gemini/OpenAI) → Self-hosted YOLO/ONNX.
- **Go migration**: explicitly deferred until traffic/connection volume justifies it (thousands of cameras nationwide / tens of thousands of concurrent connections).

## 2. Verdict on Current Architecture

**Appropriate for current scope.** Node.js + in-memory cache + static JSON is the right level of engineering for a snapshot-based camera browser serving hundreds of concurrent users. No premature database, no premature microservices, no premature Go migration — good adherence to YAGNI.

**Key clarification required**: "Low latency" in the current design refers to **API/cache delivery speed**, not video frame latency. The data source itself is snapshot-based (10–45s freshness ceiling), which is a source limitation, not an architecture flaw. This distinction matters directly for Section 3 below.

**Strongest design decision**: The Hot Set Pool + Pluggable Adapter pattern for the CV Engine. It correctly decouples compute cost from user traffic and allows shipping a working feature (via Cloud Vision API) before investing in a self-hosted model.

**Gaps to address before adding any alerting feature**:
- No persistence layer (SQLite/Redis currently "future"). Alerting requires dedupe/history state — no longer optional once built.
- Single VPS running both proxy and CV worker — acceptable for a "nice to have" congestion viewer, not for a safety-alerting feature.
- Single upstream data source dependency (`giaothong.hochiminhcity.gov.vn`) — a single point of failure exactly when load (and need) is highest, during storms.

## 3. Feasibility: Real-Time Flood Detection & Alert for HCMC Riders

**Verdict: Feasible, and well-suited to the existing Hot Set Pool design — but it is a meaningfully larger scope than "add a flood model," not a drop-in addition.**

### 3.1 Why it fits well
Flood water rises over minutes, not milliseconds — the existing 30–45s (or even 2–5 min) Hot Set inference cycle is already fast enough. This is one case where the snapshot-based data source is *not* the bottleneck.

### 3.2 Why the CV problem is harder than vehicle density
Flood detection is a scene-classification problem (dry / wet / shallow flood / deep flood), not a standard pretrained object-detection class. Wet asphalt reflections and shadows are easy to confuse with flooding.

**Recommended phased approach (maps directly onto the existing adapter pattern):**
1. **Phase 1 (MVP)**: Cheap pixel/histogram baseline-diff pre-filter per camera → only call a multimodal Cloud Vision API (Gemini Flash / GPT-4o-mini) for a structured flood-severity verdict when anomaly exceeds threshold. Reuses the existing Cloud Vision adapter tier.
2. **Phase 2**: Once a rainy season's worth of labeled data is collected, train a lightweight CPU classifier (MobileNet/EfficientNet-lite via ONNX Runtime) to remove Cloud API dependency and cost.

### 3.3 Data coverage limitation (not fixable by architecture)
Many known flood-prone alleys/streets have no public DOT camera. This is a data-source gap, not a code problem — flag transparently to users rather than implying full-city coverage.

### 3.4 The real missing piece: push notification, not more CV
Camera browsing is a *pull* model. Flood alerting must be *push* — reaching a person already on a motorbike, who cannot open a laptop or browser tab. This requires new infrastructure not present today:
- A subscription/state store (zone-based subscriptions, minimum viable — defer live GPS route-matching to a later phase).
- Deduplication/alert-state logic (avoid repeated alerts every cycle while a flood is ongoing).
- A **Notification Adapter**, mirroring the existing CV adapter pattern:

```
interface NotificationChannel {
  send(subscriberId, zoneId, verdict) -> DeliveryResult
}
├─ TelegramChannel   [Phase 1 — fastest to build, zero cost, zero approval friction]
├─ ZaloOAChannel     [Phase 2 — highest real-world reach among everyday motorbike riders in VN; requires OA registration + ZNS template approval, start early]
├─ WebPushChannel    [Parallel, low effort — bonus for existing web map users; unreliable on iOS]
└─ SmsChannel        [Later — broadcast-only, for severe events / potential city partnership; per-message cost]
```

**Channel recommendation**: Start with Telegram to validate the full pipeline (subscribe → detect → alert) cheaply and fast. In parallel, begin the Zalo OA approval process early since it is the channel that actually reaches the target audience (everyday riders, including non-technical and older users) — Telegram skews toward a tech-savvy subset of VN users. Do not rely on a single channel for a safety-relevant feature.

### 3.5 Complementary signals worth adding
- Rainfall / tide data (public) as an early-warning signal — detects risk *before* water is visibly on the road, unlike camera CV which is inherently reactive.
- User-submitted confirm/false-alarm reporting — mitigates camera coverage gaps, improves calibration, and reduces liability exposure from false positives/negatives in a safety-facing feature.

### 3.6 Competitive/regulatory context
HCMC already has UDI Maps (sensor/report-based flood alerts) and a disaster-monitoring platform reportedly targeted for September 2026. A camera-based visual-confirmation approach is a genuine differentiator, not a duplicate — but positioning should complement rather than compete, and a partnership angle with the city is worth exploring given this overlap.

## 4. Proposed Additive Architecture

```
[Existing — unchanged]
node-server (proxy/cache) ──▶ Hot Set Scheduler (extend list with known flood hotspots)
                                        │
                                        ▼
                          cv-engine/flood_worker.py
                          ├─ baseline diff (cheap pre-filter)
                          └─ Cloud Vision API call on anomaly
                                        │
                                        ▼
[New — required]          State store (SQLite/Redis)
                          - per-camera state, dedupe, zone subscriptions
                                        │
                                        ▼
[New — required]          Notification Service
                          - TelegramChannel (Phase 1)
                          - ZaloOAChannel (Phase 2)
                          - WebPushChannel (parallel)
                          - SmsChannel (later)
```

**No rewrite of the existing Node.js backend is required.** The above are additive services. Go migration criteria remain unchanged from the original design — revisit only if WebSocket/notification fan-out reaches tens of thousands of concurrent connections.

## 5. Recommended Phasing

1. Flood MVP: baseline-diff + Cloud Vision API on ~30–50 known hotspot cameras, Telegram bot alerting, zone-based subscription, manual confirm/false-alarm feedback button.
2. Begin Zalo OA registration in parallel (approval lead time is outside your control — start early).
3. Add rainfall/tide data as a leading indicator alongside camera CV.
4. Migrate to a self-trained lightweight classifier once labeled data from a rainy season is available.
5. Consider live GPS route-intersection alerting only after the zone-based model is validated and adopted.

## 6. Risks to Track

- Single upstream camera source outage risk during storms (highest-need moment).
- False positive/negative liability in a safety-facing feature — needs explicit confidence thresholds and disclaimers, not a silent beta rollout.
- Camera coverage gaps are a data limitation, not solvable by architecture.

---

# ADR-002: Backend Language Migration to Go

**Status**: Accepted
**Date**: 2026-09-20
**Context**: Decision to build the backend in Go going forward, superseding the "defer Go until traffic justifies it" stance in the original `SYSTEM_DESIGN.md`.

## Decision

Adopt Go for all **new** backend components (Hot Set Scheduler, State Store, Notification Service, WebSocket fan-out). Migrate the existing `node-server` proxy/cache to Go **after** the new components are stable, not as a big-bang rewrite. Keep the CV model-inference layer in Python.

## Rationale

This is not a traffic-driven migration (the original trigger — thousands of cameras / tens of thousands of concurrent connections — has not been reached). The justification is different: the flood-alert extension requires building several components that do not exist yet (scheduler, state store, notification fan-out). Writing new code in Go carries far less risk than rewriting an already-working `server.js`, and these components — bounded worker pools, ticker-based scheduling, concurrent fan-out to subscribers — are a strong idiomatic fit for goroutines and channels.

The existing `node-server` proxy/cache is low-risk to port later: it is a small, well-isolated reverse-proxy-with-TTL-cache, not deeply coupled to anything else.

## What stays in Python

Model inference (self-hosted YOLO/ONNX, OpenCV-based video/image processing) remains a separate Python service (`cv-engine`). Go's CV/ML ecosystem (`gocv` via cgo, limited ONNX tooling) is significantly less mature than Python's, and `gocv` requires a native OpenCV system dependency that conflicts with the project's "clone and run, zero config" static-binary philosophy. Cheap pre-filtering (pixel/histogram baseline diff) and Cloud Vision API calls, however, are simple enough to implement in pure Go (`image` stdlib, plain HTTP) and stay in the Go control plane to avoid an extra network hop per camera per cycle.

## Consequences

- Two runtimes in the stack (Go control plane + Python ML plane) — accepted as a deliberate polyglot split, not incidental sprawl.
- Deployment becomes two binaries/processes: a single static Go binary + a Python service, communicating over internal HTTP.
- `node-server` remains in place until the Go proxy/cache is verified equivalent, then is retired.
- Library choices for the Go side favor the stdlib and minimal dependencies (`net/http`, `modernc.org/sqlite` — pure Go, no cgo, `nhooyr.io/websocket` or `gorilla/websocket`) to preserve the "zero external dependency, single static binary" deployment story on low-cost VPS.

---

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