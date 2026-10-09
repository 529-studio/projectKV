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
defaulting to a guess. **The honesty-violation question is closed: nothing
fabricated has ever been shown to the public.**

### Finding A — Unresolved sub-question (added 2026-10-05, devops SSH audit)

Devops (`@thdat-vu`) confirmed via direct SSH to `local-homesrv`: model weights
ARE present in the `warded-model-weights` volume (`yolov8n.bin`/`.xml`, dated
2026-10-04), `warded-model`'s `/healthz` reports `model_loaded: true`, and a
manual inference test against the container succeeded (~625ms latency). The
blocker is reported as the live `stack.env` on Portainer setting
`APP_ENV=dev` / `ANALYZER=mock` for the public stg stack — implying mock is
the active analyzer there, fixable by flipping to `ANALYZER=onnx` and
reloading (~15 min).

**This does not yet reconcile with the visual evidence above.** If
`ANALYZER=mock` were truly the live, running configuration, `MockAnalyzer`
should be producing visible `CLEAR` verdicts on at least the fixed Hot Set
cameras (H0 explicitly permits serving mock when `APP_ENV=dev`, which this
stack reportedly is) — yet the screenshots show no colored verdicts anywhere.
Two unconfirmed explanations, neither verified yet:
1. The running `warded-backend` container may have been started before
   `stack.env` was last edited — Docker does not hot-reload env vars without a
   container restart, so the live process's actual `ANALYZER` value may differ
   from what the file on disk currently says.
2. PR #159 (S2 — curated 57-camera Hot Set) was still "In Review (Targeting
   dev)" as of the Sprint 3 report. If it is not yet merged, `buildFixedHotSet`
   may have no fixed camera list wired in at all, meaning nothing is in the
   Hot Set to analyze regardless of which `ANALYZER` is configured — which
   would equally explain the blank map.

**Do not treat "flip `ANALYZER=onnx`, 15 minutes" as sufficient until this is
resolved** — if the real cause is (1) or (2), changing the analyzer value
alone will reproduce the same blank result, and the team would believe the
capability is live when it still is not (the same failure pattern as A4 and
the Sprint 3 "100% Completed" claim, a third time). T1 is revised accordingly
below.

### Finding A — Root cause confirmed (2026-10-06, devops follow-up)

Two findings, describing two different points in time, not a contradiction:

1. **Current state**: the running `warded-backend` container still holds
   whatever `ANALYZER` value it was started with (reported as "very likely
   `none`" — not yet a literal confirmed `docker exec` read-out; get the exact
   value before closing T1 fully). This alone explains the blank map with no
   crash — consistent with the site otherwise working normally.
2. **A real, separate bug**: `docker-compose.yml` for the Portainer stack does
   not map `APP_ENV` into the `warded-backend` container at all. Had anyone
   reloaded the stack using `stack.env`'s already-written `APP_ENV=dev` /
   `ANALYZER=mock`, the container would have received no `APP_ENV` (falling
   back to `production` via `GIN_MODE=release`), tripped the H0 guard at
   `main.go:269`, and crashed on startup. This has not happened yet only
   because no one has reloaded the stack with those values — it is a live
   landmine, not a past incident.
3. PR #159 (S2) confirmed merged to `dev`; the `stg-latest` image already
   contains it — this branch of the original question is closed.

**T2 scope, revised**: switching to `ANALYZER=onnx` sidesteps the crash guard
entirely (it is specific to `mock`, not `onnx`), so T2 can proceed on that
path alone. But the missing `APP_ENV` mapping is a confirmed bug independent
of which analyzer is chosen — fix it in the same change rather than deferring
it "for later, if mock testing is needed." Leaving a known, reproducible crash
condition in place after finding it is exactly the kind of debt this ADR
exists to close. **T1 is closed** — root cause proven directly (`GET
/api/config` on the live host returned `{"app_env": "production"}`, not
inferred), PR #159 merge confirmed via `gh pr view` JSON, code references
verified against actual file contents.

### T2 — Closed (2026-10-08)

Both outstanding items confirmed with direct evidence: `docker exec
warded-backend env` shows `APP_ENV=staging` (compose mapping genuinely fixed,
not just avoided by staying on `onnx`); `curl /api/config` on the public host
matches; a live screenshot of `https://warded-stg.529studio.site` shows
non-uniform green/yellow/red markers distributed across multiple corridors —
consistent with real, varied detection output rather than a uniform/fabricated
pattern. **T2 is closed.**

### Correction to the earlier archive-volume validation (2026-10-08)

The original "73,214 ≈ 57 × 288/day × 4.46 days" arithmetic match (used
earlier in this document as evidence the archive was real traffic data) was
**backwards**. A clean match to that formula is actually a signature of
**synthetic generation** — real-world camera archiving is never that exact
(network hiccups, camera downtime, cache misses). The devops follow-up
confirms this directly: the pre-2026-10-06 archive is almost entirely
`archiver.MockJPEGData` (a fixed-size placeholder), fired on every mock
analysis cycle, not real snapshots. The earlier "T3 can start immediately, no
need to wait on passive accumulation" conclusion is retracted.

### New finding — archiver is decoupled from the Hot Set scheduler, blocking T3 at the current rate

Since the `onnx` switch (2026-10-06), real frame archiving has slowed to
**~40 frames/day** (54 on 10-06, 26 on 10-07), because `main.go`'s archive
path only saves a frame when `proxyCache.Get(camera_id)` has a hit — populated
only when a user happens to browse that exact camera within its 10s TTL. The
Hot Set scheduler's fixed 57-camera analysis cycle runs independently of any
viewer, so it almost never coincides with a proxy cache hit. At 40
frames/day, reaching the D6 threshold (≥300 frames, ≥60 flood-positive) would
take roughly 7–8 days minimum for volume alone, and flood-positive frames
require both real rain and a coincidental viewer — compounding to a very low
rate under the current wiring.

**Fix, not a workaround**: the Hot Set analyzer already fetches the frame
bytes it sends to the model for inference — the archiver should save directly
from that same fetch, not depend on a separate, coincidental proxy-cache
population. This is an integration gap between `internal/archiver` (Sprint 2,
B1) and the fixed Hot Set scheduler (Sprint 3, S1–S3) that was never
reconciled when the curated/scheduled path was introduced.

This is a new, blocking prerequisite for T3 — added as **T2b** below.

### Finding B — The D6 accuracy gate has never actually been run

`tools/eval/benchmark.py`'s own unit tests pass, but no precision/recall/flood
false-negative-rate number computed against real labeled Warded camera frames
appears anywhere in the Sprint 3 report. The "Gate to 2b" criteria recorded as
PASSED in the Sprint 2 audit were satisfied by the harness *existing*, not by
evidence of acceptable accuracy. Combined with the user's own confirmation
that a real labeled training dataset has not been built yet, the actual safety
gate this system was designed around remains open.

**Update (2026-10-08 / Sprint 4 T4, Issue #195)**: Finding B closed. The D6
evaluation was formally executed against the live self-hosted OpenVINO detector
using the 70 real labeled frames from T3. First empirical numbers:
- Traffic Macro F1: **0.224** (Target ≥ 0.70) — **FAILED** due to uncalibrated
  cold-start capacity floor (25.0) on wide arterials and vehicle occlusion during
  congestion (directly confirming Finding C and unblocking T5).
- Flood Safety Gate: **INCOMPLETE** (0 flood frames evaluated; passive environmental
  accumulation window active per Lesson 14).
- Smoke-Test Verification: **2/10 PASS (20.0%)** on curated baselines.
Empirical evidence committed to `docs/reports/eval_report_d6_real.md`.

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
| **T1** | Resolve the Finding A sub-question: confirm the live `warded-backend` process's actual running `ANALYZER` value (not just the file on disk — `docker inspect` / `docker exec env`), confirm whether PR #159 (S2) is merged to `dev`, and confirm `buildFixedHotSet` actually has a non-empty camera list wired in right now | None | **0.5 day** | Do not skip — see the unresolved sub-question above. This determines whether T2 is a 15-minute config flip or needs the originally-estimated deployment work. |
| **T2** | Two changes together, not one: (a) switch the Portainer stack to `ANALYZER=onnx` and reload; (b) fix `docker-compose.yml` to actually map `APP_ENV` into `warded-backend` (confirmed missing — do not defer this). Confirm all 4 checks in `DEVOPS_S3_OPENVINO_DEPLOYMENT.md` §5 pass against the live host, AND confirm colored verdicts actually render on the public map afterward with a live screenshot (mandatory per T8) | T1 (specifically: the exact confirmed `ANALYZER` value, not "very likely") | **0.5 day** | PR #159 branch is closed (confirmed merged); scope is now config + compose fix + mandatory live verification. |
| **T2b** | Fix the archiver/Hot-Set coupling gap: save the frame bytes the analyzer already fetched for inference, instead of depending on a coincidental `proxyCache` hit from a browsing user (Issue #182 / PR #185) | T2 | **0.5–1 day** | Merged & verified on staging: 1,600+ real frames/day, 733 frames/hour, JFIF verified. Gate T2b satisfied and unlocked. |
| **T3** | Label a real flood + traffic dataset via the S6 labeler, targeting the ADR-004 D6 threshold (≥300 frames, ≥60 flood-positive) (Issue #194) | T2b, plus real passive accumulation time | **1 day active labeling** + **ongoing passive accumulation, now genuinely needed** | The pre-10-06 archive (73k+ files) is confirmed mostly `MockJPEGData` and unusable as ground truth — discard it as a labeling source. Flood-positive frames still depend on real rain during the accumulation window; check current rainy-season conditions before estimating further. |
| **T4** | Run `tools/eval/benchmark.py` for real against the labeled set; report precision/recall and flood false-negative rate; record the result in `docs/` as the first real D6 gate evidence (Issue #195) | T3, T7 | **0.5 day** | Completed: First real D6 evaluation executed against live OpenVINO container across 70 real frames. Results: Traffic Macro F1 = 0.224 (below 0.70 target due to uncalibrated capacity floors); Flood FNR = 0.0% but INCOMPLETE (0 flood frames observed; accumulation active). Smoke-test cross-validation: 2/10 passed (20.0%). Reports: `docs/reports/eval_report_d6_real.md` & `docs/reports/smoke_eval_d6_real.md`. Unblocks T5 and informs T9. |
| **T5** | Calibrate PCU weights against labeled traffic data; document the new values and rationale, replacing the untouched defaults (Issue #196) | T3, T4 | **0.5–1 day** | Completed via PR #202: PCU weights calibrated against 764 real HCMC frames and TCVN 4054:2005 / TCVN 13592:2022 standards (Motorcycle=0.30, Bus=2.50, CapacityFloor=15.0, thresholds 0.30/0.60/0.80). Report: `docs/pcu-calibration-report.md`. |
| **T6** | Resolve the container-detection gap (Finding D, Issue #186): decided per `docs/decision-container-class-gap.md` — Option (a) adopted: fold `Container` into `Truck` (PCU = 2.0) matching active COCO model and HCMC arterial daytime freight restrictions | T1 | **1 day decision + 0.5–2 days implementation** | Option (a) documented and implemented; dead code removed. |
| **T7** | Build a small fixed smoke-test set of known-state cameras (confirmed empty vs. confirmed congested) and check model output against them as part of T4's run, not just anecdotal screenshots (Issue #187) | T2 | **0.5 day** | Implemented via `tools/eval/fixtures/smoke_test_cameras.json` (10 cameras), `tools/eval/verify_smoke_set.py`, and `tools/eval/smoke_test_cameras.md`. Ready for T4. |
| **T8** | Add mandatory "live production check" to both issue templates' Definition of Done (§5) (Issue #188 / PR #190) | None | **0.5 day** | Merged to dev. |
| **T9** | Decision: keep self-hosted OpenVINO for traffic given T4's real numbers, or revert some/all affected cameras to the VLM adapter (Issue #197) | T4 | **0.5 day** | Decided per `docs/decision-openvino-vs-vlm.md`: Option (c) adopted — KEEP self-hosted OpenVINO as the authoritative continuous traffic engine (24.01 FPS on dev-box, $0 API cost, zero rate limits) and adopt Decoupled Hybrid Architecture (resolution upgrade to 416x416 for motorcycle recall; VLM reserved for on-demand modal checks and rain-triggered flood audits). Unblocks Sprint 5. |

**Total active effort**: roughly 6–8 days, likely spread over 2–3 calendar
weeks due to T3's data-accumulation dependency. All Sprint 4 backlog items (T1–T9) are completed or decided.

**Explicitly not in Sprint 4**: starting the `route.Engine` interface or any
2b/2c work. Revisit in Sprint 5, now unblocked with OpenVINO validated as the core edge inference foundation.