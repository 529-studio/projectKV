# ADR-006: Sprint 5 Plan — Closing the T9 Empirical Gap

**Status**: Proposed
**Date**: 2026-10-10
**Related**: ADR-004 (D6 accuracy gate, D7 storage trigger), ADR-005 (Sprint 3/4 audit), `docs/sprint4_audit_qa.md` (source of findings below)

## 1. Context

Sprint 4 closed all 9 backlog items (T1-T9) and additionally shipped an
unplanned infrastructure migration (CV inference moved from `local-homesrv`
to a new worker, `dev-box`). A follow-up audit (`sprint4_audit_qa.md`)
found that T9's "keep self-hosted OpenVINO" decision was reached from a
theoretical diagnosis (uncalibrated `CapacityFloor`) that was never
re-verified empirically after the fix (T5) was applied — violating the
project's own stated rule that unit tests are not acceptable proof of
correctness. Further analysis of the pre-T5 smoke-test data shows the T5
fix may actively worsen one of the two failure modes it was meant to solve.
This ADR scopes Sprint 5 to close that gap before any further feature work.

## 2. Key Finding — T5's calibration may worsen false-positive congestion

$SI = PCU_{current} / CapacityFloor$. Lowering `CapacityFloor` from 25.0 to
15.0 multiplies every unchanged $SI$ by $25/15 \approx 1.67\times$. Applied to
the 3 non-zero false-positive `CLEAR` cameras from the pre-T5 smoke test
(reported $SI$ = 0.70, 0.51, 0.84), the post-T5 values would be approximately
**1.17, 0.85, 1.40** — moving them from over-reporting `MODERATE`/`HEAVY` to
over-reporting `CONGESTED`, strictly worse. The 2 zero-detection
`CONGESTED` failures ($SI = 0.00$) are unaffected by any floor/weight change,
since $0 / x = 0$ regardless — that failure mode is in the detection layer,
not the scoring formula. **Real re-measurement (S5-T1) is the only way to
know the actual post-T5 state; do not assume T5 was net-positive.**

## 3. Sprint 5 Decision

No new feature work starts before S5-T1 produces real, current numbers.
Everything else in this backlog is sequenced around that result.

## 3a. S5-T1 — Closed (2026-10-10), confirms the predicted regression

Real re-run on the frozen 764-frame set (controlled, same images both times):
Macro F1 0.209 → 0.206, Weighted F1 0.364 → 0.351 — flat to slightly worse,
not improved. CLEAR frames misclassified as CONGESTED rose from 22 to 46
(+109%), matching the §2 math prediction almost exactly. Flood FNR unchanged
at 100% (expected — COCO has no flood class, T5 cannot touch this).
**Confirms: parameter calibration alone does not fix the underlying detector
problem — S5-T2 is the real next step, not optional.**

**Methodology flag on the smoke-test figure**: the reported smoke-test
improvement (20% → 50% pass rate) is not fully trusted as a measurement of
T5's effect. One camera (`66b1c426...`, Điện Biên Phủ) flipped from `SI=1.00
PASS` to `SI=0.00 FAIL` between runs — a floor-value change (×1.67 multiplier
on a nonzero SI) cannot produce an exact zero, so this delta must come from a
different vehicle count between the two runs, not from the T5 parameter
change itself. This suggests `verify_smoke_set.py` may re-fetch a live
current snapshot each run rather than reusing a fixed reference image,
making before/after smoke-test comparisons methodologically invalid (it
conflates the calibration effect with real-world traffic differing between
the two test moments). **Trust the 764-frame controlled result (flat/worse),
not the smoke-test delta, as the basis for S5-T2.** Fix the smoke-test
harness to use frozen reference images for any future before/after
comparison — fold into S5-T2's scope.

## 3b. Blob storage — accepted for Sprint 5, for a different reason than D7

The ADR-004 §D7 disk-volume trigger (>1M files / >200GB) has **not** been
hit — real daily volume remains small (tens of files/day as of 2026-10-09).
That specific justification still does not apply, and this document's
earlier position on it was correct as stated.

A **different, independent justification** was given and is accepted:
centralizing snapshot storage enables **collaborative labeling** — currently
each developer must copy frames to their own machine's
`services/go-server/data/proofs/` to use `labeler.html`, with no shared
ground-truth source. A team-accessible web labeler reading directly from
object storage solves a real coordination problem, unrelated to disk
pressure. **This is now S5-T4 (promoted from an open question to a real
ticket), justified by this reason — not because the D7 threshold was met.**

**Mandatory first step, non-negotiable**: the R2 credentials shared in the
integration guide were plaintext in a markdown document, not a `.env`
template with placeholders. Treat them as compromised — rotate the Access
Key/Secret in the Cloudflare R2 dashboard before any other S5-T4 work, and
ensure the committed guide in `docs/` contains only the variable names and
structure, never real key values. This is independent of and prerequisite to
the rest of S5-T4.

## 4. Sprint 5 Backlog

| # | Title | Depends on | Est. | Notes |
|---|---|---|---:|---|
| **S5-T1** | Re-run `benchmark.py` (D6) and `verify_smoke_set.py` against the 764 labeled frames with current (post-T5) PCU weights; publish updated `eval_report_d6_real.md` and `smoke_eval_d6_real.md` | None | **0.5 day** | Execution only, no new code. Blocking everything else below. |
| **S5-T2** | Object-detection audit: investigate false-positive vehicle hallucination (background/night reflections) and missed detections on occluded queues, using S5-T1's fresh per-camera breakdown | S5-T1 | **1-2 days** | Scope narrows based on what S5-T1 actually shows — don't pre-commit to a fix before seeing the new numbers. |
| **S5-T3** | Decoupled Hybrid Flood pipeline: event-driven VLM dispatch on rain-radar trigger + on-demand dispatch on camera-modal click; curate a real `flood_chokepoint_cameras.json` from authentic municipal flood data (replacing the fabricated "10 chokepoints" reference); correct the rainfall-threshold mismatch between `decision-openvino-vs-vlm.md` (2.0mm/h, unverified) and the actual code thresholds (`rainHexUtils.ts`: 1.0/4.0/15.0mm) — pick one, document the real basis or mark it explicitly as a heuristic pending calibration | None (independent of T1/T2), but hold start until S5-T1 lands per project discipline | **2-3 days** | This is real feature work — the first since Sprint 2's H0 hotfix era. Scope it carefully; do not let it expand informally the way `dev-box` did in Sprint 4. |
| **S5-T4** | Physical disconnection test: `tailscale down` on `dev-box` against live staging, confirm graceful degrade-to-`unknown` end-to-end (not just unit-tested); verify or fix the `profiles: ["model-local"]` fallback mentioned in the Sprint 4 audit but not confirmed working | None | **1 day** | Closes the one gap Q6/Q7 explicitly flagged as untested. |
| **S5-T4** | Cloudflare R2 (S3-compatible) blob storage: upload archiver frames to `warded-proofs`; build a minimal shared web labeler reading from R2 so the team labels from one common source instead of per-machine local copies | None technically, but rotate R2 credentials FIRST (see §3b) before any other work | **1.5-2 days** | Justified by collaborative-labeling need (§3b), not the D7 disk-volume trigger, which has not been hit — note this distinction in the ticket itself so it isn't misread later as "D7 threshold reached." |

## 5. Documentation corrections to make alongside S5-T3

- `docs/decision-openvino-vs-vlm.md`: correct the "10 known flood chokepoints"
  reference — it was a conflation with the unrelated 10-camera traffic
  smoke-test set (`smoke_test_cameras.json`), not a real flood-chokepoint list.
- Same file: the "$0 cost" framing for `dev-box` should be qualified as
  API/SaaS cost only — physical hosting (power, bandwidth, hardware wear on
  a team-owned homelab machine) is a real, non-zero cost not previously
  disclosed in the decision record.