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

# ADR-004: Route-Check (#2) Decomposition, Snapshot Archive Strategy, and Sprint 2 Backlog

**Status**: Proposed
**Date**: 2026-09-28
**Related**: ADR-001 (flood alert), ADR-002 (Go), ADR-003 (Sprint 1), RFC #53 (self-hosted S3 storage)

## 1. Sprint 1 Outcome — Facts From the Data

Measured from `cameras_all.json` / `cameras_enriched.json` (837 cameras):

| Fact | Value | Consequence |
|---|---|---|
| Online / offline | 711 `UP`, 126 `NOT_IMAGE` (15%) | Offline cameras must never be matched or counted as coverage |
| Missing coordinates | 40 cameras (13 of them `UP`) | Cannot be placed on the map or matched to a route; report them, do not guess |
| Ward assignment | 37 assigned (31 Bến Nghé, 6 Tân Định), **800 unassigned** (760 with coordinates) | Ward filter is a demo, not a feature, until polygons cover the whole region |
| Coordinate bounding box | lat 10.50–11.01, lon 106.45–107.10; `BD`/`BR` title prefixes (13 each) | Data suggests the portal includes areas outside old HCMC; ward polygons must cover the merged region (inference — verify) |
| Name shape | 704/837 are intersection-form `A - B`; 301 distinct first segments; `Quốc lộ 1` (51) and `QL1` (18) are the same road; 6 duplicate names | Name-based road grouping/matching is unreliable; spatial matching must be primary |
| Fields available | id, title, displayName, status, isOnline, lat, lon, snapshotUrl | **No road id, no heading/direction** |
| i18n root cause | `phuong_name` stores `"Phường Bến Nghé"` (type prefix baked into data) | UI cannot localize; data model must store prefix-less names + type |

## 2. Decisions

### D1 — Split #2 into three deliverables
- **2a Corridor status**: status of a *named road* from its cameras. No routing engine. Ships value early, reuses the Hot Set.
- **2b Route-check**: user gives A→B, gets per-segment status with explicit coverage.
- **2c Alternatives**: reroute around bad segments. Depends on the routing engine supporting avoid-areas.

2b starts only after the gate in §4 passes.

### D2 — Routing engine: decide by spike, behind an interface
Constraints: motorbike is the primary vehicle; alternatives must be able to *avoid* flooded segments; staging has ~256MB RAM (cannot host any routing engine).

| Option | Finding | Verdict |
|---|---|---|
| OSRM public demo server | Non-commercial, max 1 req/s, no uptime guarantee; car/foot/bike profiles only | Not usable for a public product |
| OSRM self-hosted | Profile is a Lua file (motorbike profile must be authored); no arbitrary avoid-area | Weak for 2c |
| Valhalla self-hosted | Has `motorcycle` and `motor_scooter` costing; exposes `exclude_polygons` / `exclude_locations` (open GitHub issues report it misbehaving in some versions — verify on our extract) | Leading candidate |
| Hosted routing API | No RAM cost; quota/ToS and motorbike support vary | Fallback |

Define `route.Engine` (Go interface) so the engine is swappable. Run the engine on the home machine, not on staging.
Do **not** hand-write Dijkstra.

### D3 — Camera ↔ route matching
- Primary: distance from camera point to route polyline ≤ threshold (start 40 m, tunable).
- Secondary: normalized road-name tie-breaker (requires A6).
- Brute force is fine: 797 cameras × ~2,000 segments ≈ 1.6M distance checks, trivial in Go. No PostGIS, no R-tree.
- Known limitation: no heading data, so a camera on a cross street near the route can match falsely. Mitigation later: manual `heading` annotation for top corridors only.
- Only match cameras that are online, have coordinates, and have a non-stale verdict.

### D4 — Verdict semantics and honesty rules
- Two independent dimensions: `traffic` and `flood`. Never blend into one score.
- States: `clear | slow | jam | unknown | stale` (traffic), `none | shallow | deep | unknown | stale` (flood).
- **The product never says "OK to go".** It says what was verified: `coverage_pct` (route length within a verified camera's range), `oldest_observation_age_s`, and the worst state found. Unverified length is shown as unverified.
- Debounce: a state change needs N consecutive agreeing cycles (start N=2) to avoid green/yellow flapping.
- Staleness: verdict older than TTL (start 3 min) becomes `stale`. Frozen-frame detection: identical image hash for K cycles marks the camera `stale` (feeds can freeze on a dry-looking frame).
- Low model confidence becomes `unknown`, not a guess.

### D5 — Compute model (refines ADR-001's "never per-request inference")
- A route request calls `Registry.Touch()` for every matched camera (cap ≤ 25 per request).
- The handler never runs inference synchronously. Cameras without a fresh verdict are queued to a **bounded on-demand queue** (token bucket, global budget/min) and results arrive through the existing SSE stream as `route_update` events.
- Response for a cold route: partial result with `pending_cameras`, completed live over SSE.

### D6 — Accuracy gate (before any user-facing "clear")
- Build an evaluation set: ≥ 300 labeled frames, including ≥ 60 rain/flood-positive and night frames (proposal — owners may adjust).
- Report precision/recall per class for the chosen analyzer and threshold. Flood **false negatives** are the metric that matters.
- Until the gate passes, any route/flood verdict UI carries a "beta" label and cannot show a green state.
- Time-sensitive: HCMC's rainy season is under way (roughly May–November, peak around September–October, with high-tide flooding in autumn). Flood-positive frames can only be collected when floods happen.

### D7 — Snapshot storage: RFC #53 stance (defer SeaweedFS, build the interface now)
Archiving everything is not viable regardless of storage engine (100 KB/frame):

| Scenario | Files/day | GB/day | 30-day total |
|---|---|---|---|
| All 711 online cameras @ 30 s | 2,047,680 | 195 GB | 5.9 TB |
| Hot Set 50 @ 30 s | 144,000 | 13.7 GB | 412 GB |
| Hot Set 50 @ 5 min | 14,400 | 1.4 GB | 41 GB |
| Hot Set 50 @ 10 min (sampled) | 7,200 | 0.7 GB | 21 GB |

Decision:
1. Archive policy is **event-driven + sampled**: (a) frame that caused a state change to `jam`/`deep`, (b) 1 sample per 5–10 min per Hot Set camera, (c) frames added to the eval/training set. Nothing else is stored.
2. At that volume (≈ 10⁵ files/month) the small-file/inode problem RFC #53 solves does not exist yet. Start with `internal/storage` (interface) + **local filesystem** implementation (sharded by date/camera, metadata in SQLite). Add an S3 implementation when a trigger fires: > ~1M files, > ~200 GB, or a second node is needed.
3. RFC #53 requests before adoption: state the volume/retention model above; **measure** RAM inside a real 256MB container (the "~80MB" figure is unverified; `weed server -s3` runs master + volume + filer + S3 gateway in one process; volume index is ~20 B/file in RAM by default); tune volume size (default is ~30GB and compaction needs headroom — problematic on a small disk).
4. MCP addendum: acceptable as a **developer-time** tool (an agent browsing/labeling the bucket). It must not sit in the runtime path: the alert pipeline is deterministic Go code writing through the storage interface; an LLM-in-the-loop for persistence adds latency, cost, and nondeterminism to a safety path.

### D8 — Privacy and legal
- Frames contain faces and license plates. Keep retention short (start 14 days), restrict access, and define a rule before publishing any "proof" image publicly.
- Check the upstream portal's terms before long-term archiving or redistribution (currently the app only proxies).
- User origin/destination is location data: do not log or persist routes. Have this reviewed against Vietnam's personal-data decree before launch (not legal advice).

### D9 — Abuse and cost guards (before more public promotion)
Per-IP rate limit, cap cameras per route, cache route results by rounded coordinates, global analyzer budget/day with a circuit breaker that degrades to `unknown` (never to a stale green).

### D10 — Provenance is mandatory
Every verdict carries `source` (`mock | vlm | onnx`), `observed_at`, and `stale`. If `source=mock`, the UI shows a visible DEMO marker. Fabricated green dots on a public site are a trust and safety defect.

**Incident (2026-09-28, confirmed):** staging served `MockAnalyzer` output (hardcoded `CLEAR`, confidence 0.99) as if it were real analysis. Mechanism: viewing any camera calls `Registry.Touch`, the scheduler analyzes it every 30 s with the mock, the verdict is stored in SQLite and pushed over SSE, and the frontend colors the marker green and shows "BÌNH THƯỜNG". Root cause: `main.go` always constructs the mock analyzer. Fix: analyzer is selected by configuration (`ANALYZER=none|mock|vlm`), the default is `none`, and the process refuses to start with `mock` outside `APP_ENV=dev` (see H0).

## 3. Sprint 2 Backlog

Goal: make Sprint 1 honest and usable; lay foundations for #2. **No user-facing route-check in this sprint.**

| # | Title | Side | Depends on | Core DoD |
|---|---|---|---|---|
| H0 | **Hotfix: remove fabricated verdicts from public staging** | backend + web | none | `ANALYZER` config (default `none`); no verdicts stored or pushed when `none`; startup refuses `mock` outside `APP_ENV=dev`; verdict carries `source`; API does not serve mock verdicts outside dev; existing `camera_verdicts` rows purged; frontend ignores `source=mock` and hides the "AI monitoring" legend section while analysis is disabled; tests for each rule; shipped via `fix/` branch + PR, not a direct push |
| A1 | Ward polygons: full coverage | tooling | none | Every camera with coordinates has a ward, or is listed as an exception; script re-runnable; polygon source + date recorded; covers merged region |
| A2 | Structured names + i18n | data + web | A1 (schema can start in parallel) | Data stores prefix-less `name` + `type` for ward and road; UI renders "Phường X" / "X Ward", "Đường" / "Street"; diacritic-insensitive search; no baked prefixes remain |
| A3 | Filter as focus, not hide | web | none | Non-matching cameras dimmed but still visible and clickable; on-map filter chip with ✕; Esc clears; picking a camera outside the filter clears it; filter reflected in URL; no click-to-clear on touch devices |
| A4 | Dock rework | web | layout/reorder/size: none; severity badge: A5 + B3 | Larger tiles with 3 size presets (S/M/L) and a full-width grid view; drag-to-reorder pinned cameras using Pointer Events (no library) with a touch fallback (◀ ▶ buttons) and keyboard support; pin list + order persisted in localStorage; each tile shows name, image age, online/stale state; severity shown only when the verdict is fresh and `source != mock`, otherwise an explicit "no analysis" state; pin cap (start 12); collapses when empty |
| A5 | Verdict provenance + staleness (full) | backend + web | H0 | `observed_at`/`stale` in API + SSE; DEMO badge for `mock` in dev; frozen-frame detection with test (H0 already covers `source` and hiding mock output) |
| A6 | Road name normalization | data | none | Stable `road_key`; `QL1` ≡ `Quốc lộ 1`; duplicates handled; intersection names yield two road keys; unit tests on real data |
| B1 | `internal/storage` + local FS archiver | backend | none | Interface + local impl; event-driven + sampled policy; retention job; disk-usage cap; runs on home machine; starts collecting frames immediately |
| B2 | RFC #53 amendments | infra (@thdat-vu) | none | Volume/retention model added; RAM measured in 256MB container; adopt/defer decision recorded |
| B3 | Real analyzer adapter (VLM) | backend | none | Structured JSON output (traffic + flood, confidence); cadence tiers (dry vs rain mode); daily budget + circuit breaker; `source=vlm` |
| B4 | Eval set + benchmark harness | spike | B1 (frames), B3 | ≥ 300 labeled frames; script outputs precision/recall per class; report checked in |
| B5 | Routing engine spike | spike | none | Valhalla vs OSRM vs hosted compared on an HCMC extract: RAM, latency, motorcycle behavior, avoid support; decision recorded; time-boxed |
| B6 | 2a Corridor status | backend + web | A5, A6 (B3 optional) | `GET /api/roads/{road_key}/status` with coverage, oldest observation, evidence frames; status badge on street rows in the tree |
| B7 | Abuse/cost guards | backend | none | Rate limit middleware, per-request camera cap, budget breaker; tests |

Suggested order: **H0 today (public trust/safety) and B1 in parallel (data collection is time-sensitive)**, then A5, A3, A1 in parallel; A2/A6 after; A4 layout/reorder any time after H0, its severity badge only after B3; B3→B4 in parallel with B5; B6 last.

## 4. Gate to Start 2b (Route-Check)

All of: B4 report meets the agreed flood false-negative target; B5 decision made; B6 shipped and used for at least one rainy week; B7 in place.

## 5. Open Questions

1. ~~Dock UX~~ Resolved 2026-09-28: thumbnails too small; pinned cameras cannot be reordered by drag and drop; the dock is only a list and lacks status (jam/clear). Captured in A4. Note: status can only be shown truthfully after a real analyzer exists (B3); until then the dock shows an explicit "no analysis" state.
2. ~~Mock markers~~ Resolved 2026-09-28: confirmed, all green markers were `MockAnalyzer` output. Captured in D10 and H0.
3. Where do post-reform ward polygons come from (official dataset vs OSM)? Not yet verified as downloadable for the whole region.
4. Should search accept old district names (e.g. "Quận 1", "Tân Bình") as aliases? People likely still use them colloquially (inference); needs old boundary data.