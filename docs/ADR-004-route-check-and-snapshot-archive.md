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
