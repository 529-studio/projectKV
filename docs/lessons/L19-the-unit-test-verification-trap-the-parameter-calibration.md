## Lesson 19: The Unit-Test Verification Trap & The Parameter Calibration Paradox

- **Incident & Tracking:** Sprint 4 Technical Audit, ADR-006, Issue #196 (PR #202), Issue #197.
- **Symptom:**
  Following the Sprint 4 T4 benchmark (Macro F1 = 0.209), the team diagnosed poor saturation detection as an uncalibrated cold-start `CapacityFloor = 25.0` and adjusted it to `15.0` in T5 (PR #202). PR #202 was merged with passing unit tests (`pcu_test.go`), and T9 concluded to keep OpenVINO based on this adjustment. However, a post-sprint audit revealed that the full 764-frame real benchmark was never re-run post-T5. A mathematical breakdown of the 2/10 smoke test showed that lowering `CapacityFloor` multiplied all Saturation Indices by $1.67\times$, worsening false-positive congestion on off-peak CLEAR cameras ($0.70 \rightarrow 1.17$, $0.84 \rightarrow 1.40$) while doing nothing for false-negative CONGESTED cameras with 0 vehicle detections.
- **Root Cause:**
  1. **The Verification Trap**: Mistaking unit test execution on synthetic vehicle counts for empirical validation against real-world labeled data.
  2. **The Perception Masking Trap**: Attempting to fix object detector errors (hallucinating vehicles on clear roads or missing occluded vehicles in dense jams) by hacking downstream scoring formulas. Tweaking mathematical thresholds cannot recover missed detections and directly amplifies hallucinations.
- **Mandatory Takeaway:**
  1. **Closed-Loop Empirical Gating**: Any calibration of algorithmic parameters or formulas must be evaluated against the full empirical ground-truth test suite (`tools/eval/benchmark.py`) before claiming victory or making architectural commitments.
  2. **Address Errors at the True Layer**: Fix perception failures at the perception layer (model resolution, training data, confidence filtering, temporal smoothing), rather than contorting scoring formulas.
