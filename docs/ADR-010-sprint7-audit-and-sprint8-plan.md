# ADR-010: Sprint 7 Audit Reconciliation & Sprint 8 Execution Architecture — Ground-Truth Perception, Concurrency Resilience & Real-Time Alert Dispatch

**Status**: Accepted  
**Date**: 2026-10-23  
**Authors**: ProjectKV Engineering & Architecture Team  
**Auditor**: Senior Technical Auditor & Devil's Advocate  
**Governing Documents**: `AGENTS.md` (Rules 1–9), `docs/GO_BACKEND_ARCHITECTURE.md`, `docs/ADR-004`, `docs/ADR-008`, `docs/ADR-009`  
**Related**:
- [`docs/sprint7_report.md`](sprint7_report.md) (Sprint 7 Audited Completion & Staging Scorecard)
- [`docs/ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md`](ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md) (Autonomous Harvester Architecture)
- [`docs/ADR-009-zero-vlm-flood-segmentation.md`](ADR-009-zero-vlm-flood-segmentation.md) (Zero-VLM Technical Spike & Benchmark)
- [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 38 through 51)
- [`docs/project_status.md`](project_status.md) (Lifetime Status & Architecture Ledger)

---

## 1. Context & Retrospective Reality-Check Audit

### 1.1. Sprint 7 Accomplishments & Foundation
Sprint 7 delivered substantial upgrades across telemetry, data ingestion, and computer vision foundations:
1. **S7-T2 (Issue #247, PR #257)**: Offline camera filtering. 125 offline feeds (`NOT_IMAGE`) hidden by default with an active UI toggle, eliminating commuter ghost modals.
2. **S7-T8 (Issue #253, PR #258)**: Draft-07 multi-lane directional schema (`label_schema.json`) and CLI validator (`validate_labels.py`), resolving divided carriageway traffic conflation.
3. **S7-T3 (Issue #248, PR #259)**: Tactile `[Báo cáo ngập tại đây]` button and 30-second persistent rate-limit cooldown in `CameraModal.tsx`.
4. **Dataset Expansion (PR #260)**: 688 live camera frames captured from 2026-10-16 synced to Cloudflare R2; 72 gold-standard labels curated into `tools/eval/labels_gold_standard/` (total 207 certified labels).
5. **S7-T5 (Issue #250, PR #266)**: Autonomous 57 Hot Set traffic poller (`internal/hotset`) and in-memory cache serving `GET /api/v1/cameras/hotset/status`.
6. **S7-T6 (Issue #251, PR #267)**: Dynamic arterial marker saturation coloring (Green/Yellow/Red) with pulsating congested animation and Lesson 38 trust & safety fallback.
7. **S7-T7b (Issue #262, PR #268)**: Leaf package `internal/harvester` with scheduled peak sweeps (07:30, 12:00, 17:30 ICT), rain watcher triggers, and Cloudflare R2 streaming.

### 1.2. The Unvarnished Reality Check: Auditor Findings
Following Sprint 7 closure, an independent architectural and operational audit identified critical gaps between architectural documentation and runtime truth:

1. **The Zero-Flood Model Reality**:
   - The deployed runtime CV engine (`services/cv-engine/server.py`) runs a stock COCO 80-class model (`yolov8n.xml`). Lines 251–275 only extract vehicle bounding boxes (`car`, `motorcycle`, `bus`, `truck`). The service has **zero flood perception**.
   - In `services/go-server/internal/cvclient/cvengine.go` line 172, the Go backend explicitly records:
     ```go
     FloodSeverity: hotset.SeverityUnknown, // cv-engine v1 does not detect flood
     ```
   - In `cmd/server/main.go`, `CloudVisionAnalyzer` is only instantiated if `cfg.CloudVisionAPIKey != ""`. Because the key is intentionally omitted on staging and production to prevent cloud billing ($111/mo saved) and rate-limit exhaustion, `floodAnalyzer` is `nil`, and live environmental flood polling never runs.
   - **Verdict**: ProjectKV currently has **0% live flood vision perception** in production and staging.

2. **Heuristic Ground-Truth Pollution in Harvester**:
   - In `internal/harvester/pipeline.go` lines 50–61 and `tools/eval/auto_harvest.py` lines 147–156:
     ```go
     func DetermineWaterlineDepth(weather *WeatherInfo) (flood, depth, anchor, weatherCond string) {
         if weather == nil || (weather.RainfallMm < 0.10 && !IsRainWeatherCode(weather.WeatherCode)) {
             return "NONE", "DRY", "dry", "dry"
         }
         if weather.RainfallMm < 5.0 {
             return "NONE", "WET_SURFACE", "wet_asphalt_no_standing", "rain"
         }
         return "SHALLOW", "TIRE_QUARTER_SUBMERGED", "tire_base_under_10cm", "heavy_rain"
     }
     ```
   - Waterline depth calculation is not computer vision. It is a weather forecast lookup table: rainfall $\ge 5.0\text{ mm}$ automatically marks all 207 corridor cameras as `SHALLOW` and `tire_base_under_10cm`, even if roadways are elevated and completely dry. This replicates the heuristic answer key pollution condemned in Lesson 39.

3. **Data Flywheel Disconnect (Missing Segmentation Toolchain)**:
   - Draft-07 schema (`label_schema.json`) and the web labeler (`apps/web/src/app/labeler/page.tsx`) contain only categorical and directional tags. They have **zero polygon or mask annotation capabilities**.
   - The repository has 0 polygon training masks for road surface water. Training YOLOv8n-seg directly from R2 manifests is currently mathematically impossible without a polygon annotation pipeline.

4. **Critical Go Concurrency Bug in Harvester**:
   - In `services/go-server/internal/harvester/pipeline.go` lines 216–220:
     ```go
     select {
     case sem <- struct{}{}:
     case <-ctx.Done():
         break // CRITICAL BUG: only breaks the select, not the for loop!
     }
     wg.Add(1)
     go func(c Camera) {
         defer func() {
             <-sem // Stolen token or permanent deadlock!
             wg.Done()
         }()
     ...
     ```
   - In Go, `break` inside a `select` only breaks the `select`, NOT the enclosing `for` loop. When `ctx.Done()` fires, execution proceeds directly to `wg.Add(1)` and launches a goroutine with `defer func() { <-sem; wg.Done() }()`. Because `sem <- struct{}{}` was skipped, `<-sem` drains another worker's token or blocks indefinitely, deadlocking `wg.Wait()` during container shutdown.

5. **Hardware Constraints on `dev-box`**:
   - `dev-box` (AMD Ryzen 7 6800HS) has a hard 20 GB disk limit. Automated `docker-disk-guard.sh` triggers cleanup at 80% (16 GB ceiling).
   - The Docker container slice on `dev-box` is capped at `--cpus="2.0" --memory="2048m"`. A 2 GB RAM limit causes PyTorch dataloaders to be terminated by the Linux OOM-killer during training.
   - **Architectural Directive**: Training must be executed off-box (workstation / Colab). Only exported OpenVINO FP16 IR artifacts (`yolov8n-seg.xml`, `yolov8n-seg.bin`, ~6.5 MB) may be deployed to `dev-box`.

6. **Crowdsource Black Hole**:
   - `internal/feedback` archives user flood reports to Cloudflare R2, but has zero runtime integration with `FloodHexAggregator`, `hotsetStore`, or `liveHub.Broadcast`. When a commuter reports flooding, the live map remains completely unchanged.

7. **Staging Alert Paralysis (Issue #246)**:
   - Staging (`homesrv` Stack 14) continues to run with `TELEGRAM_BOT_TOKEN=""` in simulation mode. Even if set, because the CV engine outputs `SeverityUnknown`, `dispatchFloodAlert` is never called.

---

## 2. Reconciled Accounting of Delivered Sprint 8 Foundation

Between 2026-10-17 and 2026-10-23, the engineering team executed and merged foundational components addressing operational regressions:

| Component / PR | Target Issue / PR | Delivered Architectural Capabilities |
|---|---|---|
| **Doppler Rain Radar Overhaul** | Issue [#269](https://github.com/529-studio/projectKV/issues/269), PR [#272](https://github.com/529-studio/projectKV/pull/272) | Standardized Zoom 7 coordinate space; RainViewer Scheme 2 RGB dBZ decoding; 7-point contraction sampling; surface cross-validation gate eliminating false alarms on dry sunny days (Lessons 41 & 43). |
| **Crowdsource Feedback Archiver** | Issue [#249](https://github.com/529-studio/projectKV/issues/249), PR [#273](https://github.com/529-studio/projectKV/pull/273) | Built pure leaf package `services/go-server/internal/feedback/` with sliding IP rate limiter (`sync.RWMutex`, 1 report/IP/5m), SHA-256 IP hashing, snapshot fetching, and Cloudflare R2 crowd archiving under `feedback/<YYYY-MM-DD>/`. |
| **Standalone Python Harvester CLI** | Issue [#263](https://github.com/529-studio/projectKV/issues/263), PR [#274](https://github.com/529-studio/projectKV/pull/274) | Built `tools/eval/auto_harvest.py` supporting offline scheduled/rain sweeps, Draft-07 validation, and direct Cloudflare R2 streaming. |
| **Web Labeler Active Learning Queue** | Issue [#264](https://github.com/529-studio/projectKV/issues/264), PR [#275](https://github.com/529-studio/projectKV/pull/275) | Added tri-state candidate review filters (`all`, `pending_auto`, `approved_human`), provenance badges, and `Shift + Enter` fast-certification hotkey. |
| **Zero-VLM Segmentation Spike** | Issue [#254](https://github.com/529-studio/projectKV/issues/254), PR [#276](https://github.com/529-studio/projectKV/pull/276) | Completed technical evaluation in [`docs/ADR-009-zero-vlm-flood-segmentation.md`](ADR-009-zero-vlm-flood-segmentation.md) benchmarking YOLOv8n-seg, BiSeNet v2, SegFormer-B0 on OpenVINO FP16 (24.6 ms latency, 38.5 ms frame cycle vs 80 ms budget). |
| **R2 ListDates Delimiter Optimization** | Issue [#279](https://github.com/529-studio/projectKV/issues/279), PR [#280](https://github.com/529-studio/projectKV/pull/280) | Implemented `ListDirectories` with S3 `Delimiter: "/"`, slashing date partition discovery from 160s (186k objects) down to < 800ms and eliminating Cloudflare 524 timeouts. |
| **Cloudflare Zero Trust Dev Token** | Issue [#281](https://github.com/529-studio/projectKV/issues/281), PR [#282](https://github.com/529-studio/projectKV/pull/282) | Added dual-tier authentication in `middleware.RequireDevAuth` supporting both Cloudflare Zero Trust Access JWT headers (`Cf-Access-Jwt-Assertion`) and local dev token fallback. |
| **Bridge Footing Waterway Exclusion** | Issue [#283](https://github.com/529-studio/projectKV/issues/283), PR [#284](https://github.com/529-studio/projectKV/pull/284) | Filtered bridge footing cameras pointing at river water (`isWaterway: true`), eliminating false-positive flood alerts on dry bridges. |
| **In-Memory Cache & Token Bucket** | Issue [#285](https://github.com/529-studio/projectKV/issues/285), PR [#286](https://github.com/529-studio/projectKV/pull/286) | Added 60s in-memory cache with 25-goroutine pool in `archiver.Handler` (< 10ms reload), Token Bucket 8.0 RPS rate limiting in `batch_auto_label.py`, layout viewport constraint (`100vh - 49px`), and harvester env whitespace sanitization. |

---

## 3. Key Architectural Decisions for Sprint 8 Core Execution

### Decision 1: Immediate Harvester Concurrency & Pipeline Hardening (S8-T5)
1. **Fix `select` Break Concurrency Bug**:
   - Replace the bare `break` in `internal/harvester/pipeline.go` with a labeled loop break:
     ```go
     harvestLoop:
     for _, cam := range cameras {
         select {
         case sem <- struct{}{}:
         case <-ctx.Done():
             break harvestLoop
         }
         ...
     }
     ```
   - Ensure goroutines are never spawned with a cancelled context, preventing semaphore token theft and container shutdown deadlocks.
2. **Deprecate Heuristic Waterline Guessing**:
   - Deprecate `DetermineWaterlineDepth` weather lookup table in `pipeline.go` and `auto_harvest.py`.
   - In auto-harvested labels, set `flood: "UNKNOWN"` and `depth: "UNKNOWN"` unless direct vision inference is available, preventing synthetic ground-truth pollution in R2.

### Decision 2: Holistic Environment Configuration Sanitization & Fail-Fast Validation (S8-T6)
1. **Sanitize String, Numeric and Boolean Parsers**:
   - Upgrade `internal/config/config.go` to systematically strip whitespace and surrounding double/single quotes (`strings.Trim(strings.TrimSpace(val), "\"'")`) across all 25+ environment variables.
2. **Fail-Fast & Structured Warning Observability**:
   - If an environment variable cannot be parsed into its expected type, log a prominent structured warning (`slog.Warn("config: invalid numeric format, using default", "key", key, "raw_value", val, "default", fallback)`) rather than silently swallowing errors.

### Decision 3: Crowdsource Runtime Feedback Loop (S8-T7)
1. **Close the Feedback Black Hole**:
   - Extend `services/go-server/internal/feedback/` with an observer hook: `OnReportAccepted func(cameraID string, report Report)`.
   - Wire this hook in `cmd/server/main.go` to `liveHub.BroadcastUserFloodReport` and `floodHexAggregator.OnUserReport`.
2. **Map Explorer Candidate Prominence**:
   - When a valid commuter report is accepted, the camera marker on the map transitions to an amber candidate state: `[Báo cáo từ người dân: Có ngập]` with a 15-minute sliding TTL.
   - If 2 or more distinct commuter IPs report flooding at the same camera within 10 minutes, escalate to confirmed flood badge.

### Decision 4: Road Surface Water Segmentation Dataset Toolchain & Off-Box Training (S8-T8)
1. **Segmentation Annotation Toolchain**:
   - Create `tools/eval/export_yolo_seg.py` converting R2 proofs and polygon labels into Ultralytics YOLO segmentation format (`images/` and `labels/` with normalized polygon vertices).
2. **Off-Box Training & OpenVINO FP16 Deployment**:
   - Execute model training off-box (Google Colab / external GPU) on curated HCMC rain frames to avoid `dev-box` 20 GB disk and 2 GB RAM OOM termination.
   - Export best checkpoint to OpenVINO FP16 (`yolov8n-seg.xml`, `yolov8n-seg.bin`, ~6.5 MB).
   - Mount model in `services/cv-engine/server.py` and implement dual-layer physical triangulation ($R_{\text{water}} \ge 0.15$ + motorcycle wheel submersion $\ge 20\text{ cm}$) per ADR-009.

### Decision 5: Live Staging Telegram Verification & Activation on `homesrv` Stack 14 (S8-T9)
1. **Resolve Long-Deferred Issue #246**:
   - Configure live `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in Portainer Stack 14 staging environment.
   - Verify live alert dispatch on verified flood events, 30-minute sliding quiet period debouncing, token sanitization in error logs, and bilingual HTML rendering with zero emojis per Rule 1.

---

## 4. Sprint 8 Backlog Prioritization & Quantitative Estimation Matrix

In accordance with **AGENTS.md Rule 2** (Sound Solutions & Root-Cause Verification) and **Rule 3** (Issue Estimation Mandate), the following 5 tickets form the active execution backlog for Sprint 8:

| Priority | Issue / Task | Subsystem | Active Effort | Delivery Window | GitHub Issue & Kanban |
|:---:|---|---|:---:|:---:|:---:|
| **P1** | **S8-T5: Harvester Semaphore Deadlock & Labeled Loop Break** | `services/go-server` | 4h | 0.5 day | [#287](https://github.com/529-studio/projectKV/issues/287) `[KV-17]` |
| **P2** | **S8-T6: Holistic Environment Configuration Sanitization & Fail-Fast Validation** | `services/go-server` | 4h | 0.5 day | [#288](https://github.com/529-studio/projectKV/issues/288) `[KV-18]` |
| **P3** | **S8-T7: Crowdsource Runtime Bridge (`internal/feedback` -> `liveHub`)** | `services/go-server` + `apps/web` | 10h | 1.5 days | [#289](https://github.com/529-studio/projectKV/issues/289) `[KV-19]` |
| **P4** | **S8-T8: Road Surface Water Segmentation Dataset Toolchain & Off-Box Training** | `cv-engine` + `tools/eval` | 16h | 2.5 days | [#290](https://github.com/529-studio/projectKV/issues/290) `[KV-20]` |
| **P5** | **S8-T9: Live Staging Telegram Flood Alert Token Deployment (Closing #246)** | `ci + ops` | 4h | 0.5 day | [#246](https://github.com/529-studio/projectKV/issues/246) `[KV-21]` |

---

## 5. Strict Definitions of Done (DoD) per Deliverable

### S8-T5 (Harvester Concurrency Fix)
- [ ] `internal/harvester/pipeline.go` replaces bare `break` with labeled loop termination (`break harvestLoop`).
- [ ] Unit test `TestHarvester_ContextCancellationCleanShutdown` passes under Go race detector (`go test -race`).
- [ ] Verify 0 goroutine leaks and graceful exit within $< 100\text{ ms}$ upon context cancellation.

### S8-T6 (Config Sanitization & Fail-Fast Validation)
- [ ] `internal/config/config.go` applies quote/space stripping across all string, integer, float, and boolean variables.
- [ ] Parsing errors log explicit structured warnings citing key name, raw input, and fallback default.
- [ ] Comprehensive unit test suite `TestConfig_SanitizationAndQuotedVariables` covering all parameters.

### S8-T7 (Crowdsource Runtime Bridge)
- [ ] `internal/feedback` exposes `OnReportAccepted` hook wired to `liveHub` and `FloodHexAggregator`.
- [ ] Web client renders candidate amber marker `[Báo cáo từ người dân]` with 15-minute sliding TTL.
- [ ] Integration tests pass under Go race detector.

### S8-T8 (Road Water Segmentation Toolchain)
- [ ] `tools/eval/export_yolo_seg.py` exports R2 ground truth into Ultralytics YOLO segmentation format.
- [ ] Exported OpenVINO FP16 model runs on `dev-box` within $\le 45\text{ ms}$ latency and $\le 250\text{ MB}$ RSS.
- [ ] Re-run `make benchmark-real` verifying flood detection sensitivity on 2026-10-13 rain frames.

### S8-T9 (Live Staging Telegram Deployment)
- [ ] `TELEGRAM_BOT_TOKEN` configured in Portainer Stack 14 environment.
- [ ] Live Telegram alert received in test channel on simulated/verified flood verdict.
- [ ] 30-minute quiet period debouncing verified end-to-end. Issue #246 formally closed.
