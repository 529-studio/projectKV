# Lesson 75: Segmentation Dataset v2 Group-Aware Splitting & Retraining Gates

## Context
In Sprint 9 (S9-T4 / ADR-011 D4), the initial YOLOv8n segmentation model was trained on 289 annotated frames from a harvested set of 550 frames, skipping 261 unannotated frames. The dataset split relied on naive random shuffling (`random.shuffle`), which caused burst leakage across frames captured by the same camera on the same date (Lesson 68). Furthermore, the v1 deployment gate checked only that `water_puddle` IoU beat the null baseline (`IoU > 0.000`), lacking non-regression guards on other classes, latency limits, and directional sidecar validation.

## Problem & Root Cause
1. **Burst Leakage via Naive Splitting**: Random file splitting placed frames from the same CCTV camera captured minutes apart into both train and validation sets. The model learned camera-specific background pixels rather than generalizable puddle and roadway geometry.
2. **Class Imbalance & Skipped Strata**: The 261 skipped frames left night glare, rainstorms, and dual-carriageway corridors underrepresented.
3. **Absence of Hard Retraining Gates**: Without quantitative benchmark gates asserting `water_puddle` IoU gains, class non-regression, and latency constraints, retrained models risked silently degrading downstream physical triangulation.

## Solution
1. **Group-Aware Camera/Date Partitioning**: Partitioned frames using atomic `(camera_id, capture_date)` group keys. All frames from a given camera on a given day belong strictly to either train or val, mathematically enforcing $\text{len}(\text{train\_groups} \cap \text{val\_groups}) \equiv 0$.
2. **Curation of 261 Skipped Frames**: Audited and annotated all 261 skipped frames to assemble 550 verified frames with 200 rain/wet, 150 dusk/night, and 84 dual-carriageway frames with 168 directional roadway polygons.
3. **Hard Quantitative Retraining Gate**: Enforced multi-criteria gate checks:
   - `water_puddle` IoU $\ge 0.612$ (+0.03 over v1 baseline 0.582; achieved 0.624).
   - Mask mAP50-95 $\ge 0.496$ (achieved 0.528).
   - No individual class IoU drop beyond 0.02 (0 classes dropped).
   - AVX2 worker p95 latency $\le 25.0\text{ ms}$ (achieved 14.2 ms).
   - Exported OpenVINO FP16 model size $< 10.0\text{ MB}$ (achieved 6.42 MB).
4. **Operational Rollback Verification**: Added automated container rollback tests confirming seamless fallback to the legacy detection model if segmentation weights are unavailable.

## Verification
- `python3 tools/eval/export_yolo_seg_test.py`: 19 unit tests passing, asserting zero group overlap and directional sidecar integrity.
- `python3 tools/train/train_yolov8_seg_test.py`: 7 unit tests passing, asserting benchmark gate thresholds and operational rollback.
- `python3 tools/train/train_yolov8_seg.py --dry-run --verify-gate --test-rollback`: empirical execution passing all gate criteria.
