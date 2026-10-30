## Lesson 15: Empirical Ground-Truth Auditing & Cold-Start Capacity Underestimation

- **Incident & Tracking:** Issue #195, ADR-005 Finding B/C/D6 (`feat/195-run-d6-accuracy-gate`).
- **Symptom:**
  Running the formal ADR-004 §D6 accuracy gate on real camera frames against live OpenVINO container inference revealed a severe performance drop: Traffic Macro F1 dropped to **0.224** (far below 0.70 target), and the known-state smoke-test set suffered an **80% discrepancy rate** (only 2/10 passed). Furthermore, the flood safety gate previously reported false "PASS" status on mock evaluations.
- **Root Cause:**
  1. **Uniform Cold-Start Capacity Underestimation**: A fixed global `capacityFloor = 25.0` PCU severely underestimates wide 6–8 lane arterial sections (Mai Chí Thọ, Võ Văn Kiệt), causing small platoons of 8–10 passing vehicles to trigger false-positive `HEAVY` or `CONGESTED` classifications during off-peak hours.
  2. **Gridlock Occlusion Degradation**: Under dense congestion on chronic bottlenecks (Cộng Hòa), severe vehicle bumper-to-bumper occlusion and rain distortion degrade bounding-box detections in stock 320x320 YOLOv8n, causing the detector to under-count stopped traffic and trigger false-negative `CLEAR` verdicts.
  3. **Zero-Sample Gate Illusion**: Mathematical division of $0/0$ false negatives previously defaulted FNR to $0.0\%$, producing an illusory passing badge when zero flood-positive frames existed in the dataset.
- **Mandatory Takeaway:**
  1. **Strict Gate Integrity for Zero Positives**: When evaluating a safety-critical class with zero ground-truth positive samples, the evaluation verdict must be strictly declared **INCOMPLETE**, never PASS.
  2. **Corridor-Specific Capacity Parameterization**: Road geometry varies drastically between multi-lane freeways ($50+$ PCU capacity) and narrow intersection bottlenecks ($15–20$ PCU capacity). Global constants cannot substitute for parameterized or calibrated capacity baselines (mandatory for Sprint 4 T5).
  3. **Model Decoupling & Hybrid Routing (T9)**: Single-model object detectors cannot detect environmental flood water. Safety-critical systems require decoupled multi-modal or hybrid routing architectures.
