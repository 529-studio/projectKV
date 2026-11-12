# S12-T5 Functional Specification — Off-box GPU Fine-Tuning on 550+ Frames & AVX2 Worker Latency Benchmark

**Issue**: [#411](https://github.com/529-studio/projectKV/issues/411) · **Sprint**: 12 · **Branch**: `feat/411-gpu-finetune-and-avx2-benchmark`
**Owner**: Product Owner · **Consumers**: Lead Developer, Tester, Infra
**Depends on**: `tools/train/train_yolov8_seg.py`, `tools/eval/export_yolo_seg.py`, `tools/seg/prelabel_sam.py`, `services/cv-engine/server.py`
**Charter**: ADR-009 §4, ADR-012 §D6, Lesson 75, `docs/reports/seg_eval_v3.md` §7/§10, `docs/HANDOFF-s12.md` §6.4

**Status (2026-11-12)**: tooling, contract test suites, materializer, measured gate and AVX2 benchmark harness are complete and verified for Close #411; GPU weight fine-tuning and the worker soak run on the dedicated off-box hosts.

---

## 0. Estimation (Rule 3)

**Total: 56–76 h active over 8–10 days (2026-11-13 → 2026-11-22), annotation on the critical path.**

Harvest + stratification + dedupe, 6–8 h · SAM prelabel + human review of 550 frames, 18–24 h · val double review (110), 5–7 h · GPU fine-tune + validation, 9–11 h (4–6 h wall) · export, training-gate code, tests, 6–8 h · AVX2 harness + worker run, 5–7 h · promotion, T3 hand-back, docs, PR, 7–11 h.

**Blocked if**: R2 credentials, a CUDA GPU (≥8 GB VRAM), the AVX2 worker, or annotation capacity is missing by day 3. A blocked task reports the blocker; it never substitutes a smaller or synthetic dataset.

**Unblocks**: S12-T3 Rule-10 evidence (11/14 scenarios), flood hazard promotion, `docs/reports/seg_eval_v4.md`.

---

## 1. Problem analysis

### 1.1 Three independent gaps, one consequence

| Gap | Measured state (2026-11-12) | Consequence |
|:---|:---|:---|
| Frames | both `images/{train,val}` trees hold **0 files** | No training can start |
| Head | `head_status: reinitialised_untrained`; 36 head tensors re-initialised from COCO | Compiles, detects nothing; S12-T3 levels stay null |
| Latency | 13.7 ms p95 measured on Apple M4/arm64 | Not evidence for the x86-64 AVX2 worker |

The S11-T5 export is authentic (6,518,038 B, entropy 7.4324, 769 layers, dynamic 320/416, 42 channels), but authenticity is not training.

### 1.2 The staged label sets are not a dataset

| Path | Files | Unique frames | Split leakage | Images | Label provenance |
|:---|---:|---:|---|---:|---|
| `data/yolo_seg` | 301 | **289** | **12 stems appear in both train and val** | 0 | 289 genuine SAM proposals |
| `data/yolo_seg_v2` | 550 | 550 | 0 frame / 0 group overlap | 0 | 289 proposals + **261 template-generated** |
| `data/seg_prelabels` | 550 | 550 | — | — | 289 `proposal-only`, 261 `Curated segmentation annotation` |
| `data/proofs` | 744 JPEG | 744 | — | 744 | all captured **2026-11-06** |
| R2 archive | 31,935 JPEG | 31,935 | — | 31,935 | **2026-10-23 → 2026-11-12** |

### 1.3 The "301 labels paired with proof frames" premise is not materializable

1. **Exact timestamp join is zero.** Labels are `<camera>_sam_prelabel_<unix_ts>`; proofs are `<camera>/<unix_ts>_sample.jpg`. Joining all 301 labels to `data/proofs` yields **0/301**. Only 25 of 206 cameras appear, on a different date (label `…_1791467410` is 2026-10-08; nearest proof is `2026-11-06/…/1793945350_sample.jpg`). A camera-only join binds October polygons to November pixels and poisons every metric.
2. **The October bytes no longer exist.** R2 has no objects for `2026-10-08`, `-13` or `-15`, and only `labels/2026-10-16/` for that date. Retention dropped the frames; the labels are orphans. No local copy exists.
3. **The 301-file staging is contaminated.** 239 train + 62 val files resolve to 289 unique stems, so 12 frames sit in both splits.
4. **47 % of v2 is fabricated.** `curate_v2_dataset.py` fills the 261 skipped frames with fixed, hash-jittered template polygons (`0.35,0.80 … 0.42,0.92`) stamped `source: sam_prelabel`, and replaces real roadway polygons on 84 dual-carriageway frames. A gate on those labels measures agreement with a template.

**Decision**: the October label sets are retired as orphaned artifacts. #411 builds **Dataset v3** from the live proof archive. This is the only path that reaches 550+ frames with pixels and labels that actually correspond. It also satisfies the issue's AC "real camera frames materialized matching label polygons".

### 1.4 Gate provenance is currently fabricated by default

`CANONICAL_V2_METRICS` hardcodes `water_puddle 0.624 / mAP50-95 0.528`; `evaluate_retraining_gate(metrics=None)` copies them and labels the verdict `UNVERIFIED`. `seg_eval_v3.md` §5 confirms they were "not measured on this export". A gate that passes on a copied constant is worse than no gate; #411 makes measured input mandatory for a PASS.

---

## 2. Decisions (normative)

### 2.1 Dataset v3 source of truth

- **Frames**: R2 proof archive, `2026-10-23 … 2026-11-12` (31,935 JPEGs), plus harvester sidecars `labels/<date>/cam_<camera>_<ts>.json` for stratification (`weather`, `time_of_day`, `flood`).
- **Escape hatch**: a byte-complete October copy may be ingested instead, only by exact `snapshot_key` and only if every frame passes §2.4.
- **Retired**: `data/yolo_seg`, `data/yolo_seg_v2` and local `data/seg_prelabels` are audit inputs only; the materializer never trains from them.

### 2.2 Frame selection (≥550)

- **Strata** (measured from sidecars): ≥200 wet/rain, ≥150 dusk/night, ≥84 dual-carriageway, ≥28 cameras, ≥8 of the 21 dates. A shortfall fails with the missing count; never relabel a dry frame as rain.
- **Burst control**: ≤**3 frames per `(camera_id, date)`**, target ≥180 groups.
- **Rejects**: non-JPEG, decode failure, `< 320×240`, `> 4,000 px`, duplicate `sha256`, duplicate `(camera, timestamp)`.
- **Freeze the frame set** in `split.json` before annotation; reviewers cannot bias val by choosing frames.

### 2.3 Annotation and label trust

- SAM proposals come from `tools/seg/prelabel_sam.py` (runbook `docs/runbooks/sam_prelabeling.md`); proposals are **never** ground truth.
- **Every one of the ≥550 frames is human-reviewed** in `/labeler`; the 110 validation frames get a second independent review. Review means: correct every `water_puddle` boundary, remove phantom puddles, fix roadway/vehicle polygons, and save with a named human `annotator`.
- A label is **trusted** only if `annotator` is a named human (not `sam_prelabel`, not `auto_harvester`), a `reviewed_at` timestamp exists, and the polygon set does not match the template vocabulary in `curate_v2_dataset.py`. The materializer rejects anything else; there is no `--allow-unreviewed` flag.
- If the 261 template files are ever re-used, they must be re-annotated from their frames first. Their current geometry is deleted, not corrected.

### 2.4 Materializer contract — `tools/eval/materialize_seg_dataset.py` (new)

```bash
python3 tools/eval/materialize_seg_dataset.py \
  --dates 2026-10-23,2026-10-24,... --min-frames 550 \
  --out data/yolo_seg --val-split 0.2 --seed 42 \
  --manifest docs/reports/s12_t5_dataset_manifest.json [--dry-run] [--limit N]
```

1. Resolve each frame by exact `snapshot_key` from `$KV_PROOF_ROOT`, `data/proofs/proofs`, `data/proofs`, `data/harvested`, then R2 `<snapshot_key>` / `proofs/<snapshot_key>`. A miss is fatal and listed; no skipping.
2. Pair labels by `(camera_id, timestamp)` only. A camera-only match is an error, never a fallback.
3. Verify JPEG SOI `FF D8 FF`, decode with Pillow, record `sha256`, W×H and bytes. Reject zero-byte, truncated, CMYK and duplicate hashes.
4. Build in `<out>.tmp-<pid>`, verify, then atomically swap. A non-empty `--out` requires `--replace`, which archives to `<out>.bak-<timestamp>`. Never merge new labels into stale ones.
5. Write `images/`, `labels/`, `dataset.yaml`, `split.json`, manifest; assert `len(images) == len(labels)` and 0 orphans both ways.
6. Missing R2 env exits 2 naming the variable; no fallback literals (Rule 8).

### 2.5 Group split by camera and date (Lesson 75)

- Group key = `(camera_id, capture_date)` from label metadata, never the filename.
- 80/20 group split, `seed=42`, all frames of a group in exactly one split. Assert `train_groups ∩ val_groups = ∅`, `train_frames + val_frames = total`, and every one of the 6 classes has ≥1 val instance.
- `split.json` is frozen with `{seed, algorithm, groups, train:[stem], val:[stem], manifest_sha256}`. Re-running with a different seed against a frozen split is refused (`exit 2`); re-splitting requires deleting the freeze deliberately.
- The materializer also asserts no val stem (or its `sha256`) appears under `labels/train` or `images/train`.

### 2.6 Off-box training

- **Host**: CUDA GPU, ≥8 GB VRAM (T4 / L4 / RTX 3060 or better), ≥30 GB free disk, Linux x86-64. MPS/CPU training is not accepted as evidence.
- **Command**: `make train-seg DATA=data/yolo_seg/dataset.yaml EPOCHS=100`.
- **Protocol**: start `yolov8n-seg.pt`; re-head to 6 via the data YAML; `imgsz=416`, `batch=16`, `epochs=100`, `patience=25`, AdamW, `lr0=0.001`, `lrf=0.01`, `weight_decay=0.0005`, `warmup_epochs=3`, `seed=42`, `deterministic=True`, `workers=4`, `amp=True`, `flipud=0.0`, `degrees=0.0`, `close_mosaic=10`.
- **Artifacts** under `runs/segment/train_yolov8n_seg/`: `weights/{best,last}.pt`, `results.csv`, `args.yaml`, `val_metrics.json`, `run_manifest.json` binding `git_sha` (clean tree), dataset SHA, split SHA, weights SHA, GPU/driver, library versions, command, start/end. No cross-host resume that breaks the chain.

### 2.7 Measured metrics and the quantitative gate

`model.val(data=…, split='val', imgsz=416, conf=0.25, iou=0.5, plots=True)` produces the only admissible numbers:

- `mask_map50_95 = metrics.seg.map`, `mask_map50 = metrics.seg.map50`.
- Per-class **mask-pixel IoU** from the segmentation confusion matrix (`metrics.confusion_matrix.matrix`, row/col 0 = background): `IoU_c = M[c,c] / (Σ_r M[r,c] + Σ_k M[c,k] − M[c,c])` for `c = 1..6`.
- Class order is asserted against `["roadway","water_puddle","motorcycle","car","bus","truck"]`; a mismatch aborts.

Gate (`evaluate_retraining_gate`) must clear all five: `water_puddle` IoU **≥ 0.612**; mask mAP50-95 **≥ 0.496**; no class IoU drop **> 0.02** vs `V1_BASELINE_METRICS`; p95 **≤ 25.0 ms**; IR pair **≤ 10.0 MiB**. Additions:

- Measured input is mandatory: `--verify-gate` without `--metrics` exits 2 unless `--allow-untrained-head` is set, in which case the verdict stays `UNVERIFIED` and the process exits 1 for promotion.
- `metrics_provenance` must be the run artifact path, never "reference baseline".
- **Anti-cherry-pick**: report `best.pt` and `last.pt`; require the best epoch's mask mAP50-95 to exceed the median of the last 10 epochs by **≤ 0.02**. A single lucky epoch cannot carry the gate.
- The dataset/split SHA in `val_metrics.json` must equal the run manifest's; a mismatch fails.

### 2.8 OpenVINO FP16 export

- `make export-seg-openvino SEG_CHECKPOINT=runs/segment/train_yolov8n_seg/weights/best.pt EXTRA="--metrics runs/segment/train_yolov8n_seg/val_metrics.json"`.
- **No** `--allow-untrained-head`. The guard must observe `reinitialised_tensors: 0` and write `head_status: trained`.
- `dynamic=True`, `imgsz=416`, `quantize=16`; artifact identity: `.bin` **> 1 MB** (CI floor) and **≥ 6.0 MiB** (exporter floor), Shannon entropy **≥ 3.0** bits/byte, ≥ 32 unique bytes, ≥ 10 XML layers, `read_model` + `compile_model("CPU")`, execution at **320 and 416 px**, outputs `(1,42,anchors)` + `(1,32,104,104)`/`(1,32,80,80)`.
- `metadata.yaml` gains a `training` block: `dataset_manifest_sha256`, `split_sha256`, `best_pt_sha256`, `val_metrics_sha256`, `git_sha`, `gpu`, `epochs`, `gate_verdict`, `trained_at`.
- The IR pair stays gitignored; its identity travels as size + SHA-256 in evidence (Rule 6).

### 2.9 AVX2 worker latency benchmark

- **Worker**: the inference worker in `docs/runbooks/sam_prelabeling.md` §2 (AMD Ryzen 7 6800HS, AVX2, Linux x86-64). No arm64, emulation, or sub-normal CPU quota.
- **Host guard** in the new `tools/train/bench_seg_latency.py`: require Linux, `x86_64`, and `avx2` in `/proc/cpuinfo`; otherwise exit 2 with no evidence file.
- **Protocol**: `read_model` → `reshape([-1,3,-1,-1])` → `compile_model("CPU")` at default thread settings; 10 warm-ups; **200 timed runs × 3 repeats** at 416 px, then 320 px. Input is a real proof frame, letterboxed, SHA recorded.
- **Gate**: median-of-repeats p95 at 416 px **≤ 25.0 ms**; report p50/p95/p99 and the worst repeat. Record `lscpu`, kernel, governor, OpenVINO version, IR SHA, and `head_status` (must be `trained`).
- `make benchmark-real` also reports **wall** p50/p95/p99 (decode + letterbox). Graph p95 is the gate; wall p95 is context.

### 2.10 Promotion and S12-T3 hand-back

1. Verify the artifact SHA against the run manifest, write the pair to a temp directory on the model volume, atomically rename, restart/reload `cv-engine`.
2. `GET /healthz` reports `model_loaded: true`; `POST /analyze` on a real flood frame returns a non-empty `water_mask` and `water_ratio > 0`. Zero detections is a failed promotion, not "expected".
3. Roll back by renaming the XML to `.xml.bak`, restart, confirm detection fallback, restore; `--test-rollback` still passes.
4. Record the S12-T3 M1–M4 re-run in the handoff note; closing T3 is not part of #411.

### 2.11 Anti-fabrication invariants

1. No synthetic or template polygons; `--allow-synthetic` is forbidden for this dataset. The materializer imports nothing from `curate_v2_dataset.py`, and its template generators must be removed or hard-fail so the fabricated path cannot be re-run.
2. No image without an exact `(camera, timestamp)` key; no camera-only pairing; no duplicate stems across splits.
3. No gate PASS without a measured `val_metrics.json` bound to a run manifest.
4. No latency figure from a non-AVX2 host is gate-eligible.
5. `head_status` is written by the exporter from build stats, never hand-edited; `reinitialised_untrained` cannot be promoted.
6. No metric is copied from a previous report, and no evidence file carries a future timestamp (ADR-013 D1).

---

## 3. Scope / non-goals

**In scope**: Dataset v3 harvest, annotation and materialization; frozen group split; GPU fine-tune; measured validation; training-gate extension; OpenVINO FP16 export with `head_status: trained`; AVX2 benchmark harness and worker run; live promotion; evidence and docs.

**Out of scope**: new segmentation classes or taxonomy changes; curb segmentation; hazard classification logic (S12-T3); Go/cv-engine contract changes beyond consuming a trained mask; web UI changes; model compression beyond FP16; cloud training infrastructure provisioning; re-annotation tooling beyond `/labeler` and `prelabel_sam.py`.

---

## 4. Acceptance criteria

### US-411.1 — A dataset whose pixels and labels correspond
- **AC1** ≥550 frames are materialized with `len(images) == len(labels)` per split and 0 orphans in either direction.
- **AC2** Every image resolves by exact `snapshot_key`; the manifest records key, SHA-256, width, height and source (`local|r2`) for all ≥550.
- **AC3** Re-running the pairing against the retired October set produces **0/301** exact matches and a non-zero exit; no camera-only fallback exists.
- **AC4** The 12 cross-split duplicate stems of the staged set are absent; a duplicate stem or duplicate image hash fails the build.
- **AC5** Every label is human-reviewed: `annotator` is a named human, `reviewed_at` is present, and no template-generated polygon survives.
- **AC6** Strata are met (≥200 rain/wet, ≥150 dusk/night, ≥84 dual-carriageway) with counts in the manifest, or the build fails naming the shortfall.

### US-411.2 — A split that cannot leak
- **AC7** Group key is `(camera_id, capture_date)`; `train_groups ∩ val_groups = ∅`; the manifest reports both counts.
- **AC8** All 6 classes have ≥1 val instance; the split is frozen with a SHA, and a different `--seed` against the freeze exits 2.
- **AC9** No val stem or image hash appears in train; the materializer asserts this, and a seeded violation fails.

### US-411.3 — Real fine-tuning
- **AC10** `best.pt` comes from a CUDA GPU run with a run manifest binding git SHA, dataset SHA, split SHA, weights SHA, GPU, versions and ≥50 completed epochs.
- **AC11** Measured `water_puddle` mask IoU **≥ 0.612** and mask mAP50-95 **≥ 0.496** on the frozen val split, with no class drop > 0.02 vs v1.
- **AC12** The gate prints `PASSED` with `metrics_provenance` pointing at `val_metrics.json`; no path yields `PASSED` from hardcoded metrics.
- **AC13** Best-epoch stability holds (margin ≤ 0.02 over the last-10-epoch median), and `last.pt` metrics are reported beside `best.pt`.

### US-411.4 — A trained, authentic export
- **AC14** `metadata.yaml` reports `head_status: trained`, `reinitialised_tensors: 0`, and a `training` block whose SHAs match the run artifacts.
- **AC15** `.bin` > 1 MB (≥ 6.0 MiB observed), entropy ≥ 3.0, compile + execute at 320 and 416 px, 42-channel contract intact.
- **AC16** Export without `--allow-untrained-head` succeeds; with an untrained checkpoint it still aborts and leaves the deployed artifact untouched.

### US-411.5 — AVX2 evidence
- **AC17** The benchmark refuses Darwin/arm64 and any host without `avx2`, exiting 2 without an evidence file.
- **AC18** On the worker, 416 px p95 **≤ 25.0 ms** across 3 × 200 timed runs; p50/p95/p99 and host fingerprint are recorded.
- **AC19** `make benchmark-real` reports wall p50/p95/p99 on the same worker; the graph p95 remains the gate number.

### US-411.6 — Live and durable
- **AC20** The promoted IR serves a real frame with non-empty `water_mask` and `water_ratio > 0`; rollback to detection and restoration both pass.
- **AC21** `python3 tools/ci/check_evidence_integrity.py --mode gate` and `python3 tools/docs/docs_lint.py` pass with 0 new violations.
- **AC22** The PR starts `Close #411`, targets `dev`, and no model weight is committed.

---

## 5. Contracts

### 5.1 `docs/reports/s12_t5_dataset_manifest.json`

```json
{"version":"3.0","generated_at":"2026-11-xxT..+07:00","source":"r2_live_archive","total_frames":550,
 "train":440,"val":110,"seed":42,"split_sha256":"…","groups":{"train":0,"val":0,"overlap":0},
 "strata":{"rain_wet":0,"dusk_night":0,"dual_carriageway":0},
 "labels":[{"stem":"<cam>_<ts>","key":"2026-11-08/<cam>/<ts>_sample.jpg","sha256":"…","width":1920,
 "height":1080,"split":"train","group":"<cam>|2026-11-08","annotator":"<human>","reviewed_at":"…","source":"r2"}]}
```

### 5.2 `val_metrics.json`

```json
{"provenance":"ultralytics_val","split":"val","conf":0.25,"iou":0.5,"imgsz":416,
 "weights_sha256":"…","dataset_manifest_sha256":"…","split_sha256":"…","generated_at":"…",
 "epochs_completed":100,"best_epoch":87,"mask_map50":0.0,"mask_map50_95":0.0,
 "class_iou":{"roadway":0.0,"water_puddle":0.0,"motorcycle":0.0,"car":0.0,"bus":0.0,"truck":0.0},
 "stability":{"last10_median_map50_95":0.0,"best_minus_median":0.0}}
```

### 5.3 CLI / Make changes

- `train_yolov8_seg.py`: `--metrics PATH`, `--seed`, `--dataset-manifest PATH`; `--verify-gate` requires `--metrics` (or `--allow-untrained-head` for bootstrap).
- `Makefile`: `train-seg` passes `SEED`; `benchmark-real` writes the latency block; new `bench-seg-avx2` target.
- `benchmark.py`: add `latency: {p50, p95, p99, n}` to `BenchmarkResult` and the markdown report.

---

## 6. Test plan

### 6.1 Automated

- **`materialize_seg_dataset_test.py`** (new): exact-key hit/miss; camera-only rejection; `proofs/` prefix; local precedence; duplicate stem/hash; non-JPEG/truncated/small; missing frame fatal; atomic replace + backup; frozen-split refusal; class-presence and group-overlap failures; 301-set negative control; manifest schema/SHA.
- **`train_yolov8_seg_test.py`** (extend): measured PASS; each FAIL boundary (0.611, 0.495, 0.021 drop, 25.01 ms, 10.01 MiB); `--verify-gate` without metrics exits 2; bootstrap stays `UNVERIFIED`; stability margin; `head_status: trained` only at 0 re-initialised; metadata block.
- **`bench_seg_latency_test.py`** (new): Darwin/arm64 and no-avx2 refusal; percentile math; 25.0 vs 25.01; no evidence on refusal; non-future timestamps; untrained metadata refusal.
- **`benchmark_test.py`** (extend): latency summary, empty-set behaviour, report rendering.
- **Regression**: `make test-seg test-train test-eval`, `make evidence-lint`, `make docs-lint`. Fixtures stay inside unit tests; no fixture may appear in release evidence (ADR-013 D1).

### 6.2 Integration (no GPU required)

- Materializer `--dry-run` on a 10-frame fixture tree: 0 writes outside the temp dir.
- Export guard: an 80-class checkpoint exits non-zero, writes no IR, and leaves the existing artifact byte-identical.
- Gate end-to-end with a synthetic metrics file in `tmp/`: PASSED/FAILED paths and provenance string.
- Evidence checker on the committed evidence JSON (model identity by SHA, no weights committed).

---

## 7. Manual verification scenarios (Rule 10)

| ID | Scenario | Evidence required |
|:--|:---|:---|
| **M1** | Dataset audit. (a) Retired-set pairing as a negative control: 0/301, exit non-zero, no files written. (b) Materialize v3: ≥550/550 resolved, 0 orphans, 0 duplicate hashes. (c) Inspect 3 random `(stem, key, sha256, W×H)` tuples and confirm the overlay lands on the same pixels. | Terminal capture, 3 pair screenshots, manifest |
| **M2** | Split audit: `split.json` SHA, group counts, empty intersection, per-class val counts; add a val frame to train on a copy and confirm refusal. | Assertion output + refusal log |
| **M3** | GPU fine-tune: `nvidia-smi`, live epoch log, `results.csv` tail, `best.pt` SHA, measured `val` table, gate `PASSED` with measured provenance and stability margin. | Run log, `val_metrics.json`, gate summary |
| **M4** | Export without `--allow-untrained-head`: `head_status: trained`, 0 re-initialised tensors, `.bin` size/entropy/SHA, 320/416 inference, 42 channels, `metadata.yaml` block. | Export log + metadata excerpt |
| **M5** | AVX2 benchmark: `lscpu` shows `avx2`; 3 × 200 runs at 416 and 320; p50/p95/p99; ≤ 25.0 ms. On the Mac, exit 2 with no evidence file. | `s12_t5_latency_avx2.json`, captures |
| **M6** | Live promotion: atomic deploy, restart, `/healthz`, real flood frame with non-empty mask and `water_ratio > 0`; S12-T3 M1–M4 now return non-null hazard fields; rollback and restore pass. | API JSON, screenshots, rollback log |

All artifacts must pass `check_evidence_integrity.py` (`fit_source == live_history` where applicable, real non-future timestamps, genuine weights, valid image magic bytes). Synthetic fixtures, mock models and simulated telemetry are forbidden as release evidence.

---

## 8. Definition of Done

- [x] Materializer, split freeze and manifest schema verified; the ≥550-frame human-reviewed build runs off-box.
- [x] Measured gate tooling verified; PASSED still requires the off-box GPU run's `val_metrics.json`.
- [ ] `best.pt` from a CUDA GPU with a complete run manifest and stability check; executes off-box.
- [ ] FP16 IR with `head_status: trained`, > 1 MB (≥ 6.0 MiB), entropy ≥ 3.0, dynamic 320/416, 42 channels.
- [x] AVX2 benchmark harness verified; the 3 × 200 worker soak runs on the off-box AVX2 host.
- [ ] Live cv-engine serves a non-empty water mask; rollback verified; T3 hand-back recorded.
- [x] Materializer, contract suites, measured gate and AVX2 harness verified for Close #411; M1–M6, lint gates and the closing PR run at close.

---

## 9. Documentation impact (Rule 6)

- **`docs/reports/seg_eval_v4.md`** (new): measured v3 dataset, training curve, gate table, AVX2 p50/p95/p99, promotion. `seg_eval_v3.md` stays as the bootstrap audit.
- **Evidence**: `s12_t5_dataset_manifest.json`, `s12_t5_train_run.json`, `s12_t5_latency_avx2.json`; weights stay gitignored.
- **`docs/runbooks/sam_prelabeling.md`**: prelabel → review → materialize → train handoff, including retirement of the October sets.
- **`docs/reports/sprint12_report.md`** at close: S12-T5 summary and the T3 unblock.
- **`README.md`**: dataset v3 and benchmark workflow; mirror changed commands/ports in `README.vi.md` (`check_readme_vi_parity.py` must pass).
- **`docs/lessons/<issue>-dataset-v3-provenance-and-avx2-evidence.md`**: *"Labels without their frames are not a dataset; a gate measured against template polygons measures the template."* Index row only at sprint close.

---

## 10. Risks

| Risk | Mitigation |
|:---|:---|
| October frames are gone | Build v3 from the live archive; the escape hatch requires byte-exact keys |
| 550-frame review overruns | Freeze the frame set early, review date by date, escalate by day 3 rather than silently shrink |
| SAM proposals wrong on wet asphalt | Human review mandatory, water boundaries corrected first, val double-reviewed |
| Val is selection and report split | Report `best.pt` and `last.pt`, require the ≤ 0.02 stability margin, disclose optimism in v4 |
| Gate misses 0.612 | Improve annotation and augmentation; never lower the threshold or copy v2 numbers |
| GPU preempted | Checkpoint to persistent storage; resume from `last.pt` with the same dataset SHA |
| AVX2 host noisy | 3 repeats, governor/`lscpu` recorded, median-of-repeats p95, worst repeat disclosed |
| Model exceeds the 2 GB cgroup | FP16 ceiling 10 MiB plus a live RSS check in M6; rollback is one rename away |
| Label contract drift | Trust rule enforced in the materializer and unit-tested; no bypass flag |
