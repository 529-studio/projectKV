# Sprint 8 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine` / `warded-model`)
**Document Version:** 1.0.0 (Audited & Reconciled with `dev` Branch)
**Date:** 2026-10-24
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents, Developers
**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/ADR-010-sprint7-audit-and-sprint8-plan.md`](ADR-010-sprint7-audit-and-sprint8-plan.md) (Sprint 8 Roadmap: Ground-Truth Perception, Concurrency Resilience & Real-Time Alert Dispatch)
- [`docs/sprint8_execution_guide.md`](sprint8_execution_guide.md) (Sprint 8 Multi-Wave Execution Guide)
- [`docs/project_status.md`](project_status.md) & [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 52–57)
- 529 Studio Outline Wiki — `[Arch] Cấu Hình Phần Cứng & Hệ Thống (home-srv & dev-box)`, `[Arch] Tổng Quan Kiến Trúc Hạ Tầng & Danh Mục Dịch Vụ (Self-Infras 2026)` (hardware and topology ground truth)
- GitHub PR ledger: `origin/dev`, PRs #276 → #296

---

## 1. Executive Summary

Sprint 8 executed three architectural fronts defined by ADR-010: **Concurrency & Configuration Resilience**, **Crowdsource Runtime Bridge**, and the **Road Surface Water Segmentation Toolchain** — plus a staging verification wave for live Telegram alerting.

Delivered in this sprint window (PRs #276 → #295 merged; #296 open):

1. **ADR-009 Technical Design Spike (PR #276)**: Self-hosted flood segmentation design document closing Sprint 7's T9 spike.
2. **CI/CD Runner Resilience (PRs #277, #278)**: dev-box self-hosted runner routing with graceful fallback when offline and paused daily audits.
3. **Archiver Performance (PR #280)**: `ListDates` optimized with S3 Delimiter scans, eliminating Cloudflare 524 timeouts.
4. **Cloudflare Zero Trust Access (PR #282)**: OIDC authentication with Dev Token fallback for internal endpoints.
5. **Waterway Camera Exclusion (PR #284)**: bridge-footing hydrological cameras hidden from map and perception pipeline (Lesson 50).
6. **MLOps Bulk Annotation Pipeline (PR #286)**: in-memory label cache, web refresh button, resilient batch auto-labeler — empirically produced **23,665 verified labels** (documented in [`docs/mlops_automated_annotation_and_bulk_approval.md`](mlops_automated_annotation_and_bulk_approval.md)).
7. **ADR-010 Governance (PR #291)**: Sprint 7 audit certification and Sprint 8 execution architecture.
8. **S8-T5 Harvester Semaphore Deadlock (Issue #287, PR #292)**: labeled loop break fixing stolen semaphore tokens; heuristic weather depth guessing deprecated to `UNKNOWN` (Lesson 39 enforcement).
9. **S8-T6 Configuration Sanitization (Issue #288, PR #293)**: holistic quote/whitespace sanitization across 25+ environment variables with structured warning logs.
10. **S8-T7 Crowdsource Runtime Bridge (Issue #289, PR #294)**: `internal/feedback` wired to `liveHub` and `FloodHexAggregator`; amber citizen-report markers with 15-minute sliding TTL and multi-IP escalation on the live map.
11. **S8-T8 Segmentation Toolchain (Issue #290, PR #295)**: YOLO segmentation dataset exporter (`tools/eval/export_yolo_seg.py`), off-box training pipeline (`tools/train/train_yolov8_seg.py`), and cv-engine dual-layer physical flood triangulation (water ratio + motorcycle wheel submersion) with OpenVINO mask prototype parsing.

### Quantitative Scorecard

| Metric | Planned (ADR-010) | Delivered | Evidence |
|---|:---:|:---:|---|
| Core Sprint 8 tickets | 5 (S8-T5…T9) | **4 fully closed + 1 tooling-complete awaiting operator** | T5 (#287/PR #292), T6 (#288/PR #293), T7 (#289/PR #294), T8 (#290/PR #295) merged; T9 (#246/PR #296) code-complete, live token provisioning pending |
| PRs merged in sprint window | — | **12** (#276, #277, #278, #280, #282, #284, #286, #291, #292, #293, #294, #295) | GitHub ledger |
| Open PRs | — | **1** (#296) | GitHub ledger |
| Lifetime merged PRs | — | **164** | GitHub ledger |
| Open repository issues | — | **1** (#246) | GitHub ledger |
| Go backend test suite | all packages under `-race` | **100% passing** (re-verified 2026-10-24, 22 packages) | `make test-go` |
| Web unit tests | 0 failures | **155+ tests** (per latest `project_status.md` entries; labeler suites) | `npm test --prefix apps/web` |
| CV engine / tooling suites | 0 failures | model 14/14, eval 66/66, train 4/4 (PR #295) | `make test-model`, `make test-eval`, `make test-train` |
| Verified label catalog | — | **23,665 labels** on R2 with provenance audit trail | MLOps pipeline benchmarks |

---

## 2. Wave-by-Wave Audit of Delivered Work

### Wave 0 — Infrastructure Resilience (S8-T5, S8-T6)

**S8-T5 (Issue #287, PR #292)** — `[go-server] Harvester Semaphore Deadlock & Labeled Loop Break`
- Root cause: a bare `break` inside a `select` only exits the `select`, not the enclosing `for`; cancelled contexts kept spawning goroutines that stole semaphore tokens, deadlocking container shutdown.
- Fix: labeled loop break (`break harvestLoop`); goroutines are never spawned once `ctx.Done()` fires. Unit test `TestHarvester_ContextCancellationCleanShutdown` asserts 0 goroutine leaks and sub-100 ms graceful exit.
- Heuristic `DetermineWaterlineDepth` (rainfall ≥ 5.0 mm → `SHALLOW`) deprecated; auto-harvested labels now carry `flood: UNKNOWN` / `depth: UNKNOWN` unless vision-verified.

**S8-T6 (Issue #288, PR #293)** — `[go-server] Holistic Environment Configuration Sanitization`
- `strings.Trim(strings.TrimSpace(val), "\"'")` applied across all 25+ environment variables; silently discarded `strconv` errors replaced with structured `slog.Warn` lines (`key`, `raw_value`, `default`).
- Test suite `TestLoadConfig_SanitizationAndQuotedVariables` covers quoted values, trailing whitespace, and invalid numerics with fallback defaults.

### Pre-Wave Hardening (context PRs)

- **PR #280**: `ListDates` S3 Delimiter scans — one round-trip instead of recursive `ListObjectsV2` pagination.
- **PR #282**: Cloudflare Zero Trust OIDC (`/api/archiver/auth/me`) with Dev Token fail-closed fallback.
- **PR #284**: exclusion of waterway/bridge-footing cameras from map and perception (Lesson 50 sensor hygiene).
- **PR #286**: in-memory label cache + `POST /api/archiver/cache/invalidate` + resilient `batch_auto_label.py` (8.1 fps against cv-engine, token-bucket paced).

### Wave 1 — Crowdsource Runtime Bridge (S8-T7, Issue #289, PR #294)

- `feedback.Manager` exposes `OnReportAccepted`; wired in `cmd/server/main.go` to `liveHub.BroadcastUserFloodReport` and `floodHexAggregator.OnUserReport`.
- SSE event `user_flood_report`; `MapComponent.tsx` renders high-contrast amber citizen-report markers with countdown TTL; ≥ 2 distinct IP hashes within 10 minutes escalate prominence.
- Zero-emoji bilingual UI per Rule 1; 155+ web unit tests passing.

### Wave 2 — Segmentation Toolchain (S8-T8, Issue #290, PR #295)

- `tools/eval/export_yolo_seg.py`: exports Ultralytics YOLO segmentation datasets (6 classes: `roadway`, `water_puddle`, `motorcycle`, `car`, `bus`, `truck`) with normalized-coordinate verification.
- `tools/train/train_yolov8_seg.py`: off-box (Colab / GPU workstation) training with host-compute guardrails, exporting OpenVINO FP16 IR (`yolov8n_seg.xml`/`.bin` < 10 MB).
- `services/cv-engine/server.py`: mask prototype tensor parsing (`output1`, 32×104×104), dual-layer physical triangulation (Layer 1: `R_water ≥ 0.15`; Layer 2: motorcycle wheel submersion `δ_sub ≥ 0.18` → `FLOODED_DEEP`).
- Follow-up `9acafed`: Python 3.11 typing compatibility fix.

### Wave 3 — Staging Telegram Verification (S8-T9, Issue #246, PR #296 — OPEN)

- `scripts/verify_staging_telegram.sh`: credential fail-fast validation, Bot API `getMe` authenticity, bilingual zero-emoji message contract check for camera `586e2995f9fab7001111b0b5`, live `sendMessage` HTTP 200 assertion, optional staging liveness probe; verified 5/5 against a local mock Bot API.
- `docs/runbooks/telegram_staging_verification.md`: Portainer Stack 14 provisioning, live-mode startup verification, debounce observation, and token-leak audit procedures.
- Defense-in-depth: `telegramBotTokenRegex` in `internal/middleware/sanitizer.go` scrubs echoed bot tokens to `[REDACTED_BOT_TOKEN]`; `TestNotifyIntegration_DebounceFiveMinuteWindow` pins the 5-minute suppression / 31-minute expiry semantics.
- **Remaining**: operator provisioning of the live staging bot token (Stack 14) and group confirmation — see §4.

---

## 3. Reality-Check Audit Findings — What Is Still Missing

An end-to-end audit of the perception/labeling/training loop (repository + Outline infrastructure docs) found the following gaps. These directly seed the Sprint 9 backlog (ADR-011).

### F1. Zero Segmentation Masks — the Training Loop Is Broken at the Data Layer
- All 23,665 production labels are **pure classification JSON** (flood level, directional traffic, weather, time-of-day). The `LabelPayload` struct (until branch `feat/polygon-labeler-annotation-contract`) had **no geometry field**, and `/labeler` has **no drawing capability**.
- Consequently `yolov8n_seg` has **never been trained**: the staging cv-engine still serves the Sprint 3 stock COCO vehicle model (`yolov8n_openvino_320`), and flood verdicts in production come only from the quota-limited Gemini VLM path (10 RPM / 300 RPD) or crowdsourced reports.

### F2. Synthetic Geometry Hazard in the Exporter
- `tools/eval/export_yolo_seg.py` `synthesize_polygons_from_metadata()` (lines 154–183) **fabricates a fixed roadway trapezoid and generic puddle polygons from classification metadata** whenever explicit polygon annotations are absent — which today is 100% of labels.
- Training on such output would bake fabricated geometry into the model under the guise of ground truth — a geometry-level repeat of the answer-key pollution condemned in Lesson 39. Sprint 9 gates this behind `--allow-synthetic` (ADR-011 D2).

### F3. Harvester Not Autonomous on Staging
- The scheduled harvester (07:30 / 12:00 / 17:30 ICT + rain watcher) is complete in `internal/harvester` but `HARVESTER_ENABLED` defaults to `false`; current collection depends on developers running `make dev-all` locally. Staging (Stack 14) must run the flywheel 24/7 for the label catalog to keep growing unattended (ADR-011 D5).

### F4. Staging Verification Script Parity Bug (PR #296 audit finding)
- `scripts/verify_staging_telegram.sh` hardcodes district `Bình Thạnh` (from the chokepoints fixture), but production `dispatchFloodAlert` renders `cam.Zone` — for the staging camera that resolves to `Khu vực Nội thành TP.HCM` (default `ZoneNoiThanh` classification). The runbook's byte-parity claim is therefore inaccurate. Fix: single-source generation via `cmd/notify-preview` (Sprint 9, S9-T5).

### F5. Zero Forecast Capability
- All traffic/flood outputs are real-time assessments of the current frame (180 s staleness TTL). The only future-state signal is Open-Meteo's 48 h precipitation probability. There is no congestion/flood nowcast-to-forecast layer (10–30 min ahead) anywhere.

### F6. Zone Subscription Semantic Mismatch (pre-existing)
- `GetSubscribersForChokepoint` matches `target_type='zone'` subscriptions against `cam.Zone` **traffic zones** (`Khu vực Nội thành TP.HCM`, `TP. Thủ Đức`, …), while the subscription taxonomy describes administrative districts (`Quận/Huyện`). A subscriber registering `Quận Bình Thạnh` will never match any alert.

### F7. Formatting Hygiene Debt
- Five files merged in #294/#295 are not `gofmt`-clean (`internal/live/constants.go`, `flood_hex.go`, `flood_hex_test.go`, `hub_test.go`, `internal/feedback/feedback_test.go`). CI does not enforce `gofmt`; to be cleaned in Sprint 9.

### F8. Governance Drift: Plane Hub Decommissioned
- AGENTS.md Rule 9 still mandates `closes [KV-<card-id>]` Plane Kanban linking, but the 529 Studio Hub (Plane CE) was decommissioned in 10/2026 (RAM reclamation on `home-srv`); project tracking moved to GitHub Projects v2. Sprint 9 amends Rule 9 (ADR-011 D6).

### F9. Hardware & Topology Ground Truth (Outline docs, for Sprint 9 planning)
- **dev-box** (Ryzen 7 6800HS, 4 vCPU AVX2, 3.8 GB RAM, 19 GB SSD): hosts `warded-cv-engine` (Stack 17, 2 vCPU / 2 GB cgroup) and the GitHub Actions self-hosted runner. Measured YOLOv8n inference p50 = 16.17 ms; 50-camera batch = 2.24 s — 10–20× faster than `home-srv`.
- **home-srv** (ZOTAC ZBOX CI320NANO, Celeron N2930 4C non-AVX, 8 GB DDR3L + zram, HDD 5400 RPM): staging ingress (`warded-stg.529studio.site`), Traefik, Redis, SQLite; internal `warded-model` fallback only.
- Backend → dev-box inference round trip over Tailscale ≈ 110 ms (94 ms RTT + 16 ms compute), with automatic fallback to the internal Celeron-served copy when the mesh is down. SAM-assisted pre-labeling belongs on dev-box only (non-AVX Celeron is disqualified).

---

## 4. Carry-Over Ledger

| Item | State | Owner | Blocking |
|---|---|---|---|
| Issue #246 / PR #296 | Code-complete; awaiting staging bot token provisioning in Portainer Stack 14 + group confirmation + evidence capture (runbook §10) | Operator (`giangdq202`/`thdat-vu`) | Live Telegram credentials |
| S8-T9 script parity bug (F4) | Fix scheduled as Sprint 9 S9-T5 | Engineering | — |
| Open issues | #246 only | — | — |

---

## 5. Sprint 9 Bridge

Sprint 9 is chartered by [`ADR-011`](ADR-011-sprint8-audit-and-sprint9-plan.md) around one theme: **close the ground-truth segmentation flywheel** — polygon annotation contract + canvas editor, SAM-assisted pre-labeling, autonomous staging harvesting, first off-box training with a quantitative mAP gate, weights deployment, plus deferred hygiene (Telegram parity, forecasting stretch). Execution sequencing is defined in [`sprint9_execution_guide.md`](sprint9_execution_guide.md).
