# Sprint 5 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  
**Document Version:** 1.0.0 (Audited & Reconciled with `dev` Branch)  
**Date:** 2026-10-13  
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents (Claude / Antigravity), Developers  
**Primary References & Ground Truth:**
- [`docs/ADR-006-sprint4-audit-and-sprint5-plan.md`](ADR-006-sprint4-audit-and-sprint5-plan.md) (Sprint 5 Plan: Closing the T9 Empirical Gap)
- [`docs/sprint5_execution_guide.md`](sprint5_execution_guide.md) (Sprint 5 Multi-Wave Execution & Operating Protocol)
- [`docs/reports/eval_report_d6_real.md`](reports/eval_report_d6_real.md) (S5-T1: Post-T5 D6 Benchmark Evaluation on 764 Real Frames)
- [`docs/reports/smoke_eval_d6_real.md`](reports/smoke_eval_d6_real.md) (S5-T1: Known-State Smoke-Test Camera Verification)
- [`docs/reports/cv_detection_error_audit.md`](reports/cv_detection_error_audit.md) (S5-T2: YOLOv8n Object Detection Error Diagnostic Audit)
- [`docs/reports/tailscale_failover_drill_report.md`](reports/tailscale_failover_drill_report.md) (S5-T4: Physical Network Partition Drill on `dev-box`)
- [`docs/decision-openvino-vs-vlm.md`](decision-openvino-vs-vlm.md) (Architectural Decision: Decoupled Hybrid Pipeline)
- [`docs/project_status.md`](project_status.md) & [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 20–28)
- Git log: commits on `origin/dev` from `4ede5c2` (PR #218) back through `f5b5f25` (PR #206).

---

## 1. Executive Summary

Sprint 5 was executed under a strict architectural mandate established in [ADR-006](ADR-006-sprint4-audit-and-sprint5-plan.md): **"The Ground-Truth Reckoning"**. Following the Sprint 4 audit, which revealed that algorithmic parameter tuning (`CapacityFloor = 15.0`) had been accepted based on synthetic unit tests without empirical re-verification on real images, the engineering team froze all speculative feature development to confront the actual behavior of the system under real-world traffic, physical network partitions, cloud storage constraints, and security exposure.

### Quantitative Scorecard & Audit Reconciliation

| Metric | Planned in ADR-006 Backlog | Delivered in Sprint 5 | Status |
|---|:---:|:---:|:---:|
| **Core Sprint 5 Backlog Items** | 5 items (S5-T1, S5-T2, S5-T3, S5-T4 Failover, S5-T4 Storage) | 5 items completed | **100% Completed / Merged** |
| **Completed Backlog Tickets** | 5 | 5 | S5-T1 ([#207](https://github.com/529-studio/projectKV/issues/207)), S5-T2 ([#208](https://github.com/529-studio/projectKV/issues/208)), S5-T3 ([#209](https://github.com/529-studio/projectKV/issues/209)), S5-T4 Failover ([#210](https://github.com/529-studio/projectKV/issues/210)), S5-T4 Storage ([#214](https://github.com/529-studio/projectKV/issues/214)) |
| **Pending / Open Issues on Repo** | 0 | 0 | **0 Open Issues (Clean Backlog)** |
| **Supplementary Features, Fixes & Hardening** | — | 4 PRs | PR #212 (Cloudflare R2 Integration), PR #217 (PR Issue Linking Mandate & Template), PR #218 (Developer Auth Gatekeeper & Fail-Closed Security), Dependabot security bumps |
| **Total Sprint 5 PRs Merged** | — | **7 Pull Requests** | PR #211, PR #212, PR #213, PR #215, PR #216, PR #217, PR #218 |
| **Lifetime Repository PR History** | — | **131 Pull Requests** | **122 Merged to `dev`**, 8 Closed without merging, 1 Open (`docs/sprint5-report`) |
| **Go Backend Test Suite** | 18 packages | 18 packages | **100% Passing under `-race` (0 failures, 0 races)** |
| **Web Client Unit Tests** | 92 tests | 103 tests (16 suites) | **100% Passing (0 failures)** |
| **Tooling Test Suites** | 65 tests | 65 tests (`tools/eval`, `tools/geo-enrich`) | **100% Passing (0 failures)** |
| **ESLint & TypeScript Validity** | 0 errors | 0 errors | **Clean & Validated** |

---

## 2. Comprehensive Audit of Sprint 5 Epics & Waves

### Wave 0 — Empirical Hard Gate & Infrastructure Chaos Verification

#### 1. S5-T1 (Issue #207) — `[tooling] re-run D6 benchmark and smoke test with post-T5 weights`
- **Merged PR:** [#211](https://github.com/529-studio/projectKV/pull/211) (`tooling/s5-t1-rerun-d6-benchmark`)
- **Deliverables:** [`docs/reports/eval_report_d6_real.md`](reports/eval_report_d6_real.md), [`docs/reports/smoke_eval_d6_real.md`](reports/smoke_eval_d6_real.md), archived baselines in `eval_report_d6_pre_t5.md` and `smoke_eval_d6_pre_t5.md`.
- **Key Technical Findings:**
  1. **Confirmation of ADR-006 §2 Mathematical Prediction:**
     - Macro F1 moved from `0.209` (pre-T5) to `0.206` (post-T5) — virtually flat.
     - Weighted F1 shifted from `0.364` to `0.351`.
     - Lowering `CapacityFloor` from 25.0 to 15.0 multiplied all saturation indices by $1.67\times$. As predicted mathematically, this **doubled** false-positive congestion on genuine `CLEAR` cameras: CLEAR frames misclassified as CONGESTED rose from **22 to 46 frames (+109%)**.
     - Zero-detection cameras remained unchanged at $SI = 0.00$ because $0 / 15.0 = 0$.
  2. **Empirical Takeaway:** Parameter tuning downstream cannot rescue object detector failures upstream. S5-T1 definitively proved that tweaking mathematical thresholds cannot substitute for perceptual accuracy, directly gating and validating the necessity of S5-T2.

#### 2. S5-T4 Chaos Drill (Issue #210) — `[infra] physical Tailscale disconnection test for dev-box failover`
- **Merged PR:** [#213](https://github.com/529-studio/projectKV/pull/213) (`infra/s5-t4-tailscale-failover-drill`)
- **Deliverables:** [`docs/reports/tailscale_failover_drill_report.md`](reports/tailscale_failover_drill_report.md), [`docs/runbook_dev_box_failover.md`](runbook_dev_box_failover.md).
- **Key Technical Findings:**
  - Executed a live physical network severance drill on Staging (Stack 14 on `homesrv`): while the hotset scheduler was actively polling cameras, Tailscale was taken down (`tailscale down`) on the inference host `dev-box`.
  - **Graceful Degradation Verified:** The Go backend API server handled the connection drops cleanly:
    - Logged structured warnings: `WARN analysis failed, degrading to unknown camera=... error="cv-engine: POST /predict: dial tcp [::1]:8000: connect: connection refused"`.
    - Handled timeouts within 10 seconds without blocking the Gin request loop.
    - Degraded verdicts to `SeverityUnknown` (`Source: fallback`) with zero panics, zero HTTP 500 errors, and zero memory leaks.
  - **Self-Healing Verified:** When Tailscale was reconnected (`tailscale up`), inference resumed automatically on the next 30s hotset cycle without requiring container restarts.

---

### Wave 1 — Error Auditing & Decoupled Hybrid Flood Pipeline

#### 3. S5-T2 (Issue #208) — `[cv-engine] object detection audit for false-positive/missed detections`
- **Merged PR:** [#215](https://github.com/529-studio/projectKV/pull/215) (`tooling/s5-t2-cv-detection-audit`)
- **Deliverables:** [`docs/reports/cv_detection_error_audit.md`](reports/cv_detection_error_audit.md), `cv_detection_error_audit.json`, and 12 visual overlay artifacts in `docs/reports/audit_overlays/`.
- **Key Technical Findings:**
  1. **Small Vehicle Distance Decay:** Standard $320 \times 320$ YOLOv8n suffers severe recall drop-off for distant motorcycles ($>25\text{m}$ from the CCTV pole). Motorcycles appear as $8 \times 12$ pixel blobs, dropping below feature pyramid activation thresholds.
  2. **Night Glare & Reflection Hallucinations:** Wet asphalt reflections and headlight bloom trigger false-positive car/truck detections during off-peak night rain.
  3. **High-Angle Perspective Distortion:** Cameras angled downward along major boulevards compress vehicle bounding boxes, causing overlapping queues to merge into single detections.
  4. **Prescribed Roadmap for Sprint 6:** Increasing model input resolution from $320 \times 320$ to $416 \times 416$ or $640 \times 640$ on `dev-box` (which has ample GPU/CPU headroom running at 24 FPS) will directly resolve small-object recall without requiring external cloud APIs.

#### 4. S5-T3 (Issue #209) — `[go-server + web] decoupled hybrid flood pipeline`
- **Merged PR:** [#216](https://github.com/529-studio/projectKV/pull/216) (`feat/s5-t3-hybrid-flood-pipeline`)
- **Deliverables:** `services/go-server/internal/flood` package ([`service.go`](../services/go-server/internal/flood/service.go), [`limiter.go`](../services/go-server/internal/flood/limiter.go), [`handler.go`](../services/go-server/internal/flood/handler.go)), [`tools/eval/fixtures/flood_chokepoints.json`](../tools/eval/fixtures/flood_chokepoints.json), [`apps/web/src/components/CameraModal.tsx`](../apps/web/src/components/CameraModal.tsx).
- **Key Technical Findings:**
  1. **Decoupled Architecture Implementation (Option C):** Successfully decoupled continuous traffic telemetry (OpenVINO edge, 30s cycle, $0 API cost) from cloud VLM reasoning.
  2. **Dual-Trigger Dispatch:**
     - *Trigger 1 (Environmental):* Background loop automatically inspects authentic municipal flood chokepoints (`tools/eval/fixtures/flood_chokepoints.json`) ONLY during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$ or active rain WMO codes), consuming 0 API calls during dry weather.
     - *Trigger 2 (On-Demand):* Commuters can trigger real-time VLM verification from the web modal via `POST /api/flood/verify`.
  3. **In-Process Quota Guard:** Implemented a thread-safe sliding-window rate limiter (10 RPM) and daily calendar rollover quota (300 RPD) in [`flood.QuotaLimiter`](../services/go-server/internal/flood/limiter.go), returning `HTTP 429 Too Many Requests` to protect against surprise cloud bills.

---

### Wave 2 — Cloud Storage & Collaborative Team Labeling (S5-T4 / Issue #214)

#### 5. S5-T4 R2 Storage & Shared Team Labeler
- **Merged PRs:**
  - [#212](https://github.com/529-studio/projectKV/pull/212): Core Cloudflare R2 driver integration and credential rotation.
  - [#218](https://github.com/529-studio/projectKV/pull/218): Archiver endpoints, web labeler, and developer authorization gatekeeper.
- **Deliverables:** [`services/go-server/internal/storage`](../services/go-server/internal/storage), [`services/go-server/internal/archiver`](../services/go-server/internal/archiver), [`apps/web/src/app/labeler/page.tsx`](../apps/web/src/app/labeler/page.tsx), [`services/go-server/cmd/verify-r2`](../services/go-server/cmd/verify-r2).
- **Key Technical Impact:**
  1. **Storage Layer Listing Abstraction:** Extended `storage.Storage` with `List(ctx, prefix) ([]Item, error)` implemented across both `LocalStorage` (local file walk with directory traversal protection) and `R2Storage` (`s3.ListObjectsV2` pagination with AWS SDK v2).
  2. **Collaborative Friction Elimination:** Developers and annotators no longer need to copy frames locally via SFTP/USB to use standalone tools. All frames and annotations are centrally hosted on Cloudflare R2 bucket `warded-proofs`.
  3. **Monochromatic Collaborative Web Labeler:** Shipped high-contrast labeler at `/labeler` with rapid-fire keyboard shortcuts (`1-3` flood, `Q-W-E-R` traffic, `A-S-D` weather, `Z-X-C` time of day, `Enter` save/advance), bilingual support (`vi` default), and zero emojis.
  4. **Storage Justification Statement:** Adoption of R2 blob storage is explicitly justified to enable centralized collaborative team labeling and eliminate manual synchronization friction, **NOT** because the system has breached the ADR-004 §D7 disk capacity threshold (100 MB / 5,000 files).

#### 6. Security Hardening & Developer Authorization Gatekeeper (PR #218)
- **Problem Statement:** An architectural security audit revealed that `/labeler` and `/api/archiver/*` were completely unauthenticated and publicly accessible on production, allowing any public visitor or crawler to view raw camera archives and submit forged labels via `POST /api/archiver/labels`, posing an extreme **AI ground-truth data poisoning** threat.
- **Remediation & Defense-in-Depth Implementation:**
  1. **Fail-Closed Backend Gatekeeper ([`devauth.go`](../services/go-server/internal/middleware/devauth.go)):** Enforces developer authorization on all `/api/archiver/*` routes. When `APP_ENV=production` or `staging`, if `DEV_ACCESS_TOKEN` is unset or blank, the server returns `HTTP 503 Service Unavailable`, refusing to operate in an unauthenticated state.
  2. **Constant-Time Verification:** Compares tokens via `crypto/subtle.ConstantTimeCompare`, neutralizing side-channel timing attacks.
  3. **Multi-Channel Token Support:** Supports `X-Dev-Token` header, `Authorization: Bearer <token>` header, and `?token=` / `?dev_token=` query parameters (required for browser `<img>` elements streaming raw JPEG binaries).
  4. **Monochromatic Client-Side Lock UI:** Next.js `/labeler` displays an in-place lock screen until unlocked with a valid token; credentials persist strictly in `sessionStorage` (cleared on tab close or manual lock session); automatically clears cached tokens upon receiving `401 Unauthorized`.
  5. **CORS Hardening:** Allowed methods explicitly set to `GET, POST, HEAD, OPTIONS` and `X-Dev-Token` exposed in `Access-Control-Allow-Headers`.

---

## 3. Governance, CI/CD Hardening & Developer Guidelines

During Sprint 5, several critical developer guidelines and CI rules were codified to maintain codebase integrity:

1. **Rule 9 — PR Issue Linking Format Mandate (PR #217):**
   - GitHub issue closing automation exclusively recognizes strict closing keywords (`Close #<issue-number>`, `Fixes #...`).
   - Phrasing such as `Addresses Issue #209` or `Implement #209` fails to link PRs.
   - Codified Rule 9 in `AGENTS.md` and established `.github/pull_request_template.md`.
2. **Rule 8 — Zero Hardcoded Secrets:**
   - Plaintext credentials discovered during R2 integration were revoked immediately upstream.
   - Enforced empty-string fallbacks (`os.Getenv("KEY")`) and fail-fast validation across all scripts.
   - Documented in `docs/lesson_learn.md` (Lesson 21).
3. **CI Linter Network Resilience (Lesson 25):**
   - Fixed flaky CI failures caused by external JSON schema timeouts in `golangci-lint-action` by setting `verify: false`.
   - Aligned runner Go toolchain with `install-mode: "goinstall"`.
4. **Go Formatting Discipline:**
   - Resolved `gofmt` compliance errors on `services/go-server/internal/archiver/handler.go` and `devauth_test.go`.

---

## 4. Reality-Check & Production Drift Analysis (Field Findings)

### 4.1. Rate Limiting Status on `[Xác minh ngập nước]` (FE vs. BE)
- **Backend:** **Enforced.** In-process `QuotaLimiter` enforces 10 RPM and 300 RPD, returning `HTTP 429`.
- **Frontend:** **Incomplete.** The button is disabled only while a verification request is pending (`isVerifyingFlood = true`). Once the request returns, the button re-enables immediately. There is **no client-side cooldown countdown** (e.g. 15s delay), allowing users to spam requests and consume the 10 RPM pool.
- **Action Item for Sprint 6:** Implement a 15-second client-side cooldown timer on `[Xác minh ngập nước]`.

### 4.2. Traffic Saturation Misclassification on Staging (`ĐÔNG XE` on Clear Roads)
- **Field Observation:** Camera *Võ Văn Kiệt - Ký Con* displayed `[ĐÔNG XE]` (Heavy) at 10:50:53 despite the snapshot showing free-flowing traffic across a wide 10-lane boulevard with ~5 vehicles.
- **Root Cause Analysis:**
  1. **Uniform Cold-Start Capacity Floor (`capacityFloor = 15.0` PCU):** A fixed global 15.0 floor treats a 2-lane residential street the same as a 12-lane expressway (which physically holds 40–60 PCU in the camera viewport). 3 small passing trucks ($3 \times 2.0 = 6.0\text{ PCU}$) plus a few cars/bikes reach 10.2 PCU $\rightarrow SI = 10.2 / 15.0 = 0.68 \ge 0.60$ (Heavy threshold).
  2. **In-Memory Volatility:** `histMax` is stored in RAM map `capacities map[string]float64`. Every container restart on staging wipes the learned peak capacity back to 15.0, trapping arterial cameras in cold-start over-reporting.
  3. **Static Snapshots Lack Kinematic Flow:** Still frames cannot measure vehicle velocity (60 km/h free-flow vs 0 km/h standstill).
- **Action Item for Sprint 6:** Calibrate corridor-specific capacity baselines based on lane geometry and persist learned peak capacities across server restarts.

### 4.3. UI/UX Affordance Defect: Button vs. Badge Collision
- **Field Observation:** The `[Xác minh ngập nước]` action button uses the exact same typography, padding, border, and bracketed syntax (`[...]`) as passive status chips (`[HOẠT ĐỘNG]`, `[BÌNH THƯỜNG]`, `[XÁC MINH VLM: Khô ráo]`).
- **UX Impact:** Commuters perceive the button as an unclickable status badge. Furthermore, after clicking, the action chip and the outcome chip sit side-by-side with almost identical visual weight.
- **Action Item for Sprint 6:** Redesign the button with tactile elevation, an active SVG glyph (e.g., radar/scan reticle), remove square brackets, and clearly separate action triggers from verdict badges.

### 4.4. VLM Flood Analysis Production Readiness
- **Verdict:** **NOT YET PRODUCTION READY.**
- **Blockers:**
  1. Default model name bug: `gemini-3.5-flash-lite` (fixed locally on `docs/vlm-flood-readiness-and-architecture-update` to `gemini-1.5-flash`).
  2. Missing production secret: `GEMINI_API_KEY` not yet injected in production `.env`.
  3. ADR-004 §D6 Safety Gate: The 764 authentic ground-truth frames (including 120 flood-positive frames) have not yet been batch-evaluated against Gemini VLM to verify $FNR \le 5\%$.

---

## 5. Comprehensive Summary of Lessons Learned (Sprint 5)

| Lesson # | Documented Title | Core Architectural Takeaway |
|:---:|---|---|
| **Lesson 20** | Git Hook Execution & Pre-Push Direct Push Rejection | Hooks must execute from repo root; bypass-protection must be verified via real git pushes. |
| **Lesson 21** | Zero-Secret Fallback Policy & Production Credential Rotation | Fallback values in `os.Getenv` must strictly be `""` with fail-fast termination. Templates must use `<placeholder>`. |
| **Lesson 22** | Pre-Flight Environment Variable Contract Verification | Container healthchecks must probe upstream endpoints before declaring operational status. |
| **Lesson 23** | Network Partition Graceful Degradation & Chaos Drills | Microservice architectures must survive dependency loss; degraded states must surface as `unknown`. |
| **Lesson 24** | Decoupled Multimodal Hybrid Routing & In-Process Quota Enforcers | Edge telemetry for high-frequency loops; cloud VLMs strictly event-driven with thread-safe token buckets. |
| **Lesson 25** | CI Linter Schema Validation Flakiness & Toolchain Alignment | Disable brittle external HTTP schema checks (`verify: false`); match linter toolchain with active Go version. |
| **Lesson 26** | GitHub Issue Auto-Closing Keyword Semantics vs. Descriptive Prose | PRs must start with `Close #<number>`. Phrasing like `Addresses Issue #...` fails to trigger GitHub webhooks. |
| **Lesson 27** | Centralized Collaborative Team Labeling Over Local Synchronization | Cloud blob storage (R2) justified for team coordination and friction removal, not D7 capacity exhaustion. |
| **Lesson 28** | Zero Unauthenticated Dev Endpoints & Fail-Closed Developer Gatekeeping | Developer tools must enforce token auth; production/staging must fail-closed (503) if secrets are unconfigured. |

---

## 6. Strategic Backlog & Technical Recommendations for Sprint 6

Based on the empirical findings of Sprint 5, the following technical items are recommended for the Sprint 6 backlog:

1. **Sprint 6 Item 1 — Corridor-Specific Visual Capacity Calibration:**
   - Parameterize `CapacityFloor` based on camera lane count ($N_{\text{lanes}} \times 4.0\text{ PCU}$ per approach) rather than a uniform 15.0 constant.
   - For 10–12 lane arterial expressways (Võ Văn Kiệt, Mai Chí Thọ), set baseline capacity floor to $40.0 - 50.0$ PCU, eliminating off-peak false-positive `ĐÔNG XE`.
2. **Sprint 6 Item 2 — Persistent Camera Peak Capacity Storage:**
   - Persist learned `histMax` in SQLite database (`camera_capacities` table) to prevent cold-start resets upon Portainer container redeploys.
3. **Sprint 6 Item 3 — Modal UI/UX Affordance Elevation & Client Cooldown:**
   - Redesign `[Xác minh ngập nước]` into an elevated, distinct action button with an SVG action icon.
   - Add a 15-second client-side cooldown timer to prevent user spamming.
4. **Sprint 6 Item 4 — Model Resolution Scaling ($320 \rightarrow 416\text{ px}$):**
   - Increase input resolution on `dev-box` to improve small motorcycle recall as diagnosed in S5-T2.
5. **Sprint 6 Item 5 — Formal D6 Safety Gate Execution on Gemini 1.5 Flash:**
   - Batch-evaluate the 764 authentic ground-truth frames (120 flood-positive) against Gemini 1.5 Flash to satisfy ADR-004 §D6 ($FNR \le 5\%$).
