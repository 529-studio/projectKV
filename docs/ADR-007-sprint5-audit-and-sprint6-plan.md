# ADR-007: Sprint 6 Plan — Resolving the Perception Layer Gate & Dispatching Verified Flood Alerts

**Status**: Proposed  
**Date**: 2026-10-13  
**Authors**: ProjectKV Engineering & Architecture Team  
**Related**:
- [`docs/sprint5_report.md`](sprint5_report.md) (Sprint 5 Audited Completion & Scorecard Report)
- [`docs/ADR-006-sprint4-audit-and-sprint5-plan.md`](ADR-006-sprint4-audit-and-sprint5-plan.md) (Sprint 5 Plan: Closing the T9 Empirical Gap)
- [`docs/ADR-001-flood-alert-architecture.md`](ADR-001-flood-alert-architecture.md) (Flood Alert Architecture & Notification Channels)
- [`docs/reports/cv_detection_error_audit.md`](reports/cv_detection_error_audit.md) (S5-T2 CV Detection Error Audit)
- [`docs/reports/eval_report_d6_real.md`](reports/eval_report_d6_real.md) (764-Frame Real Ground-Truth Baseline)
- [`docs/lesson_learn.md`](lesson_learn.md) (Lesson 19, Lesson 22, Lesson 27, Lesson 28)

---

## 1. Context & Retrospective Audit Reconciliation

### 1.1. Sprint 5 Report Delivery Clarification
During peer review, it was noted that only a compact Q&A document was initially evaluated. The authoritative, comprehensive [Sprint 5 Report](sprint5_report.md) (tracking 100% completion of backlog tickets S5-T1 through S5-T4, 7 merged PRs, and 122 lifetime PRs) was formally compiled in PR #220. This ADR builds directly upon the empirical ground truth established in that report.

### 1.2. Accomplishments Verified in Sprint 5
1. **S5-T2 Diagnostic Rigor**: The CV detection error audit investigated raw tensor activations down to anchor cells, isolating three structural defects: lack of Non-Maximum Suppression (anchor explosion causing 2.7x–4.7x count inflation), non-letterboxed aspect ratio distortion (stretching 16:9 images into 320x320 and suppressing confidence by ~35%), and small-object resolution decay.
2. **S5-T4 Fail-Closed Security (Lesson 28)**: Unauthenticated public access to `/labeler` and `/api/archiver/*` was identified as a critical data-poisoning vector. PR #218 implemented a fail-closed gatekeeper (`middleware.RequireDevAuth`) with constant-time token comparison, returning `503 Service Unavailable` in production/staging if credentials are unconfigured.
3. **S5-T3 Decoupled Hybrid Flood Pipeline**: Successfully decoupled 24/7 edge traffic telemetry from event-driven cloud VLM verification, guarded by an in-process thread-safe quota limiter (10 RPM / 300 RPD).

### 1.3. The Primary Gap: Diagnostic Proposals vs. Empirical Implementation (Lesson 19)
The CV detection audit (`docs/reports/cv_detection_error_audit.md`) established detailed proposals (Proposals 1–5). However, an inspection of the current production codebase confirms:
- **`services/cv-engine/server.py` remains untouched**: Lines 185–198 continue to execute raw activation summation `np.sum(data[class_idx, :] > 0.45)` without Non-Maximum Suppression.
- **Preprocessing remains unletterboxed**: Line 161 directly executes `image.resize((320, 320))`, distorting geometric aspect ratios.
- **Resolution remains fixed at 320x320**: The model has not been exported or configured for 416x416 inference.
- **The 764-frame evaluation was never re-run**: `eval_report_d6_real.md` dates to 2026-10-09 (Macro F1 = 0.206), which was run *prior* to the audit.

As codified in **Lesson 19 ("The Unit-Test Verification Trap & The Parameter Calibration Paradox")**, accepting diagnostic proposals or synthetic unit tests without empirical re-verification against the full ground-truth dataset produces an illusion of progress. On live staging, wide-lane arterial cameras like *Võ Văn Kiệt - Ký Con* still report `[ĐÔNG XE]` on free-flowing traffic.

**Sprint 6 must not build new speculative traffic features upon unverified foundations.**

---

## 2. Architectural Decisions for Sprint 6

### Decision 1: Perception Layer Hard Gate (S6-T1 First)
No downstream traffic features or route-level analysis will be developed until the three core CV fixes are implemented in `services/cv-engine/server.py` and benchmarked across all 764 authentic ground-truth frames:
1. Standard Non-Maximum Suppression (`IoU <= 0.45`, `conf >= 0.25`) to collapse multi-scale anchor activations into discrete physical instances.
2. 16:9 aspect-preserving letterboxing with neutral gray padding `(114, 114, 114)` to maintain convolutional priors.
3. Input resolution upgrade to 416x416 utilizing available compute headroom on `dev-box`.
4. Execution of `tools/eval/benchmark.py` across all 764 frozen frames to publish a definitive post-fix Macro F1 and class-by-class confusion matrix.

### Decision 2: Corridor-Specific Capacity Calibration & Persistent Peak Storage (S6-T2)
A global `capacityFloor = 15.0` PCU misclassifies wide 10–12 lane boulevards (physical capacity 40–50 PCU) as congested when only ~6 vehicles are present ($10.2 / 15.0 = 0.68 \ge 0.60$).
- Parameterize `base_capacity` per camera in `cameras_enriched.json` according to physical lane counts (expressways: 40–50 PCU, major arterials: 25–30 PCU, alleys: 15 PCU).
- Refactor `services/go-server/internal/hotset/pcu.go` to consume per-camera capacity.
- Persist learned peak capacities (`histMax`) in an SQLite table `camera_capacities` to eliminate cold-start resets upon Portainer container restarts.

### Decision 3: Modal UI/UX Affordance Elevation & Client Cooldown (S6-T3)
The `[Xác minh ngập nước]` action button in `CameraModal.tsx` suffers from visual affordance collision (shares identical border, font, background, and bracketed syntax `[...]` with passive status chips) and lacks client-side rate limiting.
- Redesign the button with high-contrast tactile elevation, remove brackets `[...]` (reserved strictly for passive chips), and include a monochromatic SVG radar icon (`⌖`).
- Separate the action trigger row from the resulting verdict badge.
- Implement an explicit 15-second client-side cooldown countdown (`[Chờ xác minh (15s)]`) to prevent user spamming.

### Decision 4: Automated Secret Scanning in GitHub Actions CI (S6-T4)
In Sprint 5, an API secret was committed to a test script for the second time (Lesson 22). Relying exclusively on human vigilance violates sound software engineering principles.
- Automate secret scanning by introducing `.github/workflows/secret-scan.yml` powered by `gitleaks/gitleaks-action`.
- Enforce blocking CI gates on all pull requests and pushes targeting `dev` and `main`.
- Maintain `.gitleaks.toml` with project-specific credential patterns and an explicit allowlist for fixture mocks and template placeholders (`<placeholder>`).

### Decision 5: Real-Time Flood Alerting via Telegram (S6-T5)
With verified flood verdicts operational from S5-T3, real-time flood alerting is approved for implementation.
- Codebase audit confirmed that `services/go-server/internal/notify/` does not yet exist.
- Implement `internal/notify/` as a leaf modulith package containing `Channel` interface and `Manager` orchestrator.
- Implement `TelegramChannel` using the Telegram Bot API (`SendMessage` with clean HTML, zero emojis, and bilingual templates).
- Implement `internal/subscription` backed by SQLite storing chat IDs, target zones, and chokepoints.
- Connect `flood.Service` verdict emissions to `notify.Manager` with a mandatory **30-minute sliding debounce window** per chokepoint to eliminate alert fatigue.

### Decision 6: Explicit Deferral of Route A→B
Route-level check / navigation (Route A→B) is **formally deferred** from Sprint 6 due to two hard architectural blockers:
1. **Perception Ground Truth Blocked**: Traffic detection accuracy on the frozen test set is Macro F1 = 0.206. Building routing recommendations on unverified perception data violates ADR-005 and ADR-006 mandates.
2. **Routing Engine Architecture Missing**: Zero routing engine code exists in `services/go-server/`. Valhalla is neither deployed nor wired into Go. Building routing requires an independent infrastructure spike (OSRM/Valhalla integration, OpenStreetMap ingestion, and edge-weight overlay) that exceeds the scope of Sprint 6.

---

## 3. Sprint 6 Backlog Matrix

| Ticket | Scope & Summary | Dependencies | Active Effort | Delivery Window | Issue Tracker |
|:---|:---|:---:|:---:|:---:|:---:|
| **S6-T1** | Implement NMS, Letterboxing, 416px Scaling & Re-run 764-Frame D6 Benchmark | None (Hard Gate) | 12–16h | 2 days | [#221](https://github.com/529-studio/projectKV/issues/221) |
| **S6-T2** | Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage | S6-T1 | 8h | 1 day | [#222](https://github.com/529-studio/projectKV/issues/222) |
| **S6-T3** | CameraModal UI/UX Redesign (Tactile Affordance) & 15s Client-Side Cooldown | None | 6h | 1 day | [#223](https://github.com/529-studio/projectKV/issues/223) |
| **S6-T4** | Automated Secret Scanning in GitHub Actions CI (Gitleaks) | None | 4h | 0.5 day | [#224](https://github.com/529-studio/projectKV/issues/224) |
| **S6-T5** | Telegram Flood Alert Dispatcher (`internal/notify`, SQLite Store & 30m Debounce) | S5-T3 | 16–20h | 2.5 days | [#225](https://github.com/529-studio/projectKV/issues/225) |

---

## 4. Definition of Done & Operational Guardrails

All Sprint 6 pull requests must adhere to the following mandatory standards:
1. **Rule 1**: Zero emojis in UI components, notifications, and logs. High-contrast monochromatic styling. Codebase, documentation, and tooling 100% English.
2. **Rule 2**: Explicit time estimation provided before commencing work.
3. **Rule 4**: End-to-end simulation and empirical verification prior to declaring fixes complete.
4. **Rule 8**: Zero hardcoded credentials; all secrets loaded from `.env` with fail-fast empty fallbacks.
5. **Rule 9**: PR descriptions strictly start with `Close #<issue-number>` adhering to `.github/pull_request_template.md`.
6. **Automated Verification**: 100% test pass on Go test suite (`go test -race ./...`), Web unit tests (`npm test`), and Gitleaks CI scan.
