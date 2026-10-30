## Lesson 29: Perception-Layer Fixes vs. Scoring Hacks & Empirical Validation of NMS and Letterboxing

- **Incident & Tracking:** Issue #221, Pull Request for S6-T1 (`tooling/s6-t1-cv-nms-letterbox-benchmark`).
- **Symptom:**
  In Sprint 4 and Sprint 5, traffic classification suffered from severe false alarm rates: 46 clear frames and 45 moderate frames were falsely classified as `CONGESTED`, while Macro F1 stagnated at 0.206. In Sprint 4 T5, the team attempted to fix this downstream by reducing `CapacityFloor` to 15.0, but that doubled false-positive CONGESTED classifications without fixing missed queues (Lesson 19).
- **Root Cause:**
  1. **Anchor Activation Explosion**: In `services/cv-engine/server.py`, vehicle counts were calculated using raw thresholded anchor summations (`np.sum(data[class_idx, :] > 0.45)`). A single real vehicle activated 5–12 overlapping anchor cells across feature levels, inflating 3 cars into 39 detections.
  2. **Non-Uniform Geometric Distortion**: Direct resizing from 16:9 (`512x288`) to square `320x320` distorted vehicles vertically by $1.78\times$, dropping confidence scores of real vehicle queues from $>0.55$ down below $0.37$.
  3. **Small-Object Blindness**: Coarse 320x320 resolution collapsed distant motorcycles (>25m) below the receptive field.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Fix Perception at the Perception Layer**: Implemented standard Non-Maximum Suppression (`nms_cpu` with $\text{IoU} \le 0.45, \text{conf} \ge 0.25$) and aspect-preserving letterboxing with neutral gray `(114, 114, 114)` padding, scaled to 416x416 resolution on `inference worker` (`[1, 3, 416, 416]` input tensor, `[1, 84, 3549]` output tensor).
  2. **Empirical Ground-Truth Verification (Rule 4 & Lesson 19)**: Re-ran the benchmark across the complete frozen 764-frame dataset (`tools/eval/dataset/labels_d6_real.json`):
     - **Traffic Macro F1** improved from **0.206 to 0.245** (+18.9% relative increase).
     - **Traffic Weighted F1** improved from **0.351 to 0.402** (+14.5% relative increase).
     - **CONGESTED False Alarms** on CLEAR frames dropped by **50.0%** (from 46 frames down to 23).
     - **MODERATE Detections** tripled from 21 to 63 correct classifications (+200%).
     - Proved that resolving perception layer artifacts directly provides a sound foundation for downstream corridor capacity calibration in S6-T2.
