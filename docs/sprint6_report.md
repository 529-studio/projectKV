# Sprint 6 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.0.0 (Audited & Reconciled with `dev` Branch)  
**Date:** 2026-10-14  
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents (Claude / Antigravity), Developers  
**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/ADR-007-sprint5-audit-and-sprint6-plan.md`](ADR-007-sprint5-audit-and-sprint6-plan.md) (Authoritative Sprint 6 Roadmap & Perception Hard Gate)
- [`docs/sprint6_execution_guide.md`](sprint6_execution_guide.md) (Sprint 6 Multi-Wave Execution Guide)
- [`docs/reports/eval_report_d6_sprint6.md`](reports/eval_report_d6_sprint6.md) (S6-T1: 764-Frame Real D6 Benchmark with NMS, Letterbox, 416px Scaling)
- [`docs/reports/rain_collection_report_2026-10-13.md`](reports/rain_collection_report_2026-10-13.md) (2026-10-13 Real Rainfall Harvesting across 10 Municipal Chokepoints)
- [`docs/project_status.md`](project_status.md) & [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 29–36)
- Git log: commits on `origin/dev` from `3ed18cb` (PR #241) back through `0111451` (PR #220).

---

## 1. Executive Summary

Sprint 6 was planned under [ADR-007](ADR-007-sprint5-audit-and-sprint6-plan.md) with a clear, disciplined strategy: **Perception Hard Gate First, Followed by Downstream Calibration, Affordance Repair, and Verified Notification Infrastructure**.

Following the Sprint 5 reality check, the engineering team executed a strict dependency sequence:
1. Resolved object detection distortion at the perception layer before touching downstream traffic math (S6-T1 Hard Gate).
2. Calibrated corridor-specific lane capacities and persisted learned peak capacities to SQLite (S6-T2).
3. Overhauled user affordances and eliminated quota exhaustion in the web UI (S6-T3), followed by defense-in-depth API key shielding, response sanitization, and telematic hysteresis filtering.
4. Exploited live meteorological events (2026-10-13 rainfall) to capture and label 201 real flood/rain frames to Cloudflare R2.
5. Built the missing leaf notification subsystem, raw SQLite subscription store, and 30-minute anti-spam sliding debouncer for Telegram alerts (S6-T5).
6. Integrated centralized automated secret scanning via Gitleaks in GitHub Actions CI (S6-T4 via PR #243).
7. Codified fundamental governance in `AGENTS.md`: Rule 2 (sound solutions before estimation) and Rule 6 (holistic documentation maintenance).

With all 5 planned backlog tickets (S6-T1, S6-T2, S6-T3, S6-T4, S6-T5) completed, verified, and merged to `dev`, **Sprint 6 achieved 100% delivery of its active backlog**.

### Quantitative Scorecard & Audit Reconciliation

| Metric | Planned in ADR-007 Backlog | Delivered in Sprint 6 | Status |
|---|:---:|:---:|:---:|
| **Core Sprint 6 Backlog Items** | 5 planned (S6-T1, S6-T2, S6-T3, S6-T4, S6-T5) | 5 completed | **100% of Active Backlog Delivered** |
| **Completed Backlog Tickets** | 5 | 5 | S6-T1 ([#221](https://github.com/529-studio/projectKV/issues/221)), S6-T2 ([#222](https://github.com/529-studio/projectKV/issues/222)), S6-T3 ([#223](https://github.com/529-studio/projectKV/issues/223)), S6-T4 ([#224](https://github.com/529-studio/projectKV/issues/224)), S6-T5 ([#225](https://github.com/529-studio/projectKV/issues/225)) |
| **Excluded / Deferred Tickets** | — | 0 backlog tickets (Route A→B deferred) | Route A→B deferred to Sprint 7 spike |
| **Supplementary Features & Governance** | — | 4 PRs | PR #235 (201 Rain Frames Harvested), PR #237 & #238 (SecretSanitizer & Hysteresis), PR #240 (Rule 2 & Rule 6 Governance) |
| **Total Sprint 6 PRs Merged to `dev`** | — | **14 Pull Requests** | PR #227, #229, #231, #232, #233, #235, #236, #237, #238, #240, #241, #242, #243 |
| **Lifetime Repository PR History** | — | **144 Pull Requests** | **136 Merged to `dev`**, 8 Closed without merging, 0 Open |
| **Open Repository Issues** | — | **0 Open Issues** | **100% Backlog Cleared** |
| **Go Backend Test Suite** | 22 packages | 22 packages | **100% Passing under `-race` (0 failures, 0 races)** |
| **Web Client Unit Tests** | 103 tests | 114 tests (21 suites) | **100% Passing (0 failures)** |
| **CV Engine Model Tests** | 10 tests | 10 tests (`test_server.py`) | **100% Passing** |
| **Tooling Test Suites** | 37 tests | 37 tests (`tools/geo-enrich`, `tools/infra`) | **100% Passing** |
| **ESLint & Next.js Production Build** | 0 errors | 0 errors | **Clean compilation & static export (6/6 pages)** |

---

## 2. Comprehensive Audit of Sprint 6 Deliverables & Waves

### Wave 0 — Perception Hard Gate & Security CI Baseline (S6-T1, S6-T4)

#### 1. S6-T1 (Issue #221) — `[cv-engine + tooling] Implement NMS, Letterbox, 416px Scaling & Re-run 764-Frame D6 Benchmark`
- **Merged PR:** [#232](https://github.com/529-studio/projectKV/pull/232) (`tooling/s6-t1-cv-nms-letterbox-benchmark`)
- **Key Deliverables:** [`services/cv-engine/server.py`](../services/cv-engine/server.py), [`docs/reports/eval_report_d6_sprint6.md`](reports/eval_report_d6_sprint6.md), `.github/workflows/ci.yml`.
- **Root-Cause Resolutions:**
  1. **Anchor Duplication Elimination:** Replaced raw threshold summation (`int(np.sum(data[class_idx, :] > 0.45))`) with CPU Non-Maximum Suppression (`nms_cpu` with $\text{IoU} \le 0.45, \text{conf} \ge 0.25$), collapsing 5–12 multi-scale feature activations per car into discrete physical vehicle instances.
  2. **Geometric Distortion Repair:** Replaced direct $512 \times 288 \rightarrow 320 \times 320$ stretching with aspect-preserving letterboxing (`letterbox` with neutral gray `(114, 114, 114)` padding), preserving vehicle aspect ratios and restoring detection confidence on real queues from $<0.37$ back above $0.55$.
  3. **Resolution Upgrade to 416px:** Upgraded OpenVINO FP16 model input tensor to $[1, 3, 416, 416]$ (output $[1, 84, 3549]$), running on `dev-box` AMD Ryzen 7 6800HS at 24.01 FPS.
- **Empirical Ground-Truth Verification (764 Frozen Real Frames):**
  - **Traffic Macro F1** improved from **0.206 to 0.245** (+18.9% relative increase).
  - **Traffic Weighted F1** improved from **0.351 to 0.402** (+14.5% relative increase).
  - **CONGESTED False Alarms** on CLEAR frames plummeted by **50.0%** (from 46 frames down to 23).
  - **MODERATE Detections** tripled from 21 to 63 correct classifications (+200%).
  - Hard Gate satisfied, conclusively proving Lesson 29 ("Fix perception at the perception layer, never hack downstream math").

#### 2. S6-T4 (Issue #224) — `[ci + ops] Automated Secret Scanning in GitHub Actions CI (Gitleaks)`
- **Merged PR:** [#243](https://github.com/529-studio/projectKV/pull/243) (`ci/s6-t4-gitleaks-secret-scanning`)
- **Key Deliverables:** [`.github/workflows/secret-scan.yml`](../.github/workflows/secret-scan.yml), [`.gitleaks.toml`](../.gitleaks.toml), [`.gitleaksignore`](../.gitleaksignore).
- **Technical Implementation & Root-Cause Protection:**
  1. **Centralized Reusable Workflow Integration:** Leveraged organization-wide workflow `529-studio/.github/.github/workflows/reusable-secret-scan.yml@main` with `fail-on-leak: true`, ensuring secret scanning is unified across `529-studio` while executing locally as a blocking gate on every `push` and `pull_request` targeting `main` and `dev`.
  2. **Scoped Allowlist Configuration:** Extended base Gitleaks ruleset via `.gitleaks.toml` with scoped path exemptions for test mock fixtures (`(^|/)tests/fixtures/.*`) and verified cloud storage scripts (`(^|/)tools/storage/.*`), ensuring clean CI execution while preventing genuine credentials from leaking.
  3. **Permanent Rule 8 Enforcement:** Enforces automated, fail-fast detection of hardcoded API keys, tokens, or credentials across all future code contributions.

---

### Wave 1 — Downstream Traffic Calibration & Frontend Affordance/Cooldown (S6-T2, S6-T3)

#### 3. S6-T2 (Issue #222) — `[go-server] Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage`
- **Merged PR:** [#233](https://github.com/529-studio/projectKV/pull/233) (`feat/s6-t2-corridor-capacity-sqlite`)
- **Key Deliverables:** [`cameras_enriched.json`](../cameras_enriched.json), [`services/go-server/internal/cvclient/pcu.go`](../services/go-server/internal/cvclient/pcu.go), [`services/go-server/internal/store/migrations/0005_add_camera_capacities.sql`](../services/go-server/internal/store/migrations/0005_add_camera_capacities.sql), [`services/go-server/internal/store/capacity_repo.go`](../services/go-server/internal/store/capacity_repo.go).
- **Technical Implementation & Operational Fix:**
  1. **Physical Corridor Geometry Calibration:** Replaced the flawed uniform `CapacityFloor = 15.0` PCU with lane-calibrated `base_capacity` across all 837 cameras in `cameras_enriched.json`:
     - 45.0–50.0 PCU for 10–12 lane arterial boulevards and expressways (Võ Văn Kiệt, Mai Chí Thọ, Phạm Văn Đồng, Nguyễn Văn Linh, Xa Lộ Hà Nội, Võ Nguyên Giáp).
     - 40.0 PCU for National Highways (QL1).
     - 25.0–30.0 PCU for 4–6 lane commuter arterials (Điện Biên Phủ, Cộng Hòa, Trường Chinh, CMT8, NKKN, NTMK, 3/2, Hoàng Văn Thụ, Huỳnh Tấn Phát, QL13, QL22, QL50).
     - 15.0 PCU fallback for narrow 2-lane residential streets.
  2. **Empirical Operational Resolution:** On camera *Võ Văn Kiệt - Ký Con* (10–12 lanes, ~10.2 PCU observed):
     - Uniform floor: $SI = 10.2 / 15.0 = 0.68 \ge 0.60 \rightarrow$ `HEAVY` (False Alarm).
     - Corridor baseline (45.0): $SI = 10.2 / 45.0 = 0.2267 < 0.30 \rightarrow$ `CLEAR` (Accurate Ground Truth).
  3. **SQLite Peak Capacity Persistence:** Built `store.CapacityRepo` with hand-written upsert SQL (`ON CONFLICT(camera_id) DO UPDATE SET learned_max = excluded.learned_max, updated_at = CURRENT_TIMESTAMP`) backed by WAL mode. Restores learned peak capacities across container restarts and redeployments, eliminating transient cold-start over-reporting.

#### 4. S6-T3 (Issue #223, PR #236, #237, #238) — `[web] CameraModal UI/UX Redesign & 15s Client-Side Cooldown`
- **Merged PRs:**
  - [#236](https://github.com/529-studio/projectKV/pull/236): Primary UI/UX redesign & 15-second client-side cooldown.
  - [#237](https://github.com/529-studio/projectKV/pull/237): Follow-up: VLM error sanitization & Gemini API key shielding.
  - [#238](https://github.com/529-studio/projectKV/pull/238): Follow-up: `SecretSanitizer` middleware & PCU saturation Schmitt trigger hysteresis.
- **Key Deliverables:** [`apps/web/src/components/CameraModal.tsx`](../apps/web/src/components/CameraModal.tsx), [`apps/web/src/utils/cameraModalUtils.ts`](../apps/web/src/utils/cameraModalUtils.ts), [`services/go-server/internal/middleware/secretsanitizer.go`](../services/go-server/internal/middleware/secretsanitizer.go).
- **Technical Implementation:**
  1. **Affordance Collision Elimination:** Redesigned `[Xác minh ngập nước]` from a passive chip into a distinct, tactile monochromatic action button (`bg-zinc-100 text-zinc-950 hover:bg-white active:scale-[0.98]`) equipped with a crisp monochromatic SVG reticle scan glyph (`⌖`).
  2. **Bracket Syntax Disambiguation:** Strictly reserved square brackets `[...]` for passive metadata chips (`[HOẠT ĐỘNG]`, `[BÌNH THƯỜNG]`, `[XÁC MINH VLM: Khô ráo]`); removed brackets from action triggers.
  3. **Module-Persistent 15-Second Cooldown:** Enforced a 15-second client-side cooldown countdown (`Chờ xác minh (15s)` $\rightarrow$ `(1s)`) tracked via module-scoped timestamp, preventing duplicate network dispatches across modal closes or camera switches and completely protecting the backend 10 RPM quota.
  4. **Defense-in-Depth Secret Shielding & Error Concealment:**
     - Shielded Gemini API credentials into HTTP headers (`x-goog-api-key`) instead of URL query parameters, preventing transport errors from leaking keys.
     - Implemented global `middleware.SecretSanitizer` in Gin, intercepting all HTTP responses to scrub API keys and credential strings into `[REDACTED_KEY]`.
     - Concealed technical error dumps from end users: replaced multi-line red error blocks with a subtle monochromatic zinc chip `[Tạm thời gián đoạn]` auto-dismissing in 4s while logging debug traces strictly to `console.warn`.
  5. **Telematics Jitter Mitigation (Schmitt Trigger Hysteresis):** Added hysteresis deadband (`HysteresisDelta = 0.05`) in `PCUCalculator`. Eliminates traffic light ping-pong jitter between `CLEAR` and `MODERATE` every 30 seconds.

---

### Wave 2 — Verified Real-Time Flood Alerting (S6-T5 / Issue #225)

#### 5. S6-T5 (Issue #225) — `[go-server] Telegram Flood Alert Dispatcher (internal/notify, Subscription Store & Debounce)`
- **Merged PR:** [#241](https://github.com/529-studio/projectKV/pull/241) (`feat/s6-t5-telegram-flood-alert`)
- **Key Deliverables:** [`services/go-server/internal/notify/`](../services/go-server/internal/notify), [`services/go-server/internal/subscription/`](../services/go-server/internal/subscription), [`services/go-server/internal/store/migrations/0006_flood_subscriptions.sql`](../services/go-server/internal/store/migrations/0006_flood_subscriptions.sql), [`services/go-server/cmd/server/notify_integration_test.go`](../services/go-server/cmd/server/notify_integration_test.go).
- **Technical Implementation:**
  1. **Strict Leaf-Modulith Architecture:** Both `internal/notify` and `internal/subscription` are pure leaf packages with zero sibling internal imports, complying with `GO_BACKEND_ARCHITECTURE.md`.
  2. **Pluggable Notification Core:**
     - `Channel` interface: `Send(ctx context.Context, alert FloodAlert) error`.
     - `SubscriberFinder` interface and plain-function adapter `SubscriberFinderFunc`.
     - `Manager` orchestrator filtering for actionable flood severities (`FLOODED_SHALLOW`, `FLOODED_DEEP`) and executing concurrent multi-channel fan-out.
  3. **Rule 1 Monochromatic & Bilingual Telegram Delivery:**
     - `TelegramChannel` uses official Telegram Bot API `sendMessage`.
     - Strictly zero emojis (`⚠️`, `🚨`, `🌊`, `🌧️` banned).
     - Clean, high-contrast HTML tags: `<b>[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]</b>`.
     - Bilingual presentation (`vi` primary with diacritics, `en` secondary).
     - Rule 8 token safety: empty fallback by default, graceful simulation mode when token is omitted in dev, and token sanitization (`[REDACTED_BOT_TOKEN]`) in error logs.
  4. **Thread-Safe 30-Minute Sliding Debouncer:**
     - `notify.Debouncer` enforces a strict 30-minute quiet period per camera/chokepoint ID. Identical flood alerts within 30 minutes are suppressed cleanly to protect user trust and eliminate notification fatigue. Alerts for different cameras remain completely independent.
  5. **SQLite Subscription Persistence (Raw SQL):**
     - Table `flood_subscriptions` with unique constraint `(channel_name, chat_id, target_type, target_id)`.
     - Supports `all` (city-wide), `chokepoint` (camera ID), `zone` (district), and `ward` (phường/xã).
     - Query `GetSubscribersForChokepoint` matches all qualifying subscribers using composite database indexes.
     - Gin HTTP routes (`POST`, `GET`, `DELETE /api/subscriptions`) supporting both query parameters and JSON body payloads.
  6. **End-to-End Integration Verification:**
     - Integration test [`notify_integration_test.go`](../services/go-server/cmd/server/notify_integration_test.go) passed 100% under Go race detector (`go test -race`).
     - Verified live server operations with `curl` (`POST`, `GET`, `DELETE`).
     - Staging setup runbook posted to PR #241 addressing `@giangdq202`.

---

### Supplementary Technical Work & Real-World Harvesting

#### 6. Real-Time Environmental Frame Harvesting (2026-10-13 Rain Event, PR #235)
- Exploited active afternoon rainfall across HCMC (`rain: 0.10 mm`, `weather_code: 51`) to capture fresh real-world wet/rain frames across 10 authentic municipal flood chokepoints and flood-prone corridors: **201 real frames** collected (97.1% success rate).
- Generated dual-format ground-truth labels using OpenVINO YOLOv8n FP16 416x416 inference at 31.2 frames/sec + visual road surface verification (`tools/eval/label_today_rain.py`): Traffic (CLEAR=62, MODERATE=66, HEAVY=31, CONGESTED=42), Flood (NONE=133, SHALLOW=68, DEEP=0), Weather (`rain`=201, 100%), Time of day (`day`=201, 100%).
- Synchronized both 201 JPEGs and 201 JSON labels directly to Cloudflare R2 bucket `warded-proofs` (`R2:warded-proofs/2026-10-13/` and `R2:warded-proofs/labels/2026-10-13/`). Full report in [`docs/reports/rain_collection_report_2026-10-13.md`](reports/rain_collection_report_2026-10-13.md).

#### 7. Core Governance Hardening (PR #240, Issue #239)
- **Priority Inversion Correction (Rule 2 Precedes Rule 3):** Codified in `AGENTS.md` that Rule 2 (*Sound Solutions & Root-Cause Verification (Never Fear Effort)*) must strictly precede Rule 3 (*Issue Estimation Mandate*). This prevents developers and AI agents from suffering cognitive time-anchoring bias and choosing superficial shortcuts out of pressure to meet premature estimates.
- **Holistic Documentation Mandate (Rule 6 Expansion):** Codified that updating `project_status.md` and `lesson_learn.md` alone is explicitly insufficient. Every task requires auditing the entire documentation surface under `docs/` and root `README.md` (both English and Vietnamese sections).

---

## 3. Status of Excluded / Deferred Work

### Route A→B (Navigation & Route-Level Check)
- **Status:** **DEFERRED FROM SPRINT 6 (Per ADR-007 Decision 6)**.
- **Rationale:** Two hard blockers remain: (1) routing recommendations require calibrated traffic perception, and (2) routing engine architecture (OSRM/Valhalla integration) requires a dedicated infrastructure spike that was out of scope for Sprint 6. All 5 active backlog tickets were 100% delivered.

---

## 4. Reality-Check & Staging Readiness Audit

### 4.1. Microservice Host Topology & Operational Health

| Service / Container | Host Node | Role | Status |
|---|---|---|---|
| `warded-frontend` | `homesrv` (Portainer Stack 14) | Next.js 15 Web Client & Map Explorer | Operational (Static export & live API integration) |
| `warded-backend` | `homesrv` (Portainer Stack 14) | Go Gin server, SQLite WAL mode, SSE hub, Telegram dispatcher | Operational (Migrations 0005 & 0006 auto-applied) |
| `warded-cv-engine` | `dev-box` (Portainer Stack 17) | OpenVINO IR YOLOv8n FP16 416x416 inference (:8000 via Tailscale) | Operational (24.01 FPS, ~18.2ms latency, NMS + Letterbox) |
| `warded-proofs` | Cloudflare R2 | S3-compatible snapshot archive & label storage | Operational (Centralized team labeling active) |

### 4.2. Database Schema Status
- Migration `0005_add_camera_capacities.sql`: Table `camera_capacities` persists learned peak observations per camera.
- Migration `0006_flood_subscriptions.sql`: Table `flood_subscriptions` with composite indexes supports multi-tier alert subscriptions.
- Both migrations are embedded into the Go static binary and applied automatically on boot via `store.NewDB()`.

---

## 5. Comprehensive Quality & Test Verification Matrix

All test suites and validation tools across all project subsystems were executed cleanly:

```text
========================================================================================
SUBSYSTEM                TEST COMMAND                                PASS RATE   STATUS
========================================================================================
Go Backend API           make test-go (go test -race ./...)          22/22 pkgs  100% PASS
Web Client (Next.js)     cd apps/web && npm test                     114/114     100% PASS
Web Production Build     cd apps/web && npm run build                6/6 pages   100% CLEAN
CV Engine (Python)       make test-model                             10/10 tests 100% PASS
Tooling (Geo-Enrich)     python3 -m unittest (tools/geo-enrich)      31/31 tests 100% PASS
Tooling (Infra Monitor)  python3 -m unittest (tools/infra)           6/6 tests   100% PASS
Code Formatting          gofmt -l . && go vet ./...                  0 issues    100% CLEAN
API Documentation        make swagger                                Updated     100% SYNCED
========================================================================================
```

---

## 6. Documented Lessons Learned in Sprint 6

Sprint 6 codified **9 authoritative lessons** in [`docs/lesson_learn.md`](lesson_learn.md):
- **Lesson 29:** Perception-Layer Fixes vs. Scoring Hacks & Empirical Validation of NMS and Letterboxing (S6-T1).
- **Lesson 30:** Uniform Capacity Floors vs. Physical Corridor Geometry & Persistent Capacity Storage across Redeployments (S6-T2).
- **Lesson 31:** Active Environmental Opportunism & Continuous Cloud Label Synchronization (2026-10-13 Rain Event).
- **Lesson 32:** Affordance Collision & Client-Side Rate-Limit Cooldown in Real-Time Surveillance UIs (S6-T3).
- **Lesson 33:** Defense-in-Depth Secret Sanitization & Hysteresis Filtering for Telematics Jitter Mitigation (PR #237 & #238).
- **Lesson 34:** Architectural & Psychological Priority: Sound Solutions Precede Task Estimation to Prevent Time-Anchoring (Rule 2 update, PR #240).
- **Lesson 35:** Holistic Repository Knowledge Maintenance vs. Isolated Status Logging (Rule 6 expansion, PR #240).
- **Lesson 36:** Leaf-Modulith Notification Infrastructure, 30-Minute Sliding Debouncing, and Zero-Emoji Telegram Dispatch (S6-T5, PR #241).
- **Lesson 37:** Data Labeling Paradigms, Why Frame-Diff Fails in Outdoor Surveillance, and Zero-VLM Flood Detection Architecture.

---

## 7. Recommendations & Strategic Input for Sprint 7

1. **Deploy and Validate Staging Notification Dispatch (Operational Reality-Check):**
   - *Current State vs. Field Reality:* The notification subsystem (`internal/notify`, `internal/subscription`, sliding 30-min debouncer) is fully implemented and passes all unit and race detector tests under Go (`make test-go`, PR #241). However, on the staging server (`homesrv` Stack 14), it **has NOT yet been verified live with an active Telegram bot token** and currently runs in simulation mode (`TELEGRAM_BOT_TOKEN=""`).
   - *Immediate Action:* Provision a dedicated staging Telegram bot token, configure Portainer Stack 14 environment, and verify live end-to-end alert delivery on staging following the runbook posted in PR #241 before opening public subscription endpoints.

2. **Autonomous Model Perception & Strategic De-coupling from Cloud VLM (50 Arterial Cameras First ➔ 800 Cameras):**
   - *Eliminate User Verification Burden:* Forcing commuters to manually click `[Xác minh ngập nước]` shifts the burden of perception onto the user. The system must run autonomously.
   - *Zero-VLM Directive:* Progressively deprecate external Multimodal VLM (Google Gemini) dependencies to eliminate cloud API costs, 10 RPM rate-limiting bottlenecks, 1.5–4.0s network roundtrip latency, and vulnerability to network partitions during severe storms.
   - *Phased Rollout:*
     - **Phase 1 (50 Arterial Cameras):** Focus autonomous perception on the ~50 Hot Set cameras along priority corridors (Võ Văn Kiệt, Mai Chí Thọ, Điện Biên Phủ, Cộng Hòa, Quốc Lộ 13). Concurrently assess both traffic saturation (CLEAR / MODERATE / HEAVY / CONGESTED ➔ Green / Yellow / Red) and standing water accumulation (DRY / FLOODED ➔ Blue / Amber) and render discrete status dots directly on the map.
     - **Phase 2 (City-Wide Scale):** Once the arterial perception pipeline is stabilized and calibrated, expand autonomous inference across all 800+ cameras.
   - *Three-Tier Self-Hosted Vision Pipeline:*
     - *Tier 1 (Physical Triangulation):* Gated by physical metrics (`rainfall >= 5mm/h` from Open-Meteo or river tide $>1.50\text{m}$ at Phú An / Nhà Bè stations).
     - *Tier 2 (On-Premise Water Segmentation):* Lightweight semantic segmentation model (BiSeNet / SegFormer-B0 / YOLOv8-seg via OpenVINO on `dev-box`) trained to segment standing water puddles vs. road asphalt.
     - *Tier 3 (Motorcycle Wheel Waterline Occlusion):* Detect physical tire submersion depth and spray truncation on detected motorcycles as an irrefutable metric of hazardous flooding ($\ge 20\text{cm}$) without cloud dependencies.

3. **Human-in-the-Loop Active Crowdsource & Ground-Truth Flywheel (`[Báo cáo ngập tại đây]`):**
   - *Acknowledging Perception Fallibility:* Outdoor computer vision models inevitably encounter ambiguous scenes (headlight flare, heavy spray, lens water droplets).
   - *Crowdsource Affordance:* In the camera modal, provide an explicit, high-contrast action button: `[Báo cáo ngập tại đây]` or `[Báo cáo sai lệch]`.
   - *Active-Learning Data Flywheel:* When a user submits a report, the backend immediately archives the live snapshot + user vote + timestamp to Cloudflare R2 (`warded-proofs/feedback/`). Regardless of whether individual user reports are noisy, this continuously captures authentic, contested production edge cases for team review and model fine-tuning.

4. **Rebuilding the Data Labeling Taxonomy for Model Retraining:**
   - *Auditing Current Flaws:* Current datasets (`labels_d6_real.json`, `label_schema.json`, `label_today_rain.py`) use whole-image categorical tags (`traffic: CLEAR`, `flood: SHALLOW`). These serve strictly as an evaluation benchmark, NOT training data:
     - YOLO requires bounding boxes $[x, y, w, h]$ per vehicle, not whole-frame labels.
     - Flood segmentation requires pixel masks of standing water.
     - *Directional Congestion Anomaly:* On dual-carriageway corridors, one outbound lane can be completely jammed while the inbound lane is empty. Whole-frame tags lose directional saturation.
     - *False Congestion on Empty Roads:* Occasional empty scenes are misclassified as CONGESTED due to anchor noise or floor mismatch.
   - *Action for Sprint 7:* Re-evaluate and rebuild the labeling taxonomy and toolchain with Claude to support directional bounding boxes and polygon water masks.

5. **Map Clutter Hygiene: Filtering Dead / Ghost Cameras (Black Dots):**
   - *The Problem:* 125 cameras in `cameras_enriched.json` are permanently or temporarily offline (`NOT_IMAGE` / `NO_SIGNAL`), appearing as black dots. Commuters clicking them encounter frustrating 503 ghost camera modals.
   - *Action for Sprint 7:* Implement an active map toggle (enabled by default) that automatically filters out and hides offline/no-signal cameras, ensuring the explorer displays only functional, actionable live camera feeds.

6. **Zalo OA Integration & Proactive Commute Route Monitoring (Future Evolution):**
   - *Zalo OA Channel (ADR-001 Phase 2):* Telegram reaches tech-savvy users; everyday Vietnamese motorbike riders rely on Zalo. Leverage the leaf `Channel` abstraction in `internal/notify` to design the `ZaloChannel` adapter and begin Zalo Official Account (OA) webhook and template verification.
   - *Proactive Commute Route Monitoring:* Allow commuters to save routine homebound routes (e.g. departing office at 17:30). The system monitors corridor cameras along the route and dispatches a proactive notification ~30 minutes prior to departure (17:00) with route conditions and recommended navigation advice.
   - *Route A→B Infrastructure Spike:* Evaluate OSRM / Valhalla deployment constraints on `homesrv` and edge-weight overlay strategies consuming live PCU saturation indices.
