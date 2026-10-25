# Project Status & AI Contributor Onboarding Guide

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.3.2  
**Last Updated:** 2026-10-23  


---

## 1. Executive Overview & System Purpose

ProjectKV provides a real-time, zero-click urban traffic and flood condition explorer for Ho Chi Minh City, Vietnam. Commuters explore live streaming camera snapshots, corridor traffic saturation levels, and rain/flood layers before traveling to avoid bottlenecks and submerged streets.

---

## 2. Microservice Architecture & Port Allocations

| Service | Technology | Port | Host & Node Role | Primary Responsibilities |
|---|---|:---:|---|---|
| **Web Client** (`apps/web`) | Next.js 15 (App Router), React 19, Tailwind CSS, Leaflet | `3001` | `homesrv` (Portainer Endpoint 3) | Map visualization, discrete camera marker dots with dynamic arterial saturation coloring (Green/Yellow/Red dots), H3 hex overlays, offline camera toggle, horizontal drag-to-scroll pinned camera dock, bilingual UI (`vi` default with full diacritics, `en` secondary). |
| **Backend API** (`services/go-server`) | Go 1.24, Gin, SQLite (WAL mode) | `3000` | `homesrv` (Portainer Endpoint 3) | Snapshot proxy cache with 15s in-memory TTL, camera catalog, hotset scheduler (30s cycle, `internal/hotset`), autonomous harvester & Cloudflare R2 archiver (`internal/harvester`), PCU Saturation Index calculator, Telegram flood alert dispatcher (`internal/notify`), SSE (`/api/live/stream`), WebSockets (`/ws/live`), Weather forecast (`/api/weather`), Swagger docs. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR runtime, YOLOv8n FP16 | `8000` | `dev-box` worker (Portainer Endpoint 4) | Automated vehicle detection (`POST /predict`), class counting (motorcycle, car, bus, truck), `GET /healthz` healthchecks, multi-threaded request processing, 2 vCPU / 2 GB RAM capped. |

---

## 3. Current Operational Metrics & Repository Status

- **Lifetime PR History**: Exactly **165 Pull Requests** processed across repository history:
  - **155 Merged** to `dev` (including Sprint 4–6 PRs, Sprint 7 deliverables, PR #270 Sprint 7 Completion Report, PR #272 S8-T1 Doppler Rain Radar Pipeline Overhaul, PR #273 S7-T4 Feedback R2 Archiver, PR #274 S7-T7c Standalone Harvester CLI, PR #275 S7-T7d Web Labeler Candidate Review, PR #280 S8 Optimize ListDates, PR #282 Cloudflare Zero Trust Dev Token Fallback, PR #284 Waterway Bridge Footing Camera Exclusion, PR #286 Archiver/Labeler In-Memory Cache and Resilient Batch Auto-Labeler, PR #291 S8 ADR-010 & Wave Execution Guide, PR #292 S8-T5 Harvester Semaphore Deadlock Fix & Weather Guessing Deprecation)
  - **8 Closed** without merging
  - **0 Open** PRs pending
- **Staging Infrastructure Configuration (Stack 14)**:
  - `docker-compose.stg.yml` explicitly parameterizes `APP_ENV=${APP_ENV:-staging}` to prevent `GIN_MODE=release` from defaulting environment to `production` and triggering safety crashes during mock/staging runs.
  - Aligned with `.env.stg.example` template as the authoritative single source of truth for Portainer CD redeploy jobs.
- **Camera Catalog**:
  - **837 total cameras** loaded in `cameras_enriched.json`
  - **711 online roadway cameras** (status `UP`, `isOnline: true`), **123 offline** (status `NOT_IMAGE`), and **3 excluded bridge footing waterway sensors** (status `WATERWAY_MONITOR`, `isWaterway: true`, Issue #283) as of 2026-10-22
  - Static liveness (`isOnline` / `status`) is verified by live upstream probing; non-road river/waterway cameras are strictly excluded from map rendering and flood perception pipelines; runtime placeholder detection runs in `proxycache` and surfaces `503 GHOST_CAMERA`
  - **57 curated intersection cameras (Hot Set)** across 4 priority commuter corridors:
    1. Võ Văn Kiệt (`vo_van_kiet`): 23 cameras
    2. Mai Chí Thọ (`mai_chi_tho`): 13 cameras
    3. Điện Biên Phủ (`dien_bien_phu`): 11 cameras
    4. Cộng Hòa (`cong_hoa`): 10 cameras
  - Curated strictly via A6 rule: `len(road_keys) == 2` (n nút giao, descartes mid-block straight stretches).
- **Camera Health Audit & Catalog Resync (Issue #178, PR #179)**:
  - `tools/crawler/audit_all_cameras.js` probes all 837 snapshots against the live DOT upstream (15 workers, 5s timeout) and classifies each feed (`SUCCESS`, `GHOST_PLACEHOLDER`, `EMPTY_BODY`, `TIMEOUT`, `NETWORK_ERROR`, `HTTP_*`).
  - `tools/crawler/sync_catalog_from_audit.js` re-synchronizes `cameras_all.json` / `cameras_enriched.json` from the probe report (deterministic outcomes only; transient errors leave entries untouched). This replaced the former hardcoded `KnownGhostCameras` map, which had drifted out of sync, making the JSON catalog the single source of truth for static liveness.
  - `.github/workflows/camera-health-audit.yml` runs the audit daily (01:00 UTC) and opens a resync Pull Request against `dev` when drift is detected (never auto-merges).
  - The web client promotes a `503 GHOST_CAMERA` snapshot response into shared state (`useCameras.markCameraGhost`), immediately reflecting field signal loss across the map, sidebar, dock and modal.
- **Known-State Smoke-Test Camera Set (Sprint 4 T7, Issue #187, PR #193)**:
  - Curated 10 high-confidence ground-truth cameras in `tools/eval/fixtures/smoke_test_cameras.json` (5 `CLEAR` off-peak baselines, 5 `CONGESTED` peak rush-hour baselines) across 4 priority corridors (`vo_van_kiet`, `mai_chi_tho`, `dien_bien_phu`, `cong_hoa`).
  - Implemented automated verification script `tools/eval/verify_smoke_set.py` and unit test suite `tools/eval/verify_smoke_set_test.py` to objectively gate model predictions without relying on anecdotal staging UI spot-checks.
  - Detailed physical and operational rationale documented in `tools/eval/smoke_test_cameras.md`.
- **Evaluation Dataset & Progress Tracking (Sprint 4 T3, Issue #194, PR #199)**:
  - Strictly excluded all pre-10-06 mock frames (107 bytes) from ground-truth dataset.
  - Implemented `tools/eval/collect_real_frames.py` and `tools/eval/dataset_progress.py` to fetch, validate, and audit real frames against ADR-004 §D6 targets.
  - Completed ground-truth dataset via live passive environmental collection during the 2026-10-08 HCMC rainstorm: **764 real labeled frames** across 206 unique cameras, including **120 flood-positive frames** (98 shallow, 22 deep) and 468 night rain frames.
  - All ADR-004 §D6 composition quotas satisfied: Real frames 764/300 (254.7%), Flood 120/60 (200.0%), Clear/dry/day 145/100, Moderate 199/50, Heavy/congested 181/50, Rain 468/40, Night 468/30.
  - Dataset status: **READY FOR D6 ACCURACY GATE (T4)**.
- **Real D6 Accuracy Gate & OpenVINO Benchmark (Sprint 4 T4, Issue #195)**:
  - Executed `tools/eval/benchmark.py` against live self-hosted OpenVINO container across the full 764 real frames.
  - Traffic Macro F1: **0.209** (Weighted 0.364), highlighting uncalibrated cold-start capacity floors and dense occlusion under congestion (informing Sprint 4 T5 PCU calibration).
  - Flood Safety Gate: **FAIL (100.0% FNR > 5.0%)**, properly measuring that the stock vehicle-only COCO model does not detect standing water on the 120 flood frames (informing Sprint 4 T9 multimodal flood pipeline).
  - Known-state smoke-test validation: **2/10 PASS (20.0%)** on curated baselines (`docs/reports/smoke_eval_d6_real.md`).
  - Full empirical findings committed to `docs/reports/eval_report_d6_real.md` and `docs/reports/dataset_progress.md`.
- **PCU Calibration & Saturation Index Realignment (Sprint 4 T5, Issue #196, PR #202)**:
  - Calibrated default Passenger Car Unit (PCU) weights against empirical HCMC traffic data and Vietnamese urban transport standards (**TCVN 4054:2005** / **TCVN 13592:2022**): `Motorcycle = 0.30` (from 0.20), `Car = 1.00`, `Bus = 2.50` (from 2.00), `Truck = 2.00`, `Container = 2.00` (folded into Truck per ADR-005 T6).
  - Realigned cold-start `CapacityFloor` from `25.0` down to `15.0` PCU matching the physical queue geometry of a 20–30m CCTV viewport (3 lanes x 4–5 queue vehicles).
  - Recalibrated Saturation Index Level of Service (LOS) cutoffs: `Moderate = 0.30` (from 0.35), `Heavy = 0.60` (from 0.70), `Congested = 0.80` (from 0.90).
  - Preserved backward compatibility with custom environment variable overrides (`PCU_WEIGHT_*`, `PCU_CAPACITY_FLOOR`, `PCU_THRESHOLD_*`).
  - Full rationale, empirical distribution findings, and comparison tables documented in `docs/pcu-calibration-report.md`.
- **Architectural Decision: OpenVINO vs. VLM for Traffic (Sprint 4 T9, Issue #197, PR #205)**:
  - Evaluated self-hosted OpenVINO vs. cloud VLM (Gemini Flash-Lite) across throughput, hardware utilization, latency, and economics following Sprint 4 T4 real-world benchmarks.
  - Adopted **Option C: Decoupled Hybrid Architecture**: Retained self-hosted OpenVINO as the core continuous 30-second traffic telemetry engine ($0 API cost, 24.01 FPS on `dev-box`, 18.2ms P50 latency, 0.00% failure rate across 3,383 requests, vs. VLM's ~$111/mo cost and 228s latency for 57 cameras).
  - Outlined resolution scaling ($320 \times 320 \rightarrow 416 \times 416$) on `dev-box` to address motorcycle recall while reserving cloud VLM strictly for low-frequency on-demand modal checks and rain-triggered flood audits.
  - Formal Architectural Decision Record committed in `docs/decision-openvino-vs-vlm.md`.
  - All Sprint 4 backlog items (T1–T9) are now completed or decided, unblocking Sprint 5 long-term routing work (`route.Engine`).
- **Sprint 4 Completion & Reality-Check Audit Report**:
  - Full audit of all 9 Sprint 4 backlog tickets (T1–T9), empirical evaluation findings, staging self-hosting readiness audit, and Sprint 5 recommendations compiled in [`docs/sprint4_report.md`](sprint4_report.md).
  - Technical audit Q&A resolving all 13 critical verification inquiries from reviewers across 4 thematic groups with line-by-line codebase citations committed in [`docs/sprint4_audit_qa.md`](sprint4_audit_qa.md).
  - Ground-truth reality check and authoritative Sprint 5 execution roadmap adopted in [`docs/ADR-006-sprint4-audit-and-sprint5-plan.md`](ADR-006-sprint4-audit-and-sprint5-plan.md) (PR #206), establishing mandatory empirical gating (S5-T1) before feature expansion, upgrading detector resolution to $416\times 416$ (S5-T3), centralizing blob storage for team web labeling (S5-T4), and hardening network resilience (S5-T6).
- **Sprint 5 Backlog & Execution Guide**:
  - Four active GitHub issues created on `529-studio/projectKV`:
    - [#207](https://github.com/529-studio/projectKV/issues/207): S5-T1 — `[tooling] re-run D6 benchmark and smoke test with post-T5 weights` (**Completed / Verified**): Re-ran real D6 benchmark across all 764 frames with post-T5 weights (`CapacityFloor=15.0`). Traffic Macro F1 resulted in **0.206** (Weighted 0.351), confirming the ADR-006 Finding 2 mathematical paradox: lowering CapacityFloor doubled false-positive CONGESTED classifications on actual CLEAR frames (from 22 to 46 frames, +109%) without resolving missed bottlenecks (67/73 CONGESTED frames missed as CLEAR). Flood Safety Gate remained at 100.0% FNR. Smoke pass rate reached 50.0% (5/10), unblocking S5-T2 object detection audit.
    - [#208](https://github.com/529-studio/projectKV/issues/208): S5-T2 — `[cv-engine] object detection audit for false-positive/missed detections` (**Completed / Verified**): Diagnosed root causes for both perception failure modes (a) missed congestion queues and (b) clear-road hallucinations. Proved that missing Non-Maximum Suppression (NMS) in `cv-engine/server.py` caused a 2.7x–4.7x anchor duplication multiplying 3–5 cars into 28–39 detections, and 1.78x non-uniform aspect ratio stretch crushed detection confidence by ~35% on real queues. Identified ground-truth annotation discrepancy on `Mai Chí Thọ - Đồng Văn Cống 2` (active construction zone with physically empty road). Formulated 5 concrete resolution proposals (NMS with IoU 0.45, letterboxing, 416x416 resolution upgrade, cross-class NMS, and ground-truth correction). Committed automated audit tooling (`tools/eval/audit_cv_detections.py`), visual overlays (`docs/reports/audit_overlays/`), structured findings (`docs/reports/cv_detection_error_audit.json`), and comprehensive audit report (`docs/reports/cv_detection_error_audit.md`).
    - [#209](https://github.com/529-studio/projectKV/issues/209): S5-T3 — `[go-server + web] decoupled hybrid flood pipeline` (**Completed / Merged in PR #216**).
    - [#210](https://github.com/529-studio/projectKV/issues/210): S5-T4 — `[infra] physical Tailscale disconnection test for dev-box failover` (**Completed / Verified**): Executed live physical network partition and worker outage chaos drill on Staging (`homesrv` Stack 14 $\leftrightarrow$ `dev-box` Stack 17). Demonstrated 100% clean graceful degradation to `SeverityUnknown` (`source: "fallback"` then `source: "circuit_breaker"`) with 0 panics, 0 memory leaks (RAM bounded at 37.8 MB, 14.8%), and zero frozen stale states. Verified standby container `warded-model` reachability within `core-network`. Verified automated Circuit Breaker reset and live telemetry recovery upon worker restoration. Runbook documented in `docs/runbook_dev_box_failover.md` and empirical proof in `docs/reports/tailscale_failover_drill_report.md`.
    - [#214](https://github.com/529-studio/projectKV/issues/214): S5-T4 — `[infra + web] S5-T4 — R2 blob storage for archiver + shared team labeler` (**Completed / Verified**): Implemented storage `List` abstraction across Local and R2 drivers, backend archiver proxy streaming and label persistence endpoints, and built collaborative monochromatic Next.js team web labeler (`apps/web/src/app/labeler`).
  - Operational wave guide and `agy` CLI instructions compiled in [`docs/sprint5_execution_guide.md`](sprint5_execution_guide.md).
- **Sprint 6 Completion & Reality-Check Audit (ADR-007)** (**Completed / Closed**):
  - Authoritative sprint completion report, empirical reality check, and technical verification matrix compiled in [`docs/sprint6_report.md`](sprint6_report.md).
   - Five core backlog tickets completed, verified, and merged to `dev`:
    - [#221](https://github.com/529-studio/projectKV/issues/221): S6-T1 — `[cv-engine + tooling] Implement NMS, Letterbox, 416px Scaling & Re-run 764-Frame D6 Benchmark` (**Completed / Merged in PR #232**): Implemented aspect-preserving `letterbox` (16:9, neutral gray padding `(114, 114, 114)`) and `nms_cpu` ($\text{IoU} \le 0.45, \text{conf} \ge 0.25$) in `services/cv-engine/server.py`. Upgraded model input resolution to $416\times 416$ OpenVINO FP16 (`[1, 3, 416, 416]` input tensor, `[1, 84, 3549]` output tensor). Re-ran the complete 764-frame real benchmark: Traffic Macro F1 improved from **0.206 to 0.245** (+18.9%), Traffic Weighted F1 improved from **0.351 to 0.402** (+14.5%), CONGESTED false alarms on CLEAR frames dropped by **50.0%** (from 46 frames down to 23), and MODERATE correct predictions tripled (21 to 63, +200%). All 10 unit tests pass (`make test-model`). Hard Gate satisfied, unblocking downstream S6-T2 traffic calibration. Full report committed in [`docs/reports/eval_report_d6_sprint6.md`](reports/eval_report_d6_sprint6.md).
    - [#222](https://github.com/529-studio/projectKV/issues/222): S6-T2 — `[go-server] Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage` (**Completed / Merged in PR #233**): Calibrated physical corridor capacities in `cameras_enriched.json` (45.0–50.0 PCU for 10–12 lane expressways, 40.0 PCU for QL1, 25.0–30.0 PCU for 4–6 lane arterials, 15.0 PCU narrow fallback). Upgraded `cvclient.PCUCalculator` to consume per-camera `base_capacity` and created SQLite migration `0005_add_camera_capacities.sql` with hand-written upsert SQL in `store.CapacityRepo` to persist learned peak capacities across container restarts. Verified under Go race detector (`make test-go`) and operational simulation resolving false-positive `HEAVY` on Võ Văn Kiệt - Ký Con (10.2 PCU $\rightarrow$ SI 0.2267 $\rightarrow$ `CLEAR`).
    - [#223](https://github.com/529-studio/projectKV/issues/223): S6-T3 — `[web] CameraModal UI/UX Redesign (Tactile Affordance) & 15s Client-Side Cooldown` (**Completed / Merged in PR #236**): Redesigned `[Xác minh ngập nước]` from passive chip styling into a distinct tactile monochromatic action button (`bg-zinc-100 text-zinc-950 hover:bg-white active:scale-[0.98]`) with crisp monochromatic SVG reticle scan icon (`⌖`). Removed square brackets `[...]` from action buttons (reserving brackets strictly for passive status badges). Visually separated the action control row from the passive verdict badge row for clear visual hierarchy. Implemented 15-second client-side rate limit cooldown countdown with module-level persistence preventing duplicate network requests and quota exhaustion. Hardened security and UX under Option A: shielded Gemini API credentials into HTTP headers (`x-goog-api-key`), sanitized backend HTTP 503 errors, and concealed technical exception dumps from end users with a subtle monochromatic zinc chip `[Tạm thời gián đoạn]` auto-dismissing in 4s while preserving dev logs in `console.warn`. Passed 100% of web test suite (`npm test`, 114 tests), Go race detector (`make test-go`), and clean Next.js static export build. Follow-up PR #237 & #238 implemented `SecretSanitizer` middleware and Schmitt trigger hysteresis deadband (`HysteresisDelta = 0.05`).
    - [#224](https://github.com/529-studio/projectKV/issues/224): S6-T4 — `[ci + ops] Automated Secret Scanning in GitHub Actions CI (Gitleaks)` (**Completed / Merged in PR #243**): Integrated centralized Gitleaks secret scanning workflow `.github/workflows/secret-scan.yml` using `529-studio/.github/.github/workflows/reusable-secret-scan.yml@main` with `fail-on-leak: true` and scoped project allowlist in `.gitleaks.toml`. Blocks any PR or push introducing secrets into `main` or `dev`.
    - [#225](https://github.com/529-studio/projectKV/issues/225): S6-T5 — `[go-server] Telegram Flood Alert Dispatcher (internal/notify, Subscription Store & Debounce)` (**Completed / Merged in PR #241**): Built notification subsystem in `services/go-server/internal/notify/` following leaf modulith pattern with `Channel` interface and `Manager` orchestrator. Implemented `TelegramChannel` utilizing official Telegram Bot API with clean HTML formatting, strictly zero emojis per Rule 1, bilingual `vi`/`en` presentation, and secret token scrubbing. Implemented SQLite subscription store in `services/go-server/internal/subscription/` (`0006_flood_subscriptions.sql`) supporting `all`, `chokepoint`, `zone`, and `ward` targets with HTTP endpoints (`POST`, `GET`, `DELETE /api/subscriptions`). Enforced thread-safe 30-minute sliding debounce window per chokepoint to prevent alert spam. Wired verified flood verdicts from `flood.Service` and Hot Set scheduler into `notify.Manager`. Added end-to-end integration test (`notify_integration_test.go`), regenerated Swagger documentation (`make swagger`), verified live curl operations, and verified 100% test pass under Go race detector (`make test-go`).
   - Explicit architectural deferral: Route A→B deferred pending perception ground-truth verification and dedicated routing engine spike. All 5 active backlog tickets delivered.
  - Operational multi-wave guide and `agy` CLI instructions compiled in [`docs/sprint6_execution_guide.md`](sprint6_execution_guide.md).
- **Real-Time Environmental Frame Harvesting (2026-10-13 Rain Event)**:
  - Exploited active afternoon rainfall across HCMC (`rain: 0.10 mm`, `weather_code: 51`) to capture fresh real-world wet/rain frames across 10 authentic municipal flood chokepoints and flood-prone corridors: **201 real frames** collected (97.1% success rate).
  - Generated dual-format ground-truth labels using OpenVINO YOLOv8n FP16 416x416 inference at 31.2 frames/sec + visual road surface verification (`tools/eval/label_today_rain.py`): Traffic (CLEAR=62, MODERATE=66, HEAVY=31, CONGESTED=42), Flood (NONE=133, SHALLOW=68, DEEP=0), Weather (`rain`=201, 100%), Time of day (`day`=201, 100%).
  - Synchronized both 201 JPEGs and 201 JSON labels to Cloudflare R2 bucket `warded-proofs` (`R2:warded-proofs/2026-10-13/` and `R2:warded-proofs/labels/2026-10-13/`). Full report in [`docs/reports/rain_collection_report_2026-10-13.md`](reports/rain_collection_report_2026-10-13.md).
- **Sprint 7 Completion, Staging Review & Continuous Data Flywheel (ADR-008 & ADR-009)** (**Completed / Audited**):
  - Authoritative sprint completion report, empirical reality check, and technical verification matrix compiled in [`docs/sprint7_report.md`](sprint7_report.md).
  - Core delivered features and pull requests merged to `dev`:
    - [#247](https://github.com/529-studio/projectKV/issues/247): S7-T2 — `[web] Hide Offline / No-Signal Cameras by Default on Map` (**Merged in PR #257**): Filtered 125 offline cameras (`NOT_IMAGE`/`NO_SIGNAL`) by default with an active monochromatic header toggle `[Ẩn/Hiện camera mất tín hiệu]`.
    - [#253](https://github.com/529-studio/projectKV/issues/253): S7-T8 — `[tooling] Multi-Lane Directional Labeling Taxonomy & Toolchain Upgrade` (**Merged in PR #258**): Draft-07 multi-lane directional schema (`label_schema.json`), physical depth anchors (`dry`, `wet_asphalt_no_standing`, `tire_base_under_10cm`, `tire_half_submerged`, `impassable`), and CLI validator `validate_labels.py`.
    - [#248](https://github.com/529-studio/projectKV/issues/248): S7-T3 — `[web] Human-in-the-Loop "Báo cáo ngập tại đây" Button` (**Merged in PR #259**): High-contrast tactile action button with 30s module cooldown and confirmation toasts in `CameraModal.tsx`.
    - PR #260: `feat(eval): Curate 2026-10-16 Real Camera Dataset & Gold-Standard Labels` (**Merged in PR #260**): 688 curated live camera frames uploaded to Cloudflare R2, 72 new gold-standard labels generated, expanding gold benchmark to 207 verified labels.
    - [#261](https://github.com/529-studio/projectKV/issues/261): S7-T9b — `[docs] Revise ADR-009: Autonomous Continuous Data Flywheel & Reconcile S7-T7 VLM Deprecation` (**Merged in PR #265**): Codified zero-VLM architectural directive ($111/mo saved) and dual-trigger continuous data flywheel.
    - [#250](https://github.com/529-studio/projectKV/issues/250): S7-T5 — `[go-server] Autonomous 50 Hot Set Traffic Poller & Saturation Cache` (**Merged in PR #266**): Built `internal/hotset` background poller, in-memory store, and `GET /api/v1/cameras/hotset/status`.
    - [#251](https://github.com/529-studio/projectKV/issues/251): S7-T6 — `[web] Real-Time Arterial Marker Saturation Coloring (Green/Yellow/Red Dots)` (**Merged in PR #267**): Real-time marker coloring, 30s poller hook, CSS pulsating animation, monochromatic footer legend, and Trust & Safety status degradation (Lesson 38).
    - [#262](https://github.com/529-studio/projectKV/issues/262): S7-T7b — `[go-server] internal/harvester Autonomous Dual-Trigger Poller, Label Generator & Cloudflare R2 Archiver` (**Merged in PR #268**): Built leaf package `internal/harvester` with scheduled peak harvest (07:30, 12:00, 17:30 ICT across 57 Hot Set cameras), meteorological rain watcher (Open-Meteo $\ge 0.10\text{ mm/h}$ across 207 flood corridor cameras with 60m sliding cooldown), Draft-07 in-memory validation, and concurrent R2 streamer. Verified 100% on staging and approved by `@giangdq202`.
  - Reconciled architecture: S7-T7 VLM bridge formally superseded by ADR-009 / S7-T7b.
  - Deferred backlog items carried into post-Sprint 7: S7-T1 (Staging Telegram bot token deployment). S7-T4, S7-T7c, S7-T7d, and S7-T9 completed.
- **Sprint 8 Active Execution & Flood Integrity Epic (`epic:flood-integrity`, `epic:eval-tooling`, `rfc`)**:
  - [#269](https://github.com/529-studio/projectKV/issues/269): S8-T1 — `[web + go-server] Doppler Radar Rain Pipeline Overhaul & Flood Desynchronization Fix` (**Merged in PR #272**): Enforced Zoom 7 radar tile space resolving silent 200 OK gray placeholder degradation in HTML5 canvas; calibrated Universal Blue Scheme 2 palette decoding light rain, moderate rain, and heavy convective cores across 85 H3 cells; added dual TTL reaper (30s) to `FloodHexAggregator` in `services/go-server/internal/live/hub.go`; resolved UI semantic desynchronization between left sidebar hero and top lens toolbar with distinct sky-blue `[N ướt]` badge and dynamic flood/wet marker prominence.
  - [#249](https://github.com/529-studio/projectKV/issues/249): S7-T4 / S8-T2 — `[go-server] internal/feedback Leaf Package & Cloudflare R2 Ground-Truth Archiver` (**Merged in PR #273**): Built pure leaf package `services/go-server/internal/feedback/` (0 sibling imports) with thread-safe sliding rate limiter (`sync.RWMutex`, max 1 report per IP per camera per 5 minutes, returning HTTP 429 + `Retry-After`), SHA-256 client IP privacy hashing, asynchronous live camera frame fetching from `proxycache` with upstream fallback, and Cloudflare R2 ingestion under `feedback/<YYYY-MM-DD>/<camera_id>_<unix_ts>.jpg` and `.json`. Exposed `POST /api/v1/cameras/:id/report-flood` and legacy alias `POST /api/cameras/:id/report-flood`.
  - [#263](https://github.com/529-studio/projectKV/issues/263): S7-T7c / S8-T3 — `[tooling] Standalone Automated Harvester CLI (tools/eval/auto_harvest.py)` (**Merged in PR #274**): Built standalone Python CLI reproducing the autonomous data flywheel offline/on-demand without running Go server. Supports `--trigger [scheduled|rain|now]`, `--scope [hotset|flood-prone|all]`, `--weather [auto|dry|rain|heavy_rain]`, `--upload-r2`, `--validate`, and `--dry-run`. Strict Draft-07 schema compliance against `label_schema.json` and zero hardcoded secrets. 100% unit test pass rate (`auto_harvest_test.py`).
  - [#264](https://github.com/529-studio/projectKV/issues/264): S7-T7d / S8-T4 — `[web] Web Labeler Candidate Review Filter & Auto-Harvest Verification Badges` (**Merged in PR #275**): Built tri-state candidate review filters (`all`, `pending_auto`, `approved_human`), monochromatic provenance badges, and `Shift + Enter` fast-approval hotkey with 100% test pass rate (155 tests).
  - [#254](https://github.com/529-studio/projectKV/issues/254): S7-T9 — `[docs + spike] ADR-009 Technical Design Spike: Self-Hosted Flood Segmentation (Zero-VLM)` (**Completed / Verified**): Authored comprehensive technical specification in [`docs/ADR-009-zero-vlm-flood-segmentation.md`](ADR-009-zero-vlm-flood-segmentation.md). Benchmarked YOLOv8n-seg, BiSeNet v2, and SegFormer-B0 on AMD Ryzen 7 6800HS (2 vCPU / 2 GB RAM slice) using OpenVINO FP16 (24.6 ms neural latency, 38.5 ms frame cycle vs. 80 ms budget), defined dataset acquisition protocols via R2 crowdsourced proofs, and specified dual-layer physical triangulation logic (water surface ratio $\ge 15\%$ + motorcycle wheel submergence depth $\ge 20\text{ cm}$) per Lesson 48.
  - [#287](https://github.com/529-studio/projectKV/issues/287): S8-T5 — `[go-server] Harvester Semaphore Deadlock & Labeled Loop Break` (**Merged in PR #292**): Fixed Go concurrency bug in `services/go-server/internal/harvester/pipeline.go` where bare `break` inside `select` caused semaphore token leakage and shutdown deadlocks; replaced with labeled loop break (`break harvestLoop`); deprecated weather-based flood/depth guessing in `pipeline.go` and `tools/eval/auto_harvest.py`, defaulting rain to `UNKNOWN` pending vision/human verification; updated Draft-07 schema and tests (Lesson 52).
  - [#288](https://github.com/529-studio/projectKV/issues/288): S8-T6 — `[go-server] Holistic Environment Configuration Sanitization & Fail-Fast Observability` (**Completed / Verified**): Built unified sanitization helpers in `services/go-server/internal/config/config.go` systematically stripping surrounding quotes (`"`, `'`) and whitespace across 25+ parameters; added `math.IsNaN`/`IsInf` defensive guards; replaced silently discarded errors with structured `slog.Warn` logs emitting key, raw_value, default, and error; verified with comprehensive matrix test suite `TestLoadConfig_SanitizationAndQuotedVariables` (Lesson 54).
  - [#289](https://github.com/529-studio/projectKV/issues/289): S8-T7 — `[go-server + web] Crowdsource Runtime Bridge — Wire internal/feedback to Live Map & Hex Aggregator` (**Completed / Verified**): Closed the crowdsource black hole by exposing observer hook `OnReportAccepted` in `internal/feedback` without violating leaf modulith isolation; bridged hook in `cmd/server/main.go` to `liveHub.BroadcastUserFloodReport` (SSE `user_flood_report`) and `floodHexAggregator.OnUserReport`; tracked sliding 15-minute TTL per camera and escalated marker prominence when >= 2 distinct IP hashes report flood within 10 minutes; promoted map markers and camera modal to high-contrast amber state `[Báo cáo từ người dân: Có ngập]` with real-time countdown; verified with 100% Go race detector and 159 web client unit tests (Lesson 55).




- **Inference Runtime**:
  - Model: Stock COCO YOLOv8n OpenVINO IR 416x416 FP16 (~6.3 MB) with NMS ($\text{IoU} \le 0.45, \text{conf} \ge 0.25$) and aspect-preserving letterboxing
  - Latency: ~13–30ms per frame on host CPU, ~425ms P50 on non-AVX Celeron hardware; 57 cameras polled every 30s (~1.90 FPS, well within 2.27 FPS capacity).
  - Traffic Scoring: Passenger Car Unit (PCU) Saturation Index ($SI = PCU_{current} / PCU_{capacity}$), mapping into `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`.
  - Archiver Direct Frame Delivery (Issue #182): Archiver pipeline directly saves the analyzed frame (`Verdict.RawFrame`) forwarded from the inference analyzers (`cvengine`, `cloudvision`, `mock`), eliminating reliance on viewer proxy cache hits while preserving fallback to `proxycache` and mock frames.
  - Container Vehicle Class Resolution (Issue #186, ADR-005 T6): Resolved dead code gap where COCO model only outputs 4 vehicle classes. Per `docs/decision-container-class-gap.md`, Container is folded into heavy freight vehicle weight (`Truck = 2.0`), aligning with COCO ground truth and HCMC peak-hour freight restrictions.
- **Weather Service**:
  - Open-Meteo current & hourly forecast integrated with 5-minute in-memory cache and stale fallback.
  - Aligned to target hour bucket (`truncateToHour`) to resolve 15-minute `current.time` vs hourly `hourly.time` divergence (#172).
  - 48-hour upstream lookahead buffer (`forecast_days=2`) enabling rolling 12-hour forecast windows spanning past midnight.
- **Signal Separation**:
  - Independent layer toggles: Ground-truth camera-verified flood hexes (`HexOverlay.tsx`) are completely separated from Doppler weather radar reflectivity (`RainRadarLayer.tsx`).
- **GitHub Actions Compute (Oct 2026 Audit, #173)**:
  - Consolidated the standalone `go-ci.yml` into the `ci.yml` `verify-backend` job, removing duplicate Go verification on every backend PR.
- **Distributed Worker Architecture (`dev-box` on Endpoint 4)**:
  - Offloaded heavy OpenVINO YOLOv8n inference from edge server `homesrv` (Intel Celeron N2930, 4 cores, no AVX2) to dedicated worker VM `dev-box` (`100.118.7.57` via Tailscale, AMD Ryzen 7 6800HS, 8 vCPU, 8 GB RAM, strictly quota-capped at 20 GB disk).
  - Deployed container `warded-cv-engine` via Portainer Stack 17 (`docker-compose.cv.yml`) on Endpoint 4 with strict cgroup limits (`cpus: "2.00"`, `memory: "2048M"`).
  - Automated Disk Guard daemon (`/usr/local/bin/docker-disk-guard.sh`) installed via root cron, purging build cache and untagged images whenever root disk usage exceeds 80% (16 GB ceiling).
  - Comprehensive empirical stress tests:
    - Hot Set (50 cameras): Completed batch in 2.24s (22.29 FPS, 44.8ms/camera), 0 errors.
    - Citywide (708 cameras): Completed batch in 30.77s (23.01 FPS, 43.4ms/camera), 0 errors, container memory stabilized at 131 MB RAM (6.4% of 2 GB limit).
- **Decoupled CI/CD Pipeline (PR feat/distributed-cv-cicd-pipeline)**:
  - `ci.yml`: Added path filter `model` (`services/cv-engine/**`) and automated Python test job `verify-model` (`make test-model`).
  - `docker-publish.yml`: Independent build and deployment targeting:
    - Stack 14 on Portainer Endpoint 3 (`homesrv`) for backend/frontend changes.
    - Stack 17 on Portainer Endpoint 4 (`dev-box`) for CV engine changes.
    - Post-deploy verification checking both public web health (`https://warded-stg.529studio.site/health`) and remote Portainer container runtime state (`warded-cv-engine` healthy).
- **Cloudflare R2 Object Storage Integration (`warded-proofs`, PR `feat/r2-storage-integration`)**:
  - Implemented `R2Storage` satisfying `storage.Storage` interface using official AWS SDK for Go v2 (`s3.NewFromConfig`).
  - Mandatory S3 Path-Style addressing enabled (`UsePathStyle = true`) for compatibility with Cloudflare R2 (`https://bb67574163a065e4aad475fb3aa6bb92.r2.cloudflarestorage.com`).
  - Configurable storage driver via `STORAGE_DRIVER` (`local` default for local dev, `r2` / `s3` for staging and production).
  - Standardized snapshot object key path to Vietnam ICT (`Asia/Ho_Chi_Minh`, UTC+7): `YYYY-MM-DD/<camera_id>/<unix_timestamp>_<reason>.jpg`.
  - Public Edge CDN delivery integration via `https://file.529studio.site`, unblocking collaborative web labeling workflows for the entire engineering team.
  - Implemented and verified standalone CLI verification commands in Go (`services/go-server/cmd/verify-r2`) and Python (`tools/storage/verify_r2.py`), confirming 100% upload, head, get, edge CDN (HTTP 200), and cleanup success against live Cloudflare R2.
- **Decoupled Hybrid Flood Pipeline (Sprint 5 Task S5-T3, Issue #209)**:
  - Curated 10 authentic municipal flood hotspots in `tools/eval/fixtures/flood_chokepoints.json` based on official Sở Xây dựng TP.HCM and UDI flood monitoring bulletins, with zero overlap with the 10 traffic smoke cameras.
  - Reconciled rainfall threshold divergence across frontend (`rainHexUtils.ts`) and backend documentation at 1.0 mm (wet pavement onset), 4.0 mm (shallow flood risk), and 15.0 mm (deep flood risk). Corrected documentation discrepancies cited in ADR-006 Finding 3.
  - Implemented dual-trigger VLM dispatch subsystem under leaf modulith package `services/go-server/internal/flood`:
    1. *Trigger 1 (Environmental)*: Background polling loop inspecting flood chokepoints only during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$ or active rain WMO codes), consuming exactly 0 VLM requests during dry weather.
    2. *Trigger 2 (User On-Demand)*: Commuter-initiated verification via `POST /api/flood/verify?camera_id=...` action button in the camera modal.
  - Enforced strict in-process rate limiting (10 RPM sliding window) and daily quota budgeting (300 RPD calendar day rollover) via thread-safe `QuotaLimiter`.
  - Enhanced web client `CameraModal.tsx` with a monochromatic `[XÁC MINH NGẬP NƯỚC]` action button, loading spinner, and bilingual zero-emoji status badges (`[XÁC MINH VLM: KHÔ RÁO]`, `[XÁC MINH VLM: ĐƯỜNG ƯỚT]`, `[XÁC MINH VLM: NGẬP NÔNG]`, `[XÁC MINH VLM: NGẬP SÂU]`).
  - Regenerated Swagger API documentation via `make swagger` (`/api/flood/verify`, `/api/flood/chokepoints`, `/api/flood/quota`).
  - Verified 100% test coverage across Go backend (`./internal/flood/...`, `./...`) and web client (93 unit tests).
- **R2 Blob Storage Archiver & Shared Team Labeler (Sprint 5 Task S5-T4, Issue #214)**:
  - Extended core `storage.Storage` interface with object listing method `List(ctx, prefix) ([]Item, error)` implemented across both `LocalStorage` (filesystem walk) and `R2Storage` (`s3.ListObjectsV2` pagination).
  - Implemented secure backend archiver HTTP routes in `services/go-server/internal/archiver`:
    - `GET /api/archiver/dates`: returns sorted partition dates (e.g. `["2026-10-11", "2026-10-10"]`).
    - `GET /api/archiver/snapshots`: browses snapshot items with metadata, pagination, and camera filtering.
    - `GET /api/archiver/snapshots/raw`: streams raw JPEG binary with immutable cache headers and strict path traversal validation (`ErrInvalidPath`), bypassing Cloudflare Access Zero Trust redirects.
    - `POST /api/archiver/labels` and `GET /api/archiver/labels`: saves and aggregates ground-truth JSON annotations directly in storage (`labels/{date}/{camera}_{annotator}_{timestamp}.json`).
  - Built monochromatic collaborative Next.js web labeler at `apps/web/src/app/labeler/page.tsx` with high-contrast UI, bilingual support (`vi` default), zero emojis, and rapid-fire hotkeys (`1-3` flood, `Q-W-E-R` traffic, `A-S-D` weather, `Z-X-C` time of day, `Enter` save/next).
  - Enhanced standalone evaluator tool `tools/eval/labeler.html` with backend API synchronization bar (`Load from API`).
  - Adoption of R2 blob storage explicitly justified for collaborative team labeling friction elimination, **NOT** because system reached the ADR-004 §D7 disk capacity limit.
  - Implemented multi-tier Developer Access Token gatekeeper (`middleware.RequireDevAuth`) protecting `/labeler` and `/api/archiver/*` against data poisoning and unauthorized access on production:
    - Backend: Supports `X-Dev-Token`, `Authorization: Bearer`, and `?token=` / `?dev_token=` query params (for raw `<img>` binary streaming), validated via `crypto/subtle.ConstantTimeCompare`.
    - Fail-Closed: Automatically returns `503 Service Unavailable` on production/staging if `DEV_ACCESS_TOKEN` is unset or blank.
    - Frontend: In-place monochromatic lock screen at `/labeler`, session persistence via `sessionStorage`, and automatic token eviction upon receiving `401 Unauthorized`.
  - **Partition Discovery Delimiter Optimization (Issue #279, KV-14)**:
    - Extended core `storage.Storage` interface with shallow `ListDirectories(ctx, prefix) ([]string, error)` implemented across `LocalStorage` (`os.ReadDir`) and `R2Storage` (`s3.ListObjectsV2` with `Delimiter: "/"` and `CommonPrefixes` pagination).
    - Refactored `archiver.Handler.ListDates` to invoke `ListDirectories` instead of full-bucket `List`, slashing API latency from ~160 seconds (186+ sequential HTTP requests across 186k+ objects) down to < 800ms (single S3 request) and eliminating Cloudflare 524 timeouts.
    - Enhanced standalone verification tool `cmd/verify-r2` (Test 7/7) confirming sub-second partition directory discovery against live Cloudflare R2 bucket `warded-proofs`.
- **Offline Camera Map Hygiene & Live Counter Badge (Sprint 7 Task S7-T2, Issue #247)**:
  - Implemented client-side filtering in `apps/web/src/components/CameraFilter.tsx` and `apps/web/src/app/page.tsx` hiding 135 offline / no-signal cameras (`status === 'NOT_IMAGE'` / `'NO_SIGNAL'`) by default on the interactive Leaflet map.
  - Added monochromatic toggle control (`[Ẩn camera mất tín hiệu]` / `[Hiện camera mất tín hiệu]`) and live dynamic counter badge (`Đang hiển thị 702 / 837 camera`).
  - Persisted user toggle state in `localStorage` (`warded_hide_offline_cameras`).
  - Maintained complete integrity of underlying camera catalog (`cameras_enriched.json`) and non-destructive view-layer filtering.
- **Directional Labeling Taxonomy, Physical Flood Markers & R2 Proofs Sync (Sprint 7 Task S7-T8, Issue #253)**:
  - Formulated formal labeling specification `docs/taxonomy_traffic_and_flood_labeling.md` and architecture decision `docs/ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md`.
  - Solved directional flow asymmetry on divided boulevards by separating Direction A (approaching) and Direction B (receding) with max-severity global map rollup.
  - Eliminated naive street-name guessing for flood; established physical immersion markers (`dry`, `wet_asphalt_no_standing`, `tire_base_under_10cm`, `exhaust_hub_over_15cm`).
  - Upgraded `tools/eval/label_schema.json`, `services/go-server/internal/archiver/handler.go` (`LabelPayload`), and `apps/web/src/app/labeler/page.tsx` with directional inputs, physical depth anchors, and quality tags.
  - Built high-concurrency Go CLI `services/go-server/cmd/sync-proofs` and uploaded all 880 authentic rain frames (41.43 MB) from `2026-10-15` to Cloudflare R2 bucket `warded-proofs`.
  - Built dataset curation tool `tools/eval/label_curator.py` and curated 135 gold-standard labels across 73 priority cameras for `2026-10-15`.
  - **Real Camera Dataset Collection & Curation (2026-10-16)**:
    - Collected 688 live camera frames (34.47 MB) for `2026-10-16` across city cameras into `data/proofs/2026-10-16/`.
    - Synchronized 100% of the 688 frames to Cloudflare R2 bucket `warded-proofs` via `sync-proofs` (0 errors).
    - Upgraded `tools/eval/label_curator.py` with dynamic `--weather` and flood anchor support (`dry`, `wet_asphalt_no_standing`).
    - Curated 72 gold-standard labels across priority arterial corridors and chokepoints for `2026-10-16`, expanding the gold-standard dataset to 207 files (100% compliant with Draft-07 schema).
- **Human-in-the-Loop "Báo cáo ngập tại đây" Button & Tactile Modal Feedback (Sprint 7 Task S7-T3, Issue #248)**:
  - Added secondary tactile monochromatic action button `[Báo cáo ngập tại đây]` / `[Report flood here]` in `apps/web/src/components/CameraModal.tsx` alongside the VLM verification trigger, styled with high-contrast zinc borders (`border-zinc-700 hover:bg-zinc-800 active:scale-[0.98] text-zinc-300`) and a crisp monochromatic alert SVG icon (zero emojis per Rule 1).
  - Enforced 30-second client-side rate limit cooldown countdown (`[Đã báo cáo (30s)]` / `[Reported (30s)]`) with per-camera timestamp tracking in `apps/web/src/utils/cameraModalUtils.ts` persisting across modal close/open and camera switching.
  - Dispatched `POST /api/v1/cameras/:id/report-flood` on click, rendering an ephemeral zinc chip `[Cảm ơn bạn đã hỗ trợ ghi nhận]` auto-dismissing after 3 seconds.
  - Added comprehensive unit tests in `apps/web/src/utils/cameraModalUtils.test.ts` (131 total tests passing) verifying button states, zero-emoji compliance, cooldown math, and camera isolation.
- **Sprint 7 Task S7-T1 Deferral Note (Issue #246)**:
  - Task S7-T1 (`[ci + ops] Live Staging Telegram Flood Alert Verification & Simulation Deprecation`) was evaluated and classified as **nice-to-have** operational verification. To prioritize core perception accuracy, directional labeling taxonomy, and human-in-the-loop crowd-sourcing feedback loops, S7-T1 is deferred to post-Sprint 7 milestones.
- **Autonomous Continuous Data Flywheel & S7-T7 VLM Deprecation (ADR-009 Revision, Issues #261, #262, #263, #264)**:
  - Formally updated ADR-009 with §2.4 Autonomous Continuous Data Flywheel architecture, establishing dual triggers (daily peak schedule at 07:30, 12:00, 17:30 ICT + Open-Meteo meteorological rain watcher with 60m sliding cooldown) and continuous Cloudflare R2 ground-truth ingestion.
  - Closed and superseded Issue #252 (cloud VLM bridge) in favor of on-premise OpenVINO harvester to eliminate cloud API costs ($111/mo saved) and runtime latency.
  - Decomposed implementation into 4 tracked tasks on GitHub and Plane Kanban (https://hub.529studio.site):
    - `[KV-9]` / Issue [#261](https://github.com/529-studio/projectKV/issues/261): S7-T9b — Revise ADR-009 Data Flywheel & Reconcile S7-T7 VLM Deprecation.
    - `[KV-10]` / Issue [#262](https://github.com/529-studio/projectKV/issues/262): S7-T7b — `internal/harvester` Autonomous Dual-Trigger Poller, Label Generator & Cloudflare R2 Archiver.
    - `[KV-11]` / Issue [#263](https://github.com/529-studio/projectKV/issues/263): S7-T7c — Standalone Automated Harvester CLI (`tools/eval/auto_harvest.py`).
    - `[KV-12]` / Issue [#264](https://github.com/529-studio/projectKV/issues/264): S7-T7d — Web Labeler Candidate Review Filter & Auto-Harvest Verification Badges.
- **Autonomous 50 Hot Set Traffic Poller & Saturation Cache (Sprint 7 Task S7-T5, Issue #250, Plane [KV-4])**:
  - Implemented high-performance thread-safe in-memory cache `hotset.Store` in `services/go-server/internal/hotset/store.go` guarded by `sync.RWMutex`, storing camera verdicts and Saturation Indexes.
  - Exposed cached status endpoint `GET /api/v1/cameras/hotset/status` (with backward-compatible alias `GET /api/cameras/hotset/status`) serving all 50 Hot Set arterial camera states with sub-5ms latency (~0.5ms measured, 10x faster than requirement).
  - Extended `hotset.Verdict` with `saturation_index` and `pcu_total` fields, wired into `cvclient.CVEngineAnalyzer` to directly consume PCU Saturation Index computed from OpenVINO YOLOv8n FP16 inference, corridor-specific capacities (S6-T2), and Schmitt trigger hysteresis deadband (S6 Lesson 33).
  - Expanded priority arterial corridors and chokepoints in `cmd/server/main.go` to include Trường Chinh (`truong_chinh`) and Quốc Lộ 1 (`quoc_lo_1`, `ql1`).
  - Updated `services/cv-engine/server.py` to accept both `POST /predict` and `POST /analyze`.
  - Regenerated OpenAPI documentation via `make swagger` and verified full test suite under Go race detector (`go test -race ./...`).
- **Real-Time Arterial Marker Saturation Coloring & Monochromatic Map Footer Legend (Sprint 7 Task S7-T6, Issue #251, Plane [KV-5])**:
  - Implemented real-time dynamic marker saturation styling across the 50 arterial Hot Set cameras on the web explorer map (`apps/web/src/components/MapComponent.tsx`).
  - Added dedicated polling hook `useHotsetStatus` polling `GET /api/v1/cameras/hotset/status` every 30 seconds with automatic `AbortController` cancellation and memory leak prevention.
  - Dynamically styled Leaflet circle markers based on empirical Saturation Index ($SI$):
    - $SI < 0.30$ (`CLEAR`): Emerald green (`#10b981`)
    - $0.30 \le SI < 0.60$ (`MODERATE`): Amber yellow (`#f59e0b`)
    - $SI \ge 0.60$ (`HEAVY` / `CONGESTED`): Rose red (`#ef4444`)
  - Added subtle pulsating SVG stroke animation (`@keyframes marker-congested-pulse` in `globals.css`) on `CONGESTED` markers without triggering DOM re-renders or layout reflows, maintaining smooth 60 FPS panning/zooming.
  - Added monochromatic map footer legend `[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]` (bilingual with English equivalents) and updated expandable map legend in `MapLegend.tsx`.
  - Enriched map tooltips with exact Saturation Index percentage and PCU count.
  - Preserved camera preview modal click functionality across all markers.
  - Added comprehensive unit tests in `apps/web/src/utils/hotsetUtils.test.ts` (144 unit tests passing, Next.js static build verified).
- **Autonomous Dual-Trigger Harvester & Cloudflare R2 Archiver (Sprint 7 T7b, Issue #262, PR #266)**:
  - Created leaf package `services/go-server/internal/harvester/` conforming to `GO_BACKEND_ARCHITECTURE.md` (zero sibling internal imports, strict dependency inversion).
  - Implemented `harvester.go` Coordinator orchestrating `Scheduler`, `Watcher`, and `Pipeline` with injected consumer interfaces (`WeatherProvider`, `SnapshotFetcher`, `VisionPredictor`, `StorageUploader`).
  - Implemented `scheduler.go` with daily peak hour timer (`07:30`, `12:00`, `17:30` ICT / UTC+7) across 57 Hot Set dual-carriageway intersection cameras.
  - Implemented `watcher.go` polling meteorological weather every 5 minutes; triggers rain harvest when rainfall $\ge 0.10\text{ mm/h}$ or rain WMO codes occur, protected by a thread-safe 60-minute sliding window cooldown across flood-prone corridor cameras.
  - Implemented `pipeline.go` with semaphore-bounded worker pool (concurrency 15), JPEG integrity validation (`0xFFD8` SOI marker and $\ge 20\text{ KB}$), local OpenVINO `/predict` inference, directional PCU saturation mapping, physical waterline depth anchor mapping (`dry`, `wet_asphalt_no_standing`, `tire_base_under_10cm`), in-memory Draft-07 schema validation, and concurrent Cloudflare R2 streaming (`proofs/YYYY-MM-DD/<cam_id>/<ts>_sample.jpg` and `labels/YYYY-MM-DD/cam_<cam_id>_<ts>.json`).
  - Added configuration keys `HARVESTER_ENABLED`, `HARVESTER_SCHEDULE_HOURS`, `HARVESTER_RAIN_COOLDOWN_MINUTES`, `HARVESTER_CONCURRENCY` in `services/go-server/internal/config/config.go`.
  - Wired into `cmd/server/main.go` with graceful shutdown lifecycle integration.
  - Comprehensive unit and race test suite in `internal/harvester/harvester_test.go` (`go test -race ./internal/harvester/...`).
- **Doppler Rain Radar Pipeline Overhaul (Sprint 8 Priority 1 S8-T1, Issue #269, ADR-010, Lessons 41 & 43)**:
  - Fixed root cause of city-wide false-positive flood/rain overlays on dry sunny days (Lesson 41).
  - Investigated and resolved live regression where Zoom 8 radar requests returned HTTP 200 with "Zoom Level Not Supported" placeholder graphic, causing 0 rain detections across all cells (Lesson 43).
  - Standardized tile coordinate space on Zoom 7 ($1\text{ px} \approx 1.2\text{ km}$), mapping the entire Ho Chi Minh City metropolitan area cleanly into two tiles at row $Y=60$: `(101, 60)` (Western/Central HCMC) and `(102, 60)` (Eastern HCMC / Thu Duc City).
  - Fully calibrated RainViewer Universal Blue (Scheme 2) RGB reflectivity palette across heavy rain (pink convective core `255, 170, 255`, crimson `193, 0, 0`, vivid red `255, 68, 0`, orange `255, 170, 0`), moderate rain (pure yellow `255, 238, 0`, amber `255, 197, 0`, deep navy `0, 85, 136`), and light rain (sky blue `0, 119, 170`, cyan `136, 221, 238`), with explicit filtering of tan boundary clutter (`206, 192, 135, a=150`).
  - Verified live during real-time rainstorm: restored 85 active rain H3 cells citywide, correctly detecting heavy rain over CCTV *Điện Biên Phủ - Nguyễn Hữu Cảnh* (matching wet roadway pavement).
- **Map Overlay Layout Deconfliction (Desktop & Mobile, Lesson 42)**:
  - Desktop View: Relocated active filter breadcrumb chips (`activeFilterLabel`) from the crowded toolbar row into a dedicated sub-row, preventing horizontal collision with top-right WeatherWidget and MapLegend trigger.
  - Mobile View: Relocated WeatherWidget into the top quick-search header row, eliminated conflicting `top-28 right-3` anchor, added responsive compact button text (`9 / 837 cam`, `[Hiện offline]`), and dynamically elevated bottom arterial status legend when pinned dock is active.
  - Replaced broken alpha thresholding with meteorological RGB dBZ decoder in `rainHexUtils.ts`:
    - $< 18\text{ dBZ}$ (or saturation $< 0.20$, alpha $< 120$): `null` (suppresses ground clutter and virga).
    - $18–29\text{ dBZ}$ (cyan, vibrant teal): `LIGHT_RAIN`.
    - $30–39\text{ dBZ}$ (saturated yellow/amber or deep navy): `MODERATE_RAIN`.
    - $\ge 40\text{ dBZ}$ (orange, red, purple storm cores): `HEAVY_RAIN`.
  - Replaced single-centroid pixel evaluation with 7-point contraction sampling (`getHexSamplingPoints`, 70% radius) and $\ge 3/7$ point quorum check with median rank derivation (`evaluateHexRadarIntensity`).
  - Added Surface Cross-Validation Gate: when Open-Meteo measured rain is $< 0.2\text{ mm}$ with clear skies and ground cameras report dry pavement, airborne echoes are demoted to `ADVISORY_CLOUD` (slate gray, fillOpacity 0.04) or suppressed, preventing street inundation false alarms.
  - Fixed Open-Meteo fallback in `generateOpenMeteoFallbackHexCells` to strictly require actual measured precipitation (`rain_mm >= 1.0`).
  - Decoupled radar types from camera flood observations, defaulted initial page load lens to `'traffic'` (real-time arterial saturation dots), applied translucent atmospheric radar styling, and eliminated all graphic emojis (`⚠`, `✓`) across tooltips per AGENTS.md Rule 1.
  - Full test suite verified: 148 web unit tests passing (`npm test`), Next.js static build (6/6 pages), and Go backend tests passing (`make test-go`).
- **Flood Status Synchronization & Visual Prominence (Lesson 44, Issue #269 followup)**:
  - Synchronized in-memory spatial hex aggregator with backend TTL: added `staleTTL = 180s`, periodic 30s background reaper (`ReapStaleCells()`), and dynamic pruning in `aggregateCellLocked()`, eliminating stale hex cell leaks indefinitely served to the frontend.
  - Decoupled semantic navigation badges in `HexOverlay.tsx`: rose badge `[N]` reserved strictly for standing floods (`FLOODED_SHALLOW` / `FLOODED_DEEP`), sky-blue badge `[N ướt]` on desktop (`[N]` on mobile) for wet pavement, completely hidden when count is 0.
  - Sidebar hero card displays `GHI NHẬN MƯA: N điểm đường ướt (chưa ghi nhận ngập)` with direct action `Xem danh sách N camera đường ướt →`, in 100% synchronization with map verdicts.
  - Elevated active camera markers on `flood` lens: unaffected cameras dimmed (`opacity: 0.20`, `fillOpacity: 0.15`), active flood/wet cameras highlighted in high-contrast cyan (`#38bdf8`) or crimson (`#dc2626`) with thick white stroke, enlarged radius, and top layer sorting (`zIndexOffset(1000)`).
  - All automated test suites passing: 151 web unit tests (`npm test --prefix apps/web`), full Go backend test suite (`go test -race ./...`).
- **Web Labeler Candidate Review Filter & Auto-Harvest Verification Badges (Lesson 47, Issue #264, Kanban [KV-12])**:
  - Tri-state review filtering in `apps/web/src/app/labeler`: added `filterSnapshots()` and `isAutoHarvested()` partitioning frames into `[Tất cả]`, `[Chờ duyệt (Auto)]`, and `[Đã duyệt (Human)]` with live count chips.
  - High-contrast monochromatic provenance badges in metadata header: `[Tự động: Harvester]` for pseudo-labels vs. `[Người duyệt: <tên>]` for human-certified gold standard vs. `[Chưa gán nhãn]` for unannotated frames.
  - Fast-approval hotkey ergonomics: `Shift + Enter` (and primary button) promotes auto-harvested candidate parameters directly to gold standard, stamps annotator attribution with reviewer nickname, persists to `/api/archiver/labels`, and smoothly advances the queue.
  - 100% test pass rate: 155 web unit tests (`npm test --prefix apps/web`), zero ESLint errors, Next.js static production export (6/6 pages).

- **Sprint 8: Wave 2 — Road Surface Water Segmentation Dataset Toolchain & Off-Box Training Pipeline (S8-T8, Issue #290, Kanban [KV-20], ADR-009)**:
  - Standalone dataset export CLI (`tools/eval/export_yolo_seg.py`): Converts authentic camera frames and ground-truth metadata (`tools/eval/labels_gold_standard/`) into standard Ultralytics YOLO segmentation directory structure (`data/yolo_seg/`) with 6 target classes (`roadway`, `water_puddle`, `motorcycle`, `car`, `bus`, `truck`). Normalized coordinates in $[0.0, 1.0]$ and strict `--verify` manifest checks.
  - Off-box training pipeline (`tools/train/train_yolov8_seg.py`): Designed for off-box execution (Google Colab / external GPU workstation) to respect dev-box 20 GB disk ceiling (auto-guard at 16 GB) and 2 GB RAM limits. Automatic host compute headroom checks with dry-run fallback. Exports best checkpoints to OpenVINO FP16 IR (`yolov8n_seg.xml` and `yolov8n_seg.bin`, size $< 10$ MB).
  - CV engine inference upgrade (`services/cv-engine/server.py`): Prioritizes loading `yolov8n_seg.xml`, parses mask prototypes (`output1`, shape `[1, 32, 104, 104]`), and executes Dual-Layer Physical Triangulation (Layer 1: surface water ratio $R_{\text{water}} \ge 0.15$; Layer 2: motorcycle wheel submersion $\delta_{\text{sub}} \ge 0.18 \to \text{FLOODED\_DEEP}$).
  - 100% test pass rate: `make test-model` (14/14 tests in 0.66s), `make test-train` (4/4 tests in 0.06s), `make test-eval` (66/66 tests in 0.33s), and `make test-go` (`-race` all packages pass).

- **Sprint 9: Wave 0 — Segmentation Polygon Annotation Contract & Canvas Editor (S9-T1, Issue #297, ADR-011 D1/D2)**:
  - Backend contract (`services/go-server/internal/archiver`): `LabelPayload.annotations[]` with `PolygonAnnotation{class, polygon [0,1]-normalized vertices, source}`; `SaveLabel` canonicalizes alias classes (`flood → water_puddle`, `road → roadway`, …) in lockstep with the exporter and validates geometry (6-class taxonomy, 3-64 vertices, ≤32 annotations, pixel coordinates rejected). Swagger regenerated.
  - Synthetic geometry deprecation (`tools/eval/export_yolo_seg.py`): metadata-derived polygons are now opt-in (`--allow-synthetic`, stamped `FABRICATED GEOMETRY`); the default skips annotation-less labels with a `skipped_no_annotations` count — closing the geometry-level answer-key pollution hazard (Lesson 58, extends Lesson 39).
  - Schema alignment: `internal/harvester` Draft-07 manifest and `tools/eval/label_schema.json` accept optional `annotations` with the same validation, enabling future model-proposed masks with provenance.
  - Web labeler segmentation mode (`apps/web`): new `PolygonCanvas.tsx` SVG overlay editor (zero new dependencies) — click-to-add vertices, first-vertex/Enter close, drag-to-edit, per-annotation delete, 6-class monochromatic selector, `[Tự động: SAM]` provenance badge, client-side contract validation mirroring the backend, and layout measurement robust to embedded views without `ResizeObserver` (baseline `getBoundingClientRect` + image load + window resize, observer as enhancement).
  - End-to-end empirical verification (Rule 10): real go-server + web dev servers; curl round-trip (alias canonicalization persisted, pixel payload rejected 400); browser E2E drew a real polygon on a live snapshot, saved it (global Enter flow), reloaded and re-rendered it as an editable annotation; `export_yolo_seg.py` produced YOLO txt with the exact drawn polygon and zero synthesized masks (2 train / 1 val / 1 skipped demo). Screenshot evidence captured.
  - 100% test pass rate: `make test-go` (`-race`, all packages), `npm test` 165/165, Next.js build clean (6/6 pages), `tools/eval` 68/68, `gofmt`/`go vet` clean on changed files.
- **Sprint 9 hotfix: Sample Archiver JPEG Quality Gate (Issue #305, ADR-009 gate parity)**:
  - Empirical origin: first overnight autonomous harvest rehearsal (2026-10-25) showed the scheduled harvester correctly rejecting a frozen 2,638-byte upstream placeholder (camera Cong Hoa - Ut Tich 1, 56/57 clean labels) while the sample archiver stored 107 identical junk frames to R2 overnight.
  - `internal/archiver.OnVerdict` now buffers and validates every frame (SOI magic + >= 20,480 bytes, mirroring `harvester.IsValidJPEG` per the leaf modulith boundary) before storage; invalid/placeholder frames are skipped with a structured warning (`camera`, `reason`, `size_bytes`) and never pollute the labeler catalog.
  - Updated contract tests: valid frames archive unchanged; 2,638-byte stub, non-SOI payload, empty frame, and RawFrame-path stub all skipped with zero storage writes (fresh-camera hysteresis case covered).

---


## 4. Local Development Quickstart

```bash
# 1. Start all core web and backend services:
make dev-all
# - Go Backend API:       http://localhost:3000
# - Next.js Frontend Dev: http://localhost:3001

# 2. Start OpenVINO vehicle detection container (requires Colima / Docker):
colima start --cpu 2 --memory 4
make dev-model
# - Healthcheck endpoint: http://localhost:8000/healthz

# 3. Run automated test suites:
make test-go       # Full Go backend test suite with -race
make test-model    # CV Engine segmentation & triangulation test suite (14 tests)
make test-eval     # Ground-truth evaluation & export test suite (66 tests)
make test-train    # Off-box training pipeline test suite (4 tests)
make test-pcu      # PCU Saturation Index boundary test suite
make test-enrich   # Geo-enrichment catalog filter unit tests
cd apps/web && npm test  # Web client unit test suite (159 tests)
```

---

## 5. Non-Negotiable AI Development Guardrails

1. **Grounded Single Source of Truth & Zero-Emoji UX**: All progress, metrics, and architecture claims must be verified directly against git commit history, PR list, or repository files. No Unicode emojis in UI controls, chips, modal headers, notifications, or backend Go log messages.
2. **Sound Solutions & Root-Cause Verification (Never Fear Effort)**: When implementing any issue or task, never shy away from implementation effort or labor. Never choose sloppy, convenient shortcuts, superficial band-aids, or quick hacks out of convenience or fear of complexity. Always investigate and resolve problems at their absolute root cause, architect the soundest, most maintainable solution for the system, and verify exhaustively end-to-end before considering any task complete.
3. **Issue Estimation Mandate**: Before starting any issue or task, AI agents and developers MUST provide an explicit completion time estimate (active effort hours/days and expected delivery window) based on task scope. The estimate must honestly reflect the full effort required for sound, root-cause implementation per Rule 2, never anchoring to premature shortcuts.
4. **End-to-End Bug Reproduction**: Always simulate and reproduce bugs from a real user/network perspective before touching code. Unit tests passing in isolation is never acceptable proof of correctness.
5. **Git Flow & Zero Direct Push**: Zero direct push to `main` or `dev`. Always branch (`feat/*`, `fix/*`, `docs/*`), push, and open PR against `dev`. AI agents must NEVER auto-merge PRs.
6. **Living Documentation & Holistic Knowledge Maintenance**: Updating `docs/project_status.md` and `docs/lesson_learn.md` alone is NOT sufficient. Whenever any task or PR is implemented or merged, developers and AI agents MUST holistically audit and update all related documentation under `docs/` and root `README.md` (both English and Vietnamese sections) if developer workflows, public features, or architecture are affected. Codebase, git history, and PR list are the sole source of truth; never fabricate claims.
7. **Architecture Standards**: Follow leaf modulith pattern in `services/go-server`. Hand-written SQL only (no ORM). Never fetch snapshots directly from frontend; use backend proxy cache. Run `make swagger` on API route changes.
8. **Zero Hardcoded Secrets & Mandatory Environment Configuration**: Never hardcode credentials, tokens, or API keys in code or scripts. All secrets must be dynamically loaded from `.env` or environment variables. Fallback parameters in `os.getenv` or configuration loaders must strictly be `""` (empty string) with fail-fast validation. Documentation templates must strictly use `<placeholder>` syntax.
9. **PR Issue Linking Format Mandate**: PR descriptions must strictly start with `Close #<issue-number>` format to link and auto-close issues upon merge. Never use non-linking phrases like `Addresses Issue #...` or `Implement #...`. Follow `.github/pull_request_template.md`.

