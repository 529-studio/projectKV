# ADR-005: Sprint 3 Production Audit & Sprint 4 Plan

**Status**: Proposed
**Date**: 2026-10-05
**Related**: ADR-004 (D6 accuracy gate, D10 provenance, Finding 1 standby hardening)

## 1. Context

Sprint 3's completion report (v1.0.0) frames the sprint as "100% Completed" and
"production-grade... trustworthy." Live screenshots from the public staging
site taken after that report show traffic verdicts tagged `[DỮ LIỆU MẪU]`
(sample data) on cameras — including at least one visibly wrong case (an empty
road tagged `ÙN TẮC` / congested). This ADR records the audit findings and
plans Sprint 4 to close the gap between what was reported and what is actually
running in production, before any new feature work resumes.

This is the second time a sprint report has claimed completion while a core
deliverable was not actually live (the first was A4 in Sprint 2 — see ADR-004
§8). §5 below adds a process fix specifically to stop this pattern recurring a
third time.

## 2. Audit Findings

### Finding A — Self-hosted inference is not yet deployed to production; confirmed, and confirmed non-harmful (revised 2026-10-05)

**Correction**: the `[DỮ LIỆU MẪU]` screenshots that originally raised this
finding were taken on **local dev** (`APP_ENV=dev`), where H0 explicitly
permits mock verdicts with a visible badge — this was H0 working correctly,
not a leak, and the original framing of this as a possible honesty-rule
violation was wrong. A follow-up screenshot of the actual public `warded-stg`
site shows the real state: every camera marker white, the dock showing "—
Chưa phân tích" per tile, and the flood banner reading "Không ghi nhận điểm
ngập nào" — i.e. production is correctly showing **no verdict** rather than a
fabricated one. This is exactly the D4/D10 honesty behavior the system is
designed to have when a capability isn't ready yet. No violation occurred.

What still stands, with the alarm removed: OpenVINO model weights have not
been deployed to `local-homesrv` yet (Sprint 3's own Outstanding Debt item #4
confirms this is a TODO, not done), so the self-hosted vehicle detector is not
actually producing verdicts in production. The gap is between the Sprint 3
report's "100% Completed" / "production-grade, trustworthy" framing and this
still-pending deployment step — not between what's displayed and reality.
Production itself has not misrepresented anything.

Also confirmed: the team deliberately defaults the UI to the weather radar
layer (Open-Meteo/RainViewer — a real, externally-verified data source)
rather than the camera-based layers, specifically because the radar is the
one signal with real data behind it right now. This is a sound product
decision, consistent with the project's honesty principles, and is kept
as-is.

Further confirming evidence: a camera outside the curated 57-camera Hot Set
(`Tùng Thiện Vương - Xóm Củi`) shows an explicit `[Chờ phân tích AI]` ("waiting
for AI analysis") state on the real camera modal in production — a more
precise label than the generic "not yet analyzed" seen elsewhere, and further
confirmation that the system labels its actual per-camera state rather than
defaulting to a guess. **Finding A is considered closed.**

### Finding B — The D6 accuracy gate has never actually been run

`tools/eval/benchmark.py`'s own unit tests pass, but no precision/recall/flood
false-negative-rate number computed against real labeled Warded camera frames
appears anywhere in the Sprint 3 report. The "Gate to 2b" criteria recorded as
PASSED in the Sprint 2 audit were satisfied by the harness *existing*, not by
evidence of acceptable accuracy. Combined with the user's own confirmation
that a real labeled training dataset has not been built yet, the actual safety
gate this system was designed around remains open.

### Finding C — PCU weights are uncalibrated defaults

Motorcycle=0.2, Car=1.0, Bus=2.0, Truck=2.0, Container=3.0 are the same
starting values specified before any real data existed. Sprint 3's own
Outstanding Debt item #2 confirms no calibration has happened. This plausibly
explains the "sometimes right, sometimes wrong" behavior observed in the
screenshots, independent of Finding A.

### Finding D — Container vehicle class cannot be detected by the chosen model

Stock COCO-pretrained YOLOv8n (the model Sprint 3 adopted, after correctly
rejecting DatapolisX's slower weights) has no "container" class in its
standard 80-class output — only `truck`, `car`, `bus`, `motorcycle`. The PCU
formula's `Container = 3.0` weight, distinct from `Truck = 2.0`, cannot be
triggered by this model as deployed. Either this is dead code, or an
undocumented heuristic exists — needs a direct answer, not an assumption.

## 3. What Sprint 3 Did Right (acknowledged, not re-litigated)

- `docs/spike-vehicle-capacity.md` (S1): correctly refuses to auto-select an
  option, leaves the DoD checklist's human-confirmation item explicitly
  unchecked, and independently corrects an error in ADR-004 itself (Xa Lộ Hà
  Nội's camera cluster is actually captured under `mai_chi_tho`, not a
  separate corridor) — a good example of the audit process catching errors on
  both sides.
- S9 (flood-hex vs. radar separation): confirmed via screenshots — three
  independent toggles ("Dự báo radar" / "Camera ngập" / "Giao thông").
- The S2 execution-sequence inversion was handled correctly: disclosed
  explicitly with a full impact analysis (§4 of the Sprint 3 report), not
  hidden the way A4 was in Sprint 2.

## 4. Sprint 4 Decision

**No new user-facing feature work. No start on 2b/2c route-check.** Sprint 4's
job is verification and closing the gap between report and reality — the same
category of work as the H0 hotfix, at a larger scope.

## 5. Process Fix — Prevent a Third Recurrence

Add a mandatory Definition-of-Done item, applying to any issue that claims a
feature delivers real (non-mock) data to users: **a live production check
(an actual API response or screenshot from the real deployment, not a test
pass) must be attached to the PR before the issue can be marked complete.**
Update `docs/ISSUE_TEMPLATE_go_backend.md` and
`docs/ISSUE_TEMPLATE_frontend.md` accordingly. This is Sprint 4 item T8.

## 6. Sprint 4 Backlog

Estimates assume the team's demonstrated agent-assisted velocity (Sprint 3
delivered ~16 PRs in roughly 3 days); estimates are active-effort days, not
calendar days — T3 specifically has a calendar-time caveat noted inline.

| # | Title | Depends on | Est. | Notes |
|---|---|---|---:|---|
| **T1** | Confirm `cv-engine`'s real `model_loaded` status and `ANALYZER` value on the actual production host (lighter scope than originally planned — the suspected prod honesty-rule leak is resolved, no mock is leaking; this is now a routine pre-deployment check, not an incident investigation) | None | **0.25 day** | Quick confirmation only. |
| **T2** | Deploy real OpenVINO model weights to production; execute `DEVOPS_S3_OPENVINO_DEPLOYMENT.md` §2–5 for real; confirm all 4 Post-Deployment Verification checks pass against the live host | T1 | **1 day** | Budget extra time — ops work on unfamiliar ground tends to hit snags. |
| **T3** | Collect and label a real flood + traffic dataset via the S6 labeler, targeting the ADR-004 D6 threshold (≥300 frames, ≥60 flood-positive) | T2 (need real traffic frames flowing) | **1 day active labeling** + **ongoing passive accumulation** | Flood-positive frames depend on actual rain events — check current `data/proofs/` volume before estimating further; this may take 1–2 calendar weeks of passive collection regardless of active effort. |
| **T4** | Run `tools/eval/benchmark.py` for real against the labeled set; report precision/recall and flood false-negative rate; record the result in `docs/` as the first real D6 gate evidence | T3 | **0.5 day** | This is the actual gate — not the harness existing, the number it produces. |
| **T5** | Calibrate PCU weights against labeled traffic data; document the new values and rationale, replacing the untouched defaults | T3, T4 | **0.5–1 day** | |
| **T6** | Resolve the container-detection gap (Finding D): decide between (a) drop `Container` as a distinct PCU class, folding it into `Truck`, (b) add a container-detection step for container-relevant corridors only (e.g. near Cát Lái), or (c) accept an alternate model for that subset | T1 | **1 day decision + 0.5–2 days implementation** (depends on chosen option) | |
| **T7** | Build a small fixed smoke-test set of known-state cameras (confirmed empty vs. confirmed congested) and check model output against them as part of T4's run, not just anecdotal screenshots | T2 | **0.5 day** | Can fold into T4's session. |
| **T8** | Add mandatory "live production check" to both issue templates' Definition of Done (§5) | None | **0.5 day** | Mostly a docs change; do early, applies to every subsequent issue. |
| **T9** | Decision: keep self-hosted OpenVINO for traffic given T4's real numbers, or revert some/all affected cameras to the VLM adapter | T4 | **0.5 day** | Evidence-based, not assumed either way going in. |

**Total active effort**: roughly 6–8 days, likely spread over 2–3 calendar
weeks due to T3's data-accumulation dependency.

**Explicitly not in Sprint 4**: starting the `route.Engine` interface or any
2b/2c work. Revisit only after T9, and only if T9's answer is "keep" with
acceptable accuracy numbers — tentatively Sprint 5 at the earliest.
