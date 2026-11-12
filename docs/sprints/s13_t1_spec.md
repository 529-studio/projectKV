# S13-T1 Functional Specification — Moore Tracer Repair, Off-box GPU Evidence and AVX2 Soak

**Issue**: [#438](https://github.com/529-studio/projectKV/issues/438) · **Sprint**: 13 · **Branch**: `feat/s13-t1-gpu-finetune-and-avx2-soak`
**Owner**: QA Lead · **Consumers**: Lead Developer, Tester, Infra
**Depends on**: `tools/seg/image_geometry.py`, `tools/train/train_yolov8_seg.py`, `tools/train/bench_seg_latency.py`, `tools/eval/materialize_seg_dataset.py`, Dataset v3 (§2.4 of [`s12_t5_spec.md`](s12_t5_spec.md))
**Charter**: ADR-009 §4, ADR-012 §D6, ADR-013 D1, Lesson 75, [`docs/lessons/438-moore-boundary-tracing-jacobs-criterion.md`](../lessons/438-moore-boundary-tracing-jacobs-criterion.md), `docs/HANDOFF-s12.md` §6.4

**Status (2026-11-12)**: the Moore-tracer repair, the Colab GPU driver and the containerised AVX2 harness are implemented and verified on the development host. The off-box GPU fine-tune, the worker soak and live promotion are **not** done: `data/yolo_seg/images` holds 0 frames, no run directory exists, and the deployed IR still reports `head_status: reinitialised_untrained` with 36 re-initialised tensors. No AC below is claimed as met unless its evidence exists in the tree.

---

## 0. Estimation (Rule 3)

**Total: 44–62 h active over 6–8 days (2026-11-13 → 2026-11-20), the off-box GPU slot on the critical path.**

| Work package | Active effort |
|:---|:---|
| Moore tracer repair, 8-connected component merge, ragged-shape regression suite | 6–8 h |
| Colab GPU driver, notebook, contract tests, failure paths | 10–13 h |
| Docker AVX2 image, runner, host probe, evidence checker, tests | 9–12 h |
| Off-box GPU fine-tune and measured validation (wall time excluded) | 5–7 h |
| AVX2 worker soak and evidence validation | 4–5 h |
| Promotion, rollback and live verification | 3–4 h |
| Docs, README parity, evidence indexing, PR | 7–9 h |

Subtotal 44–58 h; the 62 h ceiling reserves 4 h for Colab quota resets and a repeated worker run after a refused or noisy host. **Blocked if**: Dataset v3 pixels are not materialized, a CUDA GPU with ≥ 8 GB VRAM is unavailable, or no native x86-64 AVX2 host can be reached by day 4. A blocked task reports the blocker and never substitutes a smaller dataset, a CPU run or an emulated container.

**Unblocks**: AC10–AC20 of #438, `docs/reports/seg_eval_v4.md`, flood-hazard promotion, Sprint 13 evidence close.

---

## 1. Problem Analysis

### 1.1 Moore tracing terminated on a pixel instead of a state

The geometry stage is a pure-NumPy chain: SAM proposals are unioned into one mask, `largest_component` keeps the region under test, and `mask_to_polygon` walks the outer boundary clockwise before RDP simplification. The original stop test was `cur == start and len(path) > 2`. Moore tracing legitimately re-enters the start pixel from a different approach two steps after leaving it, which is exactly what a one-pixel protrusion tip produces: the topmost-leftmost pixel of a single-pixel antenna has one foreground neighbour, so the contour wraps back almost immediately. The trace then "closed" with a three-vertex path covering only the tip.

Three coupled defects made that failure silent and destructive:

| # | Defect | Effect |
|:--|:---|:---|
| 1 | Stop test compared a pixel, not the `(pixel, backtrack)` state | A protrusion tip closed the trace after 3 vertices |
| 2 | `mask_to_polygon` returns `[]` below three points, and the caller drops candidates below `MIN_POLYGON_POINTS` | A collapsed trace became "frame with no label", indistinguishable from an empty SAM result |
| 3 | `largest_component` merged row runs with a plain `run_nodes[y-1, s:e]` slice (4-connectivity) | A diagonally attached antenna was split off and discarded before tracing began |
| 4 | Every geometry test used rectangles and squares | On a rectangle the start is a corner with two orthogonal neighbours, so the broken condition was never exercised |

**Measured consequence**: the same live frame set produced **370 polygons over 744 candidate frames**, and the missing ~40 % was first misread as weak SAM masks rather than a defect in the code that describes them. Fixing the trace alone would not have restored the diagonally attached shapes; the connectivity window had to be corrected in the same change.

### 1.2 The training evidence requires an off-box CUDA host

The development host is Apple Silicon (arm64) with no CUDA device. Training on MPS or CPU is explicitly not evidence (`s12_t5_spec.md` §2.6), so a GPU run must happen on a separate host with:

- Linux x86-64, CUDA GPU with **≥ 8 GB VRAM** (T4 / L4 / RTX 3060 or better), **≥ 30 GB free disk**;
- a clean git tree, so the run manifest can bind an unambiguous commit SHA;
- enough uninterrupted wall time for `epochs=100`, `patience=25`, `imgsz=416`, `batch=16`.

The blocker is not only hardware. `data/yolo_seg/images` currently holds **0 frames**, so the GPU driver has nothing to train on until Dataset v3 is materialized from the live proof archive (S12-T5 §2.4). The Colab runtime is also ephemeral and quota-bound: a run that dies at epoch 60 must resume from `last.pt` against the same dataset and split SHA, or it is not the same experiment. The driver therefore has to fail closed on host, dataset and provenance before it spends GPU minutes.

### 1.3 The latency gate is only meaningful on the x86-64 AVX2 worker

The local M4 measurement of **13.70 ms p95** is context, never evidence: the gate exists to prove the model meets **p95 ≤ 25.0 ms at 416 px** on the machine that serves inference. Two failure modes must be refused before any number is written:

1. **Wrong architecture** — a Darwin/arm64 host, or a `linux/amd64` container running under Docker Desktop emulation on Apple Silicon. The latter is QEMU/TCG, not the worker.
2. **Fake capability** — a QEMU/TCG CPU model can advertise `avx2` in `/proc/cpuinfo` while being an order of magnitude slower. A flag check alone is insufficient; the CPU model must also be screened.

A third, quieter failure is time: a drifting VM clock can produce evidence with a future timestamp, which ADR-013 D1 forbids and which `check_evidence_integrity.py` rejects. The harness therefore checks the container clock against the host clock before measuring, and the post-run checker re-derives every timestamp from the JSON itself.

---

## 2. Technical Architecture

### 2.1 Jacob's stopping criterion in `trace_boundary`

Termination is a property of **state**, not position. The state is `(pixel, backtrack)`, where `backtrack` is the neighbour the trace arrived from.

1. **Start state**: the topmost-leftmost foreground pixel paired with its **west** neighbour, which is background because the row-major scan reaches the pixel from there. That pair is the closed contour's true entry state.
2. **Clockwise scan from the backtrack cell**: neighbour order begins at `_DIR_INDEX[backtrack - cur] + 1` and skips the previous pixel, so the trace cannot U-turn into itself.
3. **Moore bookkeeping**: the cell scanned immediately before the chosen neighbour becomes the next backtrack cell. This is what makes the state a function of contour geometry rather than loop position.
4. **Stop on a repeated state**: a `seen` set records every `(pixel, backtrack)` pair; the loop ends on the first repeat, after the full outer boundary has been walked. The safety bound `grid.size * 9` is a guard, never the normal exit.
5. **8-connected component merge**: `largest_component` reads `run_nodes[y-1, max(0, s-1):e+1]`, so diagonal-only contact joins the component and the antenna reaches the tracer instead of being pruned.
6. **Measure yield, not just the trace**: the same 744-frame set now yields **6,386 polygons** (8.58 per frame) instead of 370, and the six new tests pin a two-pixel protrusion tip, a diagonally clipped staircase corner, a one-pixel antenna, a diagonally attached antenna at component level, a closed 8-connected trace, and two slabs joined at one corner. Each fails against the old `cur == start` condition.

### 2.2 Colab GPU runner and `colab_pipeline.py`

`train_yolov8_seg.py` stops at weights plus IR: it cannot invent a measured `val_metrics.json`, and its training branch exports before any metric exists. [`tools/train/colab_pipeline.py`](../../tools/train/colab_pipeline.py) supplies the missing orchestration around the same shipped functions so one off-box run produces the complete AC10–AC16 evidence set:

| Stage | Contract |
|:---|:---|
| 1. Pre-flight | Linux x86-64, CUDA GPU ≥ 8 GB VRAM, ≥ 30 GB free disk, clean git tree, resolved SHA, library versions |
| 2. Dataset | Frozen `split.json`, `manifest.json`, six canonical class names, zero orphans, every class present in `val` |
| 3. Train | Shipped CLI with fixed §2.6 hyperparameters: `imgsz=416`, `batch=16`, `epochs=100`, `patience=25`, AdamW, `lr0=0.001`, `seed=42`, `workers=4`, `amp=True` |
| 4. Validate | `model.val()` on the frozen split for **both** `best.pt` and `last.pt`; mask mAP50/mAP50-95 and per-class **mask-pixel IoU** from the validator's mask batches, never the box confusion matrix |
| 5. Admit metrics | `load_val_metrics`: ≥ 50 epochs, frozen split SHA, canonical class order, anti-cherry-pick stability margin |
| 6. Gate | `evaluate_retraining_gate` with measured IR size and p95, or an authoritative AVX2 evidence file bound by IR SHA |
| 7. Export | `export_to_openvino_fp16` with `--metrics` provenance, then assert `head_status: trained`, `reinitialised_tensors: 0`, training-block SHAs matching the run |
| 8. Bundle | Deterministic zip: weights, metrics, manifests, IR, reports, hashes |

```bash
python3 tools/train/colab_pipeline.py \
  --data-dir /content/datasets/yolo_seg --work-dir /content/kv_s13 \
  --export-dir models/yolov8n_seg_openvino_416 \
  --epochs 100 --seed 42 --device 0 \
  [--latency-evidence docs/reports/s13_t1_latency_avx2.json]
```

Exit codes: `0` evidence complete, `1` pipeline or gate failure, `2` usage or host refusal. `--allow-short-run` and `--allow-dirty` are diagnostic only and leave promotion blocked. [`colab_train_yolov8_seg.ipynb`](../../tools/train/colab_train_yolov8_seg.ipynb) is the 18-cell wrapper: install, clone and pin the branch, configure, GPU/disk pre-flight, acquire and verify Dataset v3, run the pipeline, inspect evidence, download the bundle, and print the remaining worker step.

**Latency binding is strict**: `resolve_latency` rejects evidence whose `head_status` is not `trained`, and rejects evidence whose `ir_sha256` differs from the freshly exported `.bin`. Without `--latency-evidence`, the pipeline labels its number *provisional* and sets `authoritative: false`; that label cannot satisfy AC18.

### 2.3 Docker AVX2 benchmark harness

[`deploy/benchmark/Dockerfile.avx2`](../../deploy/benchmark/Dockerfile.avx2) pins the measurement surface: `python:3.12.7-slim-bookworm`, OpenVINO `2026.4.1`, NumPy `2.1.3`, Pillow `11.0.0`, PyYAML `6.0.2`, `libgomp1`, `util-linux` for `lscpu`. It copies only `bench_seg_latency.py`, `bench_avx2_host_probe.py` and `bench_avx2_evidence_check.py`, runs as uid `10001`, and binds no data, model or secret into the image.

[`tools/train/bench_avx2_docker.sh`](../../tools/train/bench_avx2_docker.sh) enforces the four-layer refusal before a number exists:

| Layer | Check | Failure mode |
|:---|:---|:---|
| Runtime | Container runtime reachable and reporting an x86-64 architecture | exit 2, before any build |
| Host probe | Inside the image: Linux, `x86_64`, `avx2` in `/proc/cpuinfo`, and a non-QEMU/TCG CPU model | exit 2, no evidence |
| Clock | Container clock within **300 s** of the host clock | exit 2, no evidence |
| Protocol | Fixed at **10 warm-ups, 200 timed runs × 3 repeats at 416 px** | no flag shortens it |

After the run, [`bench_avx2_evidence_check.py`](../../tools/train/bench_avx2_evidence_check.py) re-derives the 600 measurements, the trained head, the 25.0 ms verdict and every `*_at` timestamp from the evidence JSON. Exit codes: `0` gate passed, `1` gate failed (evidence still written and validated), `2` host refused, `3` evidence rejected.

```bash
make bench-seg-avx2-check                       # prove the runtime is gate-eligible
make bench-seg-avx2-docker SEG_IR=... SEG_FRAME=... \
     SOAK_OUT=docs/reports/s13_t1_latency_avx2.json
```

The repository mount is read-only; the evidence directory is the only writable host path. `SOAK_OUT` defaults to `docs/reports/s13_t1_latency_avx2.json` in the [`Makefile`](../../Makefile).

### 2.4 End-to-end chain and invariants

```
live proof frames ──► geometry (Jacob trace + 8-connected merge) ──► Dataset v3
        ──► colab_pipeline.py (CUDA host) ──► best.pt + val_metrics.json
        ──► FP16 IR + metadata(trained) ──► AVX2 soak (container, 600 runs)
        ──► promotion ──► /analyze returns a non-empty water mask
```

Invariants that no flag may bypass: no synthetic or template polygons; no CPU/MPS training counted as evidence; no gate `PASSED` from copied constants; no latency from a non-AVX2 host; no evidence with a future timestamp; no dirty-tree SHA attributed to a clean run; no IR accepted whose SHA differs from the one that was measured.

---

## 3. Acceptance Criteria Matrix (AC10–AC20)

Status legend: **DONE** verified with evidence in-tree · **PARTIAL** tooling verified, host artifact missing · **BLOCKED** depends on an unavailable host or dataset.

| AC | Requirement | Gate (command / threshold) | Status 2026-11-12 | Blocker |
|:--|:---|:---|:---|:---|
| **AC10** | `best.pt` from a CUDA run with a manifest binding git SHA, dataset SHA, split SHA, weights SHA, GPU, versions and ≥ 50 completed epochs | `colab_pipeline.py` pre-flight + `run_manifest.json`; `MIN_EPOCHS = 50` | **BLOCKED** | No CUDA host; `data/yolo_seg/images` = 0 frames; no run directory |
| **AC11** | Measured `water_puddle` mask IoU **≥ 0.612**, mask mAP50-95 **≥ 0.496**, no class drop > 0.02 vs v1 | `model.val()` on frozen split → `val_metrics.json` | **BLOCKED** | Depends on AC10 |
| **AC12** | Gate prints `PASSED` with `metrics_provenance` pointing at `val_metrics.json`; no path yields `PASSED` from hardcoded metrics | `evaluate_retraining_gate(metrics=…)`; provenance tokens `reference baseline` / `CANONICAL_V2_METRICS` rejected | **PARTIAL** | Code path and refusal tests exist; measured input missing |
| **AC13** | Best-epoch stability margin ≤ 0.02 over the last-10-epoch median; `last.pt` reported beside `best.pt` | `compute_epoch_stability` + dual validation | **BLOCKED** | Depends on AC10 |
| **AC14** | `metadata.yaml` reports `head_status: trained`, `reinitialised_tensors: 0`, training block SHAs match the run | `validate_metadata_payload` after export | **FAILS TODAY** | Deployed IR is `reinitialised_untrained`, 36 tensors |
| **AC15** | `.bin` > 1 MB (≥ 6.0 MiB observed), entropy ≥ 3.0, compile + execute at 320 and 416 px, 42-channel contract intact | exporter identity gate + `verify_openvino_ir` | **PARTIAL** | Current `.bin` is 6,518,038 B, entropy 7.4324, 42 channels — but head untrained, so not AC15-eligible |
| **AC16** | Export without `--allow-untrained-head` succeeds; with an untrained checkpoint it aborts and leaves the deployed artifact untouched | `make export-seg-openvino SEG_CHECKPOINT=…` | **PARTIAL** | Abort path verified; success path needs a trained checkpoint |
| **AC17** | Benchmark refuses Darwin/arm64 and any host without `avx2`, exit 2, no evidence file | 38 contract tests + `bench-seg-avx2-check` on macOS → exit 2 | **DONE** | — |
| **AC18** | Worker 416 px p95 ≤ 25.0 ms across 3 × 200 runs; p50/p95/p99 and host fingerprint recorded | `bench_avx2_docker.sh` → `bench_avx2_evidence_check.py` | **PARTIAL** | Harness verified; worker soak not run |
| **AC19** | `make benchmark-real` reports wall p50/p95/p99 on the same worker; graph p95 stays the gate number | `bench_seg_latency.py --wall` block | **BLOCKED** | Needs the same worker run as AC18 |
| **AC20** | Promoted IR serves a real frame with non-empty `water_mask` and `water_ratio > 0`; rollback and restore both pass | `POST /analyze`, `GET /healthz`, `--test-rollback` | **BLOCKED** | Depends on AC14/AC18 |

**Cluster notes.**

- **AC10–AC13 (GPU evidence)**: one Colab session produces all four. The driver admits nothing below 50 epochs and refuses a dirty tree, so a shortcut run cannot masquerade as the experiment. A resumed run must present the same dataset and split SHA; otherwise it is a new experiment and starts over.
- **AC14–AC16 (artifact identity)**: the current IR is authentic but untrained. Size, entropy, 42 channels and dynamic 320/416 are already satisfied by the S12-T5 export; the missing condition is `head_status: trained`, which only a GPU run can write. Re-export must happen after AC10/AC11, and the export guard must be exercised twice: success with a trained checkpoint, abort with the old one, artifact byte-identical after abort.
- **AC17–AC19 (AVX2 evidence)**: AC17 is the only fully closed criterion, because refusal is measurable on the development host. AC18/AC19 require the native x86-64 AVX2 worker; a Docker Desktop emulated run is not a substitute and the runner refuses it by design.
- **AC20 (live)**: promotion is the last step and includes rollback. A model that loads but returns an empty mask is a failed promotion, not an expected cold start.

**Closing gates for the PR (not part of the AC10–AC20 matrix)**: `make docs-lint` 0 new / 0 stale, `make evidence-lint` PASS, PR description starts `Close #438`, targets `dev`, and no model weight is committed.

---

## 4. Quantitative Verification and Test Ledger

### 4.1 Executed suites (2026-11-12, development host)

| Suite | Tests | Command | Result |
|:---|---:|:---|:---|
| Geometry, including 6 ragged-shape regressions | 23 | `python3 -m unittest tools.seg.image_geometry_test` | **OK** (0.153 s) |
| Colab driver contract | 36 | `python3 -m unittest tools.train.colab_pipeline_test` | **OK** (0.081 s) |
| Container harness contract | 38 | `python3 -m unittest tools.train.bench_avx2_docker_test` | **OK** (1.943 s) |
| Full training toolchain | 137 | `make test-train` | **OK**, 3 skipped (2.186 s) |
| Docs budget and structure | — | `make docs-lint` | **PASS**: 20 violations, 0 new, 0 stale |
| Evidence integrity | — | `make evidence-lint` | **PASS**: 77 artifacts evaluated |
| Host refusal, real run | — | `tools/train/bench_avx2_docker.sh --check` on macOS | exit **2**, `Darwin arm64`, no evidence written |

The three skips are CUDA/OpenVINO-dependent cases that cannot execute on the arm64 host; they are not failures and must run on the off-box hosts before AC10/AC18 are claimed.

### 4.2 Gate thresholds and current measurements

| Quantity | Threshold | Measured / expected | Source |
|:---|:---|:---|:---|
| Polygon yield | ≥ 1 polygon per candidate frame | 6,386 / 744 (8.58 per frame) | Lesson 438, live frames |
| Pre-fix yield (negative control) | — | 370 / 744 | Lesson 438 |
| `water_puddle` mask IoU | ≥ 0.612 | not measured | AC11 |
| Mask mAP50-95 | ≥ 0.496 | not measured | AC11 |
| Per-class IoU drop | ≤ 0.02 vs v1 | not measured | AC11 |
| Completed epochs | ≥ 50 (target 100) | 0 | AC10 |
| Stability margin | ≤ 0.02 | not measured | AC13 |
| IR `.bin` size | > 1 MB, ≥ 6.0 MiB observed | 6,518,038 B | deployed IR |
| IR entropy | ≥ 3.0 bits/byte | 7.4324 | deployed IR |
| IR channels | 42 | 42 | deployed IR |
| Latency p95, 416 px | ≤ 25.0 ms on AVX2 worker | 13.70 ms on M4 (not eligible) | deploy README |
| Soak protocol | 10 warm-ups, 3 × 200, 600 measurements | harness fixed | evidence checker |
| Training host VRAM | ≥ 8 GB | not probed | AC10 |
| Training free disk | ≥ 30 GB | not probed | AC10 |
| Container clock skew | ≤ 300 s | checked before run | runner |
| Head status | `trained`, 0 re-initialised | `reinitialised_untrained`, 36 | deployed IR |

### 4.3 Evidence artifact ledger

| Artifact | Producer | Present |
|:---|:---|:---|
| `run_manifest.json` (git/dataset/split/weights SHA, GPU, versions) | `colab_pipeline.py` | No — blocked |
| `val_metrics.json` (best and last, per-class mask IoU, stability) | `colab_pipeline.py` | No — blocked |
| FP16 IR + `metadata.yaml` (`head_status: trained`, training block) | `export_to_openvino_fp16` | Only the untrained S12-T5 pair |
| `docs/reports/s13_t1_latency_avx2.json` | `bench_avx2_docker.sh` | No — worker not run |
| Deterministic zip bundle with checksums | `colab_pipeline.py` stage 8 | No — blocked |
| Promotion and rollback capture | `cv-engine` + `/analyze` | No — blocked |

Planned soak evidence shape: `generated_at`, `protocol{warmup_runs:10, timed_runs:200, repeats:3, gate_resolution:416}`, `head_status:"trained"`, `ir_sha256`, `host{avx2:true, …}`, `latency{p50_ms,p95_ms,p99_ms,worst_repeat_p95_ms}`, `repeats[3][200]`, `gate{metric:"p95_ms", threshold_ms:25.0, aggregation:"median_of_repeats_p95", verdict}`. The checker rejects a document with 599 measurements, a missing worst-repeat p95, an untrained head, or a verdict that disagrees with the numbers.

### 4.4 Re-run commands before closing #438

```bash
make test-train test-seg docs-lint evidence-lint
make bench-seg-avx2-check                       # must pass on the worker, exit 2 elsewhere
make bench-seg-avx2-docker SOAK_OUT=docs/reports/s13_t1_latency_avx2.json
python3 tools/ci/check_evidence_integrity.py --verify-all
```

### 4.5 Non-substitutes

A passing unit suite is not evidence. The following never satisfy an AC: the M4 13.70 ms p95, a Docker Desktop emulated `linux/amd64` run, a CPU/MPS fine-tune, a gate driven by `CANONICAL_V2_METRICS`, a soak document with synthetic repeats, a bundle produced with `--allow-short-run` or `--allow-dirty`, and any artifact whose `generated_at` precedes its `window.end` or lies in the future.
