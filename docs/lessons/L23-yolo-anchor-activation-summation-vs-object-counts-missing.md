## Lesson 23: YOLO Anchor Activation Summation vs Object Counts (Missing NMS), and Ground-Truth Sanity Checks

- **Incident & Tracking:** Issue #208, Sprint 5 Task S5-T2 (`tooling/s5-t2-cv-detection-audit`).
- **Symptom:**
  In Sprint 5 evaluation, the Go PCU Saturation Index ($SI$) exhibited two extreme failure modes that could not be reconciled by formula adjustments:
  1. Wide-open arterial roads with only 3 to 5 physical cars (`Mai Chí Thọ - Nguyễn Cơ Thạch 1`, `Võ Văn Kiệt - Ký Con`, `Điện Biên Phủ - Pasteur`) were reported as $SI \ge 0.51 - 1.00$ (HEAVY / CONGESTED).
  2. Severe commuter bottlenecks with dense queues (`Cộng Hòa - Trường Chinh`) were reported as $SI = 0.00$ with 0 detected vehicles.
- **Root Cause:**
  1. **Direct Anchor Summation without Non-Maximum Suppression (NMS)**:
     In `services/cv-engine/server.py`, vehicle counts were calculated as `int(np.sum(data[class_idx, :] > 0.45))` directly over the raw YOLOv8 `(1, 84, 2100)` output tensor. In anchor-free multi-scale object detection, a single physical vehicle activates 5 to 12 overlapping feature cells across strides P3, P4, and P5. Without NMS deduplication, each physical car was multiplied by 2.7x to 4.7x, and large vehicles triggered both `truck` and `bus` classes, converting 3 to 5 cars into 28 to 39 detected objects.
  2. **Aspect Ratio Distortion Suppressing Detection Confidence**:
     Snapshots are $512 \times 288$ (16:9). Resizing directly to $320 \times 320$ stretched height by $1.78\times$ relative to width, de-tuning convolutional receptive fields and depressing peak detection confidences on real queues from $0.55+$ down to $0.37$, pushing real vehicles below the rigid $0.45$ threshold.
  3. **Synthetic Ground Truth Assumption Errors**:
     Camera `Mai Chí Thọ - Đồng Văn Cống 2` was assumed to be chronic CONGESTED during rush hours in smoke fixtures. Visual audit proved the physical road was an active construction zone blocked by barriers and completely empty. The model's prediction of 0 vehicles was physically accurate; the benchmark error was an ungrounded label.
- **Mandatory Takeaway:**
  1. **NMS is Non-Negotiable in Object Detection**: Raw neural network output tensors must NEVER be summed to count physical entities. Object detectors always require Non-Maximum Suppression (IoU $\le 0.45$) to collapse multi-anchor activations into discrete physical instances.
  2. **Preserve Input Aspect Ratios with Letterboxing**: Never stretch non-square images directly to square model dimensions. Always apply aspect-preserving letterboxing with neutral padding to maintain convolutional geometric priors.
  3. **Visual Inspection Before Formula Tuning (Rule 4)**: Never tune downstream math (e.g. lowering `CapacityFloor` or adjusting threshold curves) to compensate for upstream perception anomalies without visually examining raw image frames and ground-truth annotations.
