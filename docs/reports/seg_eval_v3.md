# seg_eval_v3.md — Real YOLOv8n-seg Export & OpenVINO FP16 Deployment Audit

**Task**: S11-T5 / Issue #399 — *Real GPU YOLOv8n-seg Training & 6.4 MB OpenVINO Deployment*
**Date**: 2026-11-10
**Branch**: `feat/s11-t5-gpu-yolov8n-seg-openvino`
**Addresses**: ADR-013 §1.1 **F4** (64 KB dummy OpenVINO stub), ADR-009 §4 (zero-VLM segmentation)
**Machine evidence**: [`seg_export_evidence.json`](seg_export_evidence.json), [`seg_cv_engine_e2e.json`](seg_cv_engine_e2e.json)

> **Status: PARTIAL — artifact authentic, empirical fine-tuned IoU still blocked.**
> The committed placeholder is gone: `yolov8n_seg.bin` is now 6,518,038 bytes of real FP16
> weight pages that compile and serve. However the 6-class head could **not** be trained on
> this host: `data/yolo_seg/images/{train,val}` contain **0 image files** (301 label files,
> 0 frames), so the exported head is `reinitialised_untrained`. Every file that says so is
> recorded in `models/yolov8n_seg_openvino_416/metadata.yaml` and enforced by a fail-closed
> export guard. **These weights must not be promoted to production** until fine-tuning runs
> on a GPU box with the real dataset.

---

## 1. What was wrong (F4 root cause)

`tools/train/train_yolov8_seg.py` claimed to run "live OpenVINO export" but its only
reachable code path in this environment was `--dry-run`, which synthesised the IR itself:

```python
bin_path.write_bytes(b"\x00\x00\x01\x00" * 16384)   # 64 KB repeating stub
```

Two design faults made this silent rather than loud:

1. A fabricated `.bin` was written into the **shipping directory** on a dry run, so CI and
   reviewers saw a "model" on disk and the retraining gate passed on hardcoded metrics.
2. The real export path called `core.read_model(<source>)` on a `.pt` file, which OpenVINO
   cannot read, and its `ImportError` fallback re-entered the stub writer. Any failure
   degraded into a plausible-looking artifact instead of stopping.

Measured before (on the same host):

| Metric | Before (stub) | After (this change) |
|---|---:|---:|
| `yolov8n_seg.bin` bytes | 65,536 | **6,518,038** |
| MiB | 0.0625 | **6.216** |
| Shannon entropy (bits/byte) | 0.8113 | **7.4324** |
| Unique byte values (1 MiB sample) | 2 | **256** |
| IR layer count | 3 (hand-written XML) | **769** |
| `openvino.Core().read_model()` | fails | passes |
| `compile_model("CPU")` | fails | passes |
| Inference | fails | `(1,42,3549)` + `(1,32,104,104)` |

## 2. Environment used for this run

| Component | Version |
|---|---|
| Python (`.venv-seg/bin/python3`) | 3.14.6 |
| ultralytics | 8.4.190 |
| openvino | 2026.4.1 |
| torch | 2.14.1 (MPS available) |
| Host | Apple M4, macOS, arm64 (AVX2 worker not available here) |
| Checkpoint | `yolov8n-seg.pt` (7,071,756 B, Ultralytics nano segmentation backbone) |

The inference worker's target is an AVX2 x86-64 CPU. The latency numbers in §5 come from
this arm64 host and are **not** the worker's AVX2 figures; they are reported as a local
regression guard, not as production capacity evidence.

## 3. What the pipeline does now

`tools/train/train_yolov8_seg.py` (rewritten export path):

1. **Rebuilds the 6-class graph** from the authentic checkpoint YAML (`nc: 6`, class names
   `roadway, water_puddle, motorcycle, car, bus, truck`) and transplants every
   shape-compatible tensor (381 / 417 tensors, 89.0 % of 3,264,786 parameters: CSP
   backbone, PAN neck, box branch, mask-coefficient branch, prototype branch).
   *Why re-head at all*: the COCO checkpoint emits 116 channels (`4 + 80 + 32`). The
   consumer contract in `services/cv-engine/server.py` decodes exactly **42** channels
   (`4 box + 6 class + 32 mask coefficients`, rows `data[10:42]`). Shipping the 80-class
   graph would break the decoder silently.
2. **Exports through Ultralytics + OpenVINO** with `quantize=16` → `compress_to_fp16=True`,
   `imgsz=416`, `dynamic=True`, inside a private staging directory (no writes into the
   caller's CWD, no in-place clobbering of the deployment directory).
3. **Fails closed on an untrained head.** If any tensor had to be re-initialised, the export
   aborts unless `--allow-untrained-head` is passed, and then records
   `head_status: reinitialised_untrained` in `metadata.yaml`.
4. **Verifies the artifact** (`verify_openvino_ir`): size, Shannon entropy, byte diversity,
   periodic-stub detection, XML layer count, `read_model`, `compile_model("CPU")`, output
   channel/stride shapes, and **execution at 416 px and 320 px**.
5. **No longer writes any placeholder IR on `--dry-run`** — it logs that export is skipped.

### 3.1 Static-vs-dynamic defect found by this verification

The first real export was static (`dynamic=False`). It compiled and served 416 px, but the
cv-engine load-shedding path (Issue #379, `X-Target-Resolution: 320`) returned **HTTP 500**:

```
[CPU] PowerStatic node with name 'Multiply_6259' Check 'input_shape[j] == 1' failed
Eltwise shape infer input shapes dim index: 2 mismatch
```

Root cause: `services/cv-engine/server.py` calls `model.reshape([-1, 3, -1, -1])` and treats a
successful reshape as "the graph accepts per-request spatial sizes". A static Ultralytics IR
*accepts* that reshape at IR level while its stride-locked anchor/`Multiply` constants are
still welded to the export resolution, so the CPU plugin rejects 320 px at inference time.
The service therefore advertised a capability it could not execute.

Fix: export with `dynamic=True` and prove it — `verify_openvino_ir` now runs a real inference
at every load-shedding resolution and raises with an actionable message if the graph claims
dynamic but cannot execute it. Measured cost on this host: p95 ~13.7 ms (dynamic, 416 px)
vs ~12.8 ms (static, 416 px), i.e. under 1 ms of jitter-level difference for keeping 320 px
load shedding alive.

## 4. Shipped artifact facts

| Field | Value |
|---|---|
| `models/yolov8n_seg_openvino_416/yolov8n_seg.xml` | 409,568 B |
| `models/yolov8n_seg_openvino_416/yolov8n_seg.bin` | **6,518,038 B (6.216 MiB / 6.518 MB)** |
| IR pair total | 6.607 MiB (ceiling 10.0 MiB) |
| `.bin` SHA-256 | `bbc1fc66798e5e99e322336ea35fcb25440dd109baaada61133eda62fe475120` |
| Shannon entropy | 7.4324 bits/byte (floor 3.0) |
| Unique byte values | 256 (floor 32) |
| XML layers | 769 |
| Input | `[?, 3, ?, ?]`, dynamic (executes at 320 and 416) |
| Outputs | `(1, 42, 3549)` + `(1, 32, 104, 104)` at 416 px; `(1, 42, 2100)` + `(1, 32, 80, 80)` at 320 px |
| Class head | `reinitialised_untrained` (see §7) |
| OpenVINO | 2026.4.1, compiled on CPU |

## 5. Verification log (verbatim, 2026-11-10 17:12)

```
[INFO] Rebuilt 6-class segmentation graph from yolov8n-seg.pt: 381/417 tensors transplanted (89.0% of 3264786 parameters), 36 re-initialised
[WARNING] Exported IR head_status=reinitialised_untrained: these weights are not fine-tuned on the ProjectKV segmentation dataset and must not be promoted to production.
[INFO] OpenVINO FP16 IR written: yolov8n_seg.xml (409568 B) + yolov8n_seg.bin (6518038 B) = 6.61 MiB
[INFO] OpenVINO IR verified: bin=6.216 MiB entropy=7.432 bits/byte layers=769 input_mode=dynamic compile=CPU p95@416px=13.71 ms; also executed at 320px
[INFO] [ROLLBACK TEST] Renamed yolov8n_seg.xml -> yolov8n_seg.xml.bak
[INFO] [ROLLBACK TEST] Clean fallback to detection model: yolov8n.xml
[INFO] [ROLLBACK TEST] Restored primary model: yolov8n_seg.xml
[INFO] Operational rollback verification PASSED 100%
[INFO]   Verdict:              PASSED
[INFO]   Metrics provenance:   docs/reports/seg_eval_v2.md
[INFO]   water_puddle IoU:     0.6240 (Threshold >= 0.6120)
[INFO]   Mask mAP50-95:        0.5280 (Threshold >= 0.4960)
[INFO]   p95 Latency:          13.71 ms (Threshold <= 25.00 ms)
[INFO]   Model Size:           6.61 MiB (Threshold <= 10.00 MiB)
[INFO] Quantitative benchmark gate PASSED 100%
[INFO] --- EXPORT-ONLY PIPELINE COMPLETED SUCCESSFULLY ---
```

Evidence Integrity gate on the same tree:

```
$ python3 tools/ci/check_evidence_integrity.py --mode gate
Evidence Integrity CI Gate: evaluated 34 artifacts.
PASS: All evidence artifacts satisfy integrity, temporal, model, and anti-spoofing gates.
```

The Sprint 10 baseline row that grandfathered the 64 KB stub was deleted in this change;
leaving it behind is now a **stale-baseline failure** (`--mode gate` rejects it), which is
the ratchet working as designed.

## 6. End-to-end serving through the real consumer

`services/cv-engine/server.py` started with `MODEL_DIR=models/yolov8n_seg_openvino_416` and
fed a real camera frame (`data/proofs/proofs/2026-11-06/662b57471afb9c00172d9095/1793945296_sample.jpg`).
Raw responses: [`seg_cv_engine_e2e.json`](seg_cv_engine_e2e.json).

| Request | HTTP | `model` reported | `input_resolution` | Wall latency |
|---|---:|---|---|---:|
| `GET /healthz` | 200 | `model_loaded: true` | — | — |
| `POST /analyze` `X-Target-Resolution: 416` | 200 | `yolov8n_seg_openvino_416` | `[416, 416]` | 41.4 ms |
| `POST /analyze` `X-Target-Resolution: 320` | 200 | `yolov8n_seg_openvino_320` | `[320, 320]` | 19.5 ms |
| `POST /analyze` `X-Target-Resolution: 416` (repeat) | 200 | `yolov8n_seg_openvino_416` | `[416, 416]` | 22.0 ms |

Load model log:

```
[INFO] cv-engine: Dynamic reshape succeeded on .../yolov8n_seg.xml
[INFO] cv-engine: OpenVINO model compiled successfully on CPU in 98.1 ms
[INFO] cv-engine: Input resolution mode: dynamic (compiled input shape None); per-request X-Target-Resolution 320/416 is honoured
```

Wall latency includes JPEG decode and letterboxing; the graph-only p95 is 13.71 ms (§4).
`vehicles_detected: 0` in every response is the expected consequence of the untrained head
(§7) — it is not evidence that flood segmentation works.

## 7. Deployment status and the promotion gate

Acceptance criteria from Issue #399, point by point:

| Criterion | Verdict | Evidence |
|---|---|---|
| `yolov8n_seg.bin` has real weights (> 6.0 MB) | **PASS** | 6,518,038 B, entropy 7.4324, 256 unique bytes, 769 layers |
| OpenVINO compiles the graph successfully | **PASS** | `compile_model("CPU")` + inference at 320/416 px |
| Quantitative gate clears IoU >= 0.612 on real validation images | **NOT MET** | gate passes on the documented v2 metrics (`seg_eval_v2.md`); no image files exist in `data/yolo_seg/images/`, so no held-out evaluation could be run |
| Trained on 550+ frame dataset v2 on a GPU instance | **NOT MET** | dataset has 301 label files and **0** frames on this host; no GPU instance was available |

Why the artifact is still worth shipping to the *staging* volume: it is a real, compilable,
contract-correct 42-channel graph. It replaces an unloadable stub, so the runtime path is now
exercised honestly instead of being dead. What it must not do is masquerade as a trained
model: `metadata.yaml` carries `head_status: reinitialised_untrained`, and the export refuses
to do this without an explicit flag.

**Promotion checklist before production:**

1. Materialize `data/yolo_seg/images/{train,val}` from R2 / the label store (301 labels exist).
2. `make train-seg` on the GPU instance → `runs/segment/train_yolov8n_seg/weights/best.pt`.
3. `make export-seg-openvino SEG_CHECKPOINT=runs/segment/train_yolov8n_seg/weights/best.pt`
   (no `--allow-untrained-head`; the guard proves the head was trained), then confirm
   `metadata.yaml` shows `head_status: trained`.
4. Run `make benchmark-real` on the AVX2 worker and append the p95/mIoU figures here.
5. Only then copy the IR pair onto the inference worker's model volume.

## 8. Manual test scenarios executed

| # | Scenario | Result |
|---|---|---|
| 1 | Export from `yolov8n-seg.pt` with a real OpenVINO conversion | PASS — 6.61 MiB IR, 3.5 s |
| 2 | Stub detector on the old 64 KB artifact | PASS — rejects size, entropy, periodicity and layer count |
| 3 | `read_model` + `compile_model("CPU")` on the shipped IR | PASS — 98.1 ms cold compile |
| 4 | Shape contract vs `cv-engine` | PASS — `(1,42,3549)` + `(1,32,104,104)` |
| 5 | Load-shedding execution at 320 px | PASS — `(1,42,2100)` + `(1,32,80,80)` |
| 6 | Live `POST /analyze` at 416 px and 320 px | PASS — HTTP 200 both times |
| 7 | Rollback: rename seg XML → `.xml.bak`, fall back to detection, restore | PASS |
| 8 | Export without `--allow-untrained-head` (80→6 classes) | PASS — aborts with an explicit message, existing artifact untouched |
| 9 | `--dry-run` | PASS — no IR files written anywhere |
| 10 | `make evidence-lint` (`--mode gate`) | PASS — 34 artifacts, baseline row removed |
| 11 | `python3 -m unittest tools.train.train_yolov8_seg_test` | PASS — 12 tests |

## 9. Reproduction

```bash
# Export + verify + gate + rollback (the run captured in this report)
.venv-seg/bin/python3 tools/train/train_yolov8_seg.py \
  --export-only --model yolov8n-seg.pt --imgsz 416 --nc 6 \
  --allow-untrained-head --verify-gate --test-rollback \
  --report docs/reports/seg_export_evidence.json

# Unit tests
.venv-seg/bin/python3 -m unittest tools.train.train_yolov8_seg_test -v

# Evidence integrity ratchet
.venv-seg/bin/python3 tools/ci/check_evidence_integrity.py --mode gate
```

`models/` is gitignored (`.gitignore:77`), so the IR pair is a build/deploy artifact and is
never committed; this report plus `metadata.yaml` on the deployed volume carry its identity
(size, SHA-256, entropy, provenance).

## 10. Open items

- Fine-tune the 6-class head once dataset images are materialized; re-run this report as v4
  with measured `water_puddle` IoU and mask mAP from the held-out split.
- Re-measure p95 on the AVX2 inference worker (this host is arm64; 13.7 ms is not a worker figure).
- Consider hardening `cv-engine` itself: `reshape()` success is not proof of dynamic
  execution. The exporter now guarantees a genuinely dynamic graph, but a future IR from
  another toolchain could reintroduce the false-positive that produced the 320 px 500.
