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

---

## 6. Addendum — H3 Hexagon Overlay Proposal, and DatapolisX Reference

### D11 — H3 hexagon overlay: adopt Option B, with two corrections to the proposal

The macro-zoom clutter problem is real (837 points at zoom 11–13 is visibly noisy in the current staging screenshots) and the sparse/transparent-by-default styling correctly extends this ADR's honesty rule (D4) from points to areas. Adopt it, with corrections:

**Correction 1 — aggregation function must be worst-observed, never averaged, and gated by a minimum camera count.** The proposal doesn't specify how a cell's color is derived from its cameras. Averaging a flooded street's reading into a hex sharing space with nine dry streets makes the alert disappear exactly where it matters most; a 5 km² cell (H3 res 7) also risks over-broad panic if one camera drives the whole cell red. Rule: a cell shows a color only if it has ≥3 cameras with a fresh, non-`mock` verdict (reuse D10's provenance work — H0/A5); its color is the worst state among them; the tooltip states the count and the worst camera's name, never a blended figure. Cells below the camera-count floor stay transparent (true "no data"), same as D4's per-route rule — this is the H3 version of the same honesty constraint, not a new one.

**Correction 2 — a hex is not a Phường.** H3 is a uniform global grid; its cell boundaries do not follow administrative boundaries and will routinely straddle two or more wards. The proposal's own example tooltip ("Phường Bến Nghé (Quận 1)") repeats the exact pre-reform naming ADR-003 corrected — Quận no longer exists (ADR-003 §Decision 3). Label a hex by its nearest road/landmark, not by ward; if a ward label is wanted, show "spans N wards" for boundary cells rather than picking one.

**Runtime library conflict — resolve by moving H3 indexing offline.** The official `uber/h3-go` binding requires `CGO_ENABLED=1`, which conflicts with the pure-Go, no-cgo, single-static-binary line this project has held everywhere else (the reason `modernc.org/sqlite` was chosen over a cgo SQLite driver, and why `gocv`/OpenCV was rejected for the Go server). No mature pure-Go H3 port is confirmed available — do not assume one exists without checking at spike time. The actual fix is simpler than swapping libraries: **lat/lng → H3 cell ID only needs to run once, offline**, at the same stage as A1's ward enrichment. Add `h3_id` (res TBD by spike, start at two resolutions — e.g. 7 for macro, 9 for a closer zoom tier — and compare) to the same enrichment output using `h3-py` (or any H3 lib) in the offline tool, where cgo is a non-issue. The running Go server never computes H3 math — it only groups already-tagged cameras by the precomputed `h3_id` string and picks the worst verdict per group, which is plain map/slice code. This keeps the server binary exactly as dependency-free as before.

**Transport**: reuse the existing SSE stream (#1) with a new `hex_update` event type. No new connection type needed.

### D12 — SI Score (DatapolisX): valid technique, wrong phase — defer

Verified in `web/src/controllers/trafficController.ts` (MIT-licensed, repo confirmed real): `siScore = (avgSiRaw / (capacity * 1.3)) * 100`, where `capacity` is the historical max vehicle count ever observed on that camera (`_max: total_objects`), not a fixed road-engineering capacity. This is a reasonable data-driven proxy, but it requires **raw per-class vehicle counts from an object-detection model** as its input. Warded's current and near-term analyzer (Mock, then a Cloud Vision/VLM adapter per ADR-001) returns a coarse categorical label, not vehicle counts — SI Score is not computable from that output. Adopting it now would mean committing to self-hosted YOLO detection earlier than ADR-001's phased plan intends.

Decision: defer. Revisit only when B3 (Sprint 2) moves past the VLM adapter to a self-hosted detector. At that point, DatapolisX's `best.pt` (YOLOv11, MIT-licensed, verified as a real ~80MB checkpoint, not a placeholder) is a legitimate starting checkpoint for the B4 benchmark — subject to the same accuracy gate (D6) as any other candidate, re-validated on Warded's own camera angles/quality before being trusted, and with attribution kept per its MIT license. Also note for whoever picks this up later: a brand-new camera has no historical max, so `capacity` needs a floor value to avoid divide-by-zero/nonsense SI on cold-start cameras — DatapolisX's implementation doesn't appear to guard this either (not fully audited — check before reusing).

### D13 — Gemini weather+advice text (DatapolisX): acceptable later, only as commentary over an already-computed verdict

The referenced `/api/weather` route calls Gemini per popup-click, combining SI score + trend + time-of-day into short driving advice. This is a different use of an LLM than the flood/traffic verdict itself (D5/D10): it's user-triggered, request-time commentary layered on top of a number the Hot Set pipeline already computed — not the thing deciding the severity. That distinction is what makes it acceptable: it doesn't reintroduce per-request inference into the safety-critical path, it explains a result that already exists.

Two conditions before building this: (1) the prompt must be constrained to only reference the structured inputs it's given (score, trend, time) — not free-associate street names or claims the input didn't contain, or it inherits the same fabrication risk as the H0 incident, just in prose instead of a colored dot; (2) it only ships after a real analyzer exists (H0/B3) — advice text narrating a mock number is the H0 incident again, dressed in sentences. Not scheduled yet; track as a candidate for a future sprint once B3 ships.

## 7. Sprint 2 Additions (H3 Overlay)

| # | Title | Side | Depends on | Core DoD |
|---|---|---|---|---|
| C2 | `h3_id` offline enrichment | tooling | none (extends A1's script) | Adds `h3_id` to the same enrichment output as A1, at least 2 candidate resolutions computed for comparison; library choice and resolution recorded with rationale; runs offline only, no H3 dependency added to `services/go-server` |
| C3 | Hex aggregation + SSE event | backend | C2, A5 (provenance) | Groups cameras by `h3_id`; colors only cells with ≥3 fresh non-mock verdicts; worst-of aggregation (never averaged); `hex_update` pushed over the existing `/api/live/stream`; unit test on the camera-count floor and the worst-of rule |
| C4 | Hex overlay rendering | web | C3 | Sparse/transparent by default; fades in at macro zoom (11–13), fades out by micro zoom (14+); toggle chip; tooltip shows camera count + worst camera name, never a ward name (or "spans N wards" if shown at all); no `h3-js` bundle added — cells and colors come from the backend only |

Not scheduled: SI-Score-based severity (D12, waits on self-hosted detector) and LLM advisory text (D13, waits on a real analyzer + prompt-guardrail design).

---

## 8. Sprint 2 Report Corrections

- **A4 (dock rework) was never shipped**, despite the completion report counting "15/15 (100%)". The count only matched because A4 was silently dropped from the Wave-planning document, not because it was done. PR #115 covers only the mobile collapse/drawer presentation, not drag-to-reorder, S/M/L sizing, the pin cap, or per-tile age/stale display. **A4 is reopened, carried into Sprint 3 at its original scope** (severity badge sub-item now additionally gated on S3 below).
- The report's "Gemini 2.5 Flash-Lite" does not match the actual code default (`GEMINI_MODEL`, default `gemini-3.5-flash-lite`, per `cloudvision.go:23`). Minor, no action needed beyond noting that sprint completion reports should be spot-checked against code, not taken as ground truth — this is the second discrepancy found this way (the first being A4).
- B5 spike conclusion (adopt Valhalla) stands — `exclude_polygons` support is an independent, sufficient reason, with or without the RAM test. Two caveats for the record, not reversing the decision: (1) the stated "<100ms, fully real-time" claim is contradicted by the benchmark's own P99 of 122.2ms on `long_q12_d1` — fine for P50 UX, just don't repeat the "fully within 100ms" framing; (2) the OSRM OOM test used `osrm-routed` loading the graph directly, not the documented `osrm-datastore` shared-memory mode — the RAM comparison may be harsher on OSRM than its best configuration, but doesn't change the outcome given the hard `exclude_polygons` blocker.

## 9. Sprint 3 Scoping — Self-Hosted Vehicle Detection

### D14 — Real hardware capacity differs substantially from the spike's test condition

`local-edge host` (ZOTAC ZBOX-CI320NANO, Celeron N2930, 4C/4T @ 1.83GHz, **8GB RAM**, 250GB 5400RPM HDD, Debian 13, Docker) is the deployment target. The 256MB figure in spike #104/#72 was an artificial constraint simulating a worst-case container, not this machine's real capacity. Combined estimated footprint of Valhalla (~223MB unconstrained) + a CV inference worker (~82MB measured, single-stream) + `go-server` + Docker/Portainer/Traefik overhead is under 1GB against 7.7GB available — **RAM is not the binding constraint for this sprint's scope.** The real constraints are CPU throughput (only single-stream throughput has been measured; the box has 4 real cores, untested in parallel) and disk I/O (spinning 5400RPM HDD shared by the archiver, SQLite, Valhalla tiles, and the CV worker — a correlated write/read burst during a storm, same shape as the previously-noted correlated network-load risk, is a disk-contention risk here too). WiFi is the only active uplink (`enp1s0` is down) — if `local-edge host` is reachable by the public-facing `warded-stg` service (topology not yet confirmed — see Open Questions), a home WiFi/power outage is a single point of failure for a public safety feature, and needs the same degrade-to-`unknown` treatment as the existing VLM budget breaker (D9), not a hard failure or a frozen stale state. The fanless/7.5W TDP chassis is also a thermal-throttling risk under sustained 4-core load and should be monitored, not assumed fine.

### D15 — Vehicle detection: two-tier design, self-hosted, candidate models to benchmark before choosing

Agreed approach: YOLO inference layer outputs full per-class vehicle counts (motorcycle/car/bus/truck/container — near-zero marginal cost since YOLO already produces all class boxes per frame); a separate Go business-logic layer converts counts to a PCU-weighted Saturation Index, then buckets into the existing `CLEAR/MODERATE/HEAVY/CONGESTED` labels. Candidate weights (motorcycle 0.2, car 1.0, bus/truck 2.0, container 3.0) are a reasonable starting point but should be treated as calibratable, not fixed — motorcycle PCU values in published Vietnamese/regional traffic studies vary (some cite higher than 0.2); tune against the B4 eval set once real labeled frames exist, don't ship the first guess as final.

Two candidate weight sets to benchmark via the existing `tools/eval` harness (B4) before picking one, on Warded's own camera images (not DatapolisX's): stock COCO-pretrained YOLOv8n/v11n (zero-shot, no training, free) vs. DatapolisX's `best.pt` (MIT-licensed, Vietnamese-vehicle-specific including container trucks, verified real in the earlier ADR-004 addendum). Export the chosen model to OpenVINO IR for the runtime validated in spike #104.

### D16 — Flood stays VLM-only this sprint (confirmed)

No self-hosted flood model this sprint, per the earlier addendum's finding (SpaceNet8's best published flooded-road IoU is ~40%, on satellite imagery, not street-level CCTV — public flood-segmentation datasets found are out-of-domain for Warded's cameras). The VLM adapter continues to carry flood severity. The archiver (B1) continues collecting frames; a lightweight labeling tool (S6 below) is added this sprint specifically to make progress toward the ≥300-frame eval set (D6) faster, not to train a model yet.

## 10. Sprint 3 Backlog

| # | Title | Side | Depends on | Core DoD |
|---|---|---|---|---|
| S1 | Multi-core OpenVINO throughput benchmark on `local-edge host` | spike | none | Measures real multi-stream/parallel throughput (not just the single-stream 2.27 FPS from #104) on the actual box; result used to set the final safe N and analysis interval, replacing the provisional estimate |
| S2 | Curate final Hot Set corridor/camera list (resolve the 70-vs-30-50 gap) | data | S1 | Explicit, named list of cameras (not just corridor names) with a stated inclusion rule (e.g. major intersections only vs. every camera on a corridor); extends the existing `criticalKeywords` list in `main.go` rather than inventing a parallel config; count reconciled against S1's real capacity number |
| S3 | Self-hosted vehicle analyzer (OpenVINO) | backend | S1, S2 | Implements `hotset.Analyzer`; benchmarks stock COCO YOLOv8n/v11n vs. DatapolisX `best.pt` via `tools/eval` on Warded's own frames, decision recorded with numbers; exports chosen model to OpenVINO IR; sets `Verdict.Source = "onnx"` (or a new `"openvino"` value — pick one and use it consistently) |
| S4 | PCU-weighted Saturation Index business logic | backend | S3 | Per-class counts to PCU-weighted score to `CLEAR/MODERATE/HEAVY/CONGESTED` mapping; weights documented as calibratable, not hardcoded as final; unit tests on the bucket boundaries |
| S5 | A4 carryover: dock rework (full original scope) | web | S3 for the severity badge only; rest is independent | Drag-to-reorder via Pointer Events + touch/keyboard fallback; S/M/L tile sizes; pin cap 12; age + online/stale shown per tile; severity badge shown only once S3 ships, explicit "no analysis" state until then |
| S6 | Flood frame labeler tool | tooling | none | Reads `data/proofs/`; single-keypress labeling (dry/shallow/deep) against the existing `label_schema.json`; target: labeling throughput fast enough to reach the ≥300-frame D6 target well before rainy season ends |
| S7 | Disk I/O monitoring on `local-edge host` | infra | none | Baseline I/O wait/throughput captured under normal load and simulated storm-burst load (concurrent archiver writes + SQLite writes + Valhalla tile reads); documented threshold for when to move to SSD or split services across disks |
| S8 | `local-edge host` ↔ `warded-stg` topology + fallback behavior | infra + backend | answers the Open Question below | Documents how the public staging service reaches `local-edge host` (if at all); defines degrade-to-`unknown` behavior (reusing D9's breaker pattern) if `local-edge host` becomes unreachable, instead of a hard failure or a frozen last-known state |

## 11. Open Questions

1. Does `warded-stg` (the public staging service) call `local-edge host` over Tailscale for routing/CV work, or does everything for Sprint 3 run entirely on `local-edge host` with nothing public-facing yet? This decides whether S8's fallback behavior is urgent or can wait.
2. For S2: confirm whether "N≈30–50" means curated key intersections within the 4 named corridors, or the full corridor camera count (which sums to 70 for just those 4) — the two don't currently match.

---

## 12. Sprint 2 Report v1.2.0 Reconciliation

- A4 is now correctly reported as **Deferred (1 item, 93.75% completed)** rather than folded into a padded "100%". Accepted, no further correction needed on this point.
- New infrastructure shipped beyond the original Sprint 2 scope: #117 (3-container split: `warded-backend`, `warded-frontend`, `warded-model` — the latter an OpenVINO Python 3.11 container, `cgroups 512MB RAM / 1.0 CPU`, port 8000) and #120 (Open-Meteo + RainViewer weather/radar integration, zero-key, in-memory TTL cache + `singleflight`). Both are reasonable, but raise open questions below before Sprint 3 scope is finalized.
- **Open question — `warded-model` container**: does it already run an actual model/endpoint, or is it an empty scaffold awaiting S3's work? Which host runs it (`local-edge host` or a separate staging VPS)? Is the `1.0 CPU` limit intentional/final, or an untuned default? This directly changes S1/S3 scope — if the limit is final, the single-stream 2.27 FPS number from spike #104 is the real ceiling for this container regardless of the host's 4 physical cores, and S1 becomes "decide whether to raise the limit" rather than "benchmark multi-core throughput."
- **Open question — flood hex (#106) vs. weather radar (#120)**: confirm these remain two separately labeled, distinguishable layers in the UI. A camera-verified flood hex (worst-of, ≥3 cameras, provenance-tracked per D10/D11) and a radar-based rain-probability forecast are different categories of claim (observed vs. predicted) and must not be presented as interchangeable under one toggle — this is the same Honesty Rule (D4) already governing every other verdict in this system, now extended to the new weather layer.

## 13. S2 Update — Intersection-Only Curation Rule (confirmed)

N≈30–50 is achieved by restricting the Hot Set to cameras **at named intersections on the 16 arterial corridors**, not every camera along a corridor. This reuses A6's existing output directly: a camera qualifies if its `road_keys` array has length 2 (A6 already splits `"A - B"`-style intersection names into two keys; a single-key camera is mid-block, not an intersection) **and** at least one of those two keys matches one of the 16 arterial `road_key`s. No new data or classification logic is needed — S2 is a filter over already-enriched data, not a new enrichment pass. S2's DoD is updated accordingly: the curated list must be reproducible by this rule (documented as a query/script over `cameras_enriched.json`, not a hand-picked list), with the resulting count reconciled against S1's real capacity number once the `warded-model` CPU-limit question above is resolved.

---

## 14. Sprint 3 Open Questions — Resolved

### Finding 1 — `warded-model` standby response is a provenance hazard, not yet live

Confirmed: no model is loaded (`compiled_model is None`); `POST /predict` returns a static `{"traffic_severity": "CLEAR", "confidence": 0.95, "mode": "standby_ready"}` with HTTP 200. No live risk today — the Go backend does not yet call `CV_ENGINE_URL` (`config.go:37` reads the var but no client exists). This is the same failure shape as the H0 incident, worse in one respect: there is no `source` field to filter on, and a confidence of 0.95 reads as more credible than the old mock's. **Required DoD addition for S3**: the standby response must return a non-200 status (or at minimum an explicit `"source": "standby_dummy"` with confidence 0.0) before any Go client is wired to this endpoint. Do this before, not during, S3's integration step.

The `1.0 CPU / 512MB` limit on `warded-model` is confirmed to be a deliberately protective default carried over from the spike #104 sandbox (explicitly to avoid starving the rest of staging), not a final tuned value — revisit in S1/S2 per Finding 4 below.

### Finding 2 — Single point of failure confirmed, not hypothetical

All three staging containers (`warded-backend`, `warded-frontend`, `warded-model`) run on the same box, `local-edge host`, with no separate VPS and no wired ethernet (WiFi-only uplink, per the earlier hardware audit). A home power/WiFi outage takes down the entire public `warded-stg` service, not just a compute auxiliary. This elevates S8 (topology + fallback behavior) from "worth doing" to a real operational risk to prioritize in Sprint 3, and is a deciding factor in Finding 4 below.

### Finding 3 — Flood hex (camera-verified) is visually suppressed by weather radar (forecast), confirmed

`HexOverlay`'s single "flood" mode toggle drives both `RainRadarLayer` (Open-Meteo/RainViewer forecast) and the camera-verified flood hex simultaneously. With `floodHexCells` currently empty (no camera has yet triggered a flood verdict), users seeing the "Khả năng có mưa" layer are only ever shown radar cloud imagery, with no UI cue that this is forecast data, not a camera-confirmed observation — a real violation of the Honesty Rule (D4), confirmed in code rather than suspected. **Required fix**: separate, clearly labeled layers (e.g. "Dự báo radar" vs. "Camera xác nhận"), rendered so the camera-verified layer is never hidden behind or replaced by the forecast layer when real flood data exists. Tracked as new backlog item S9.

### Finding 4 — Real N is 286 (245 after filtering offline/duplicates) across all 16 corridors, far above the 30–50 estimate

The precise query (road_keys length 2, intersecting one of the 16 arterial `road_key`s) returns far more cameras than the earlier rough estimate assumed. At 2.27 FPS / 1 CPU, 286 cameras on a 30s cycle would require ~9.5 FPS — not achievable without either narrowing scope or adding compute. Three options, not one:
1. Narrow to ~4–5 priority corridors (e.g. Điện Biên Phủ + Cộng Hòa + Xa Lộ Hà Nội + Võ Văn Kiệt = **55** cameras, corrected from an earlier arithmetic slip of 53) — no infrastructure change.
2. Raise `warded-model`'s CPU limit and validate real multi-core throughput — recovers broader coverage, but adds load to the same box now confirmed as the sole host for every public-facing service (Finding 2) — a materially different risk profile than when this was assumed to be a dedicated or redundant box.
3. Keep 1.0 CPU, widen the analysis cycle to 45–60s instead of 30s for vehicle congestion specifically (flood stays on its own VLM cadence) — no infra change, larger safe N than option 1.

**Decision for Sprint 3: prefer option 1 or 3 over option 2.** Given Finding 2, deliberately adding CPU load to the single box already carrying the entire public service is the wrong trade-off right now; preserve headroom rather than spend it. Revisit option 2 only after S8's resilience/fallback work lands.

## 15. Sprint 3 Backlog — Updates

- **S1**: scope changes from "benchmark multi-core throughput" to "decide final cycle interval and corridor count under the confirmed 1.0 CPU limit" (Finding 4, option 1 or 3) — do not raise the CPU limit as part of this sprint.
- **S2**: curation rule confirmed and reusable as-is (A6's `road_keys` length-2 filter); scope now narrows to selecting the specific 4–5 priority corridors (~55 cameras) per Finding 4, not all 16.
- **S3**: add explicit DoD item — fix the `warded-model` standby response (Finding 1) before wiring any Go client to `CV_ENGINE_URL`.
- **S8**: priority raised from "worth doing" to a real operational risk, given Finding 2 is now confirmed rather than hypothetical.
- **New — S9**: `[web] separate flood-hex (camera-verified) from weather radar (forecast) as distinguishable layers`. Depends on: none (frontend-only fix). DoD: two independently labeled layers per Finding 3; camera-verified flood hex never hidden by the radar layer when real flood data exists; i18n labels make the observed-vs-forecast distinction explicit in both `vi` and `en`.