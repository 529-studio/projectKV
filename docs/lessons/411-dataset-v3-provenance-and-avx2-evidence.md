# Lesson 411: Labels Without Their Frames Are Not a Dataset

## Context

Sprint 12 Issue #411 (S12-T5) had to turn the segmentation model from a
correctly-shaped but untrained graph into measured evidence. The staged
`data/yolo_seg` and `data/yolo_seg_v2` trees looked like datasets: 301 and 550
label files, plausible polygons, a documented split. The audit in
`docs/sprints/s12_t5_spec.md` §1.3 found three independent defects in them, and
each defect had the same consequence: any metric computed from those labels
measures the label pipeline, not the road.

## Problem & Root Cause

1. **The exact timestamp join was empty.** Labels were named
   `<camera>_sam_prelabel_<unix_ts>`; proof frames were
   `<date>/<camera>/<unix_ts>_sample.jpg`. Joining all 301 labels against the
   November proof tree produced **0/301** matches. Only 25 of 206 cameras
   reappeared, on a different date, so a camera-only join would have bound
   October polygons to November pixels and poisoned every downstream number.
2. **The October bytes no longer existed.** R2 held no objects for
   `2026-10-08`, `-13` or `-15`; retention had dropped the frames. The labels
   were orphans, not data.
3. **The staged split leaked.** 239 train + 62 val files resolved to 289 unique
   stems: 12 frames sat in both splits.
4. **47 % of v2 was fabricated.** `curate_v2_dataset.py` filled 261 skipped
   frames with fixed, hash-jittered template polygons stamped
   `source: sam_prelabel`, and overwrote real roadway polygons on 84
   dual-carriageway frames. A gate scored against those labels measures
   agreement with a template.
5. **The gate could pass without a measurement.** `CANONICAL_V2_METRICS`
   hardcoded `water_puddle 0.624 / mAP50-95 0.528`, and
   `evaluate_retraining_gate(metrics=None)` copied them while labelling the
   verdict `UNVERIFIED`. A PASS on a copied constant is worse than no gate.

## Solution

1. **Retire the orphaned sets.** Dataset v3 is built only from the live proof
   archive. `tools/eval/materialize_seg_dataset.py` resolves each frame by exact
   `snapshot_key` from local roots before R2, and a local hit never triggers a
   network call.
2. **Pair on the exact key, never on the camera.** `match_pairs` indexes labels
   by `(camera_id, timestamp)`; a label whose camera appears but whose timestamp
   does not is reported as `camera_only` and `assert_pairs_buildable` raises
   before any miss is reported, so the dangerous fallback can never be silently
   chosen.
3. **Prove every frame.** SOI `FF D8 FF`, Pillow decode, CMYK rejection,
   320x240..4000 px bounds, sha256, duplicate stem and duplicate digest
   rejection. A miss is fatal and listed; nothing is skipped.
4. **Build atomically.** The tree is assembled in `<out>.tmp-<pid>` on the
   target filesystem, checked for orphans in both directions, then swapped; a
   non-empty `--out` requires `--replace` and is archived to
   `<out>.bak-<timestamp>`, so new labels are never merged into stale ones.
5. **Freeze the split before annotation can bias it.** Group key is
   `(camera_id, capture_date)`; `split.json` binds seed, assignment and manifest
   digest. Re-running with a different seed exits 2 without touching a byte.
6. **Make the gate require a measurement.** `--verify-gate` without `--metrics`
   exits 2 unless `--allow-untrained-head`, which keeps the verdict `UNVERIFIED`
   and exits 1 for promotion. The loader refuses `provenance: reference
   baseline`, a reordered class list and a stability margin above 0.02.
7. **Fix the boundary that hid a compliant model.** `0.721 - 0.741` is
   `-0.020000000000000018` in IEEE-754, so a drop of exactly the documented
   0.02 tolerance was reported as a violation. Round the delta to four decimals
   before comparing.
8. **Refuse non-AVX2 latency evidence.** `bench_seg_latency.py` requires Linux,
   `x86_64` and `avx2` in `/proc/cpuinfo`; anything else exits 2 and writes no
   evidence file. The gate number is the median of three per-repeat p95 values,
   with the worst repeat disclosed beside it.

## Verification

- `KV_TDD_STRICT=1 python3 -m unittest tools/eval/materialize_seg_dataset_test.py`:
  44 tests, including the 301-set negative control (0 matches, 12 duplicate
  stems) and the frozen-split refusal.
- `KV_TDD_STRICT=1 python3 -m unittest tools/train/train_yolov8_seg_test.py`:
  33 tests, including both sides of every quantitative boundary.
- `KV_TDD_STRICT=1 python3 -m unittest tools/train/bench_seg_latency_test.py`:
  24 tests, including Darwin/arm64 and non-AVX2 refusal with zero evidence.
- End-to-end CLI scenarios: materialize (0 orphans, frozen re-run, seed
  mismatch exit 2 with a byte-identical tree), retired-set control, and the
  Darwin host refusal that creates no evidence directory.

## Guardrail

A label is not evidence until its own pixels are present and its own split is
frozen. Pairing by camera, filling gaps with templates, or copying a previous
report's numbers all produce the same failure: a gate that measures the
pipeline that wrote the label instead of the road the camera saw.
