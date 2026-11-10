# Lesson 82: Authentic Model Artifacts — No Fabricated IR, No Silent Untrained Heads

## Navigation Aid & Quick Reference
- **Domain**: Model export pipelines (PyTorch → OpenVINO IR), evidence integrity, consumer
  contract enforcement, dry-run semantics
- **Relevant Issues / Findings**: Issue #399 (S11-T5), ADR-013 F4 (64 KB dummy OpenVINO stub),
  ADR-009 §4 (42-channel 6-class segmentation contract), Issue #379 (320/416 load shedding)
- **Key Files**:
  - `tools/train/train_yolov8_seg.py` (real export, IR verification, untrained-head guard)
  - `tools/train/train_yolov8_seg_test.py` (stub rejection, artifact and provenance tests)
  - `services/cv-engine/server.py` (consumer: 42-channel decode, `reshape` before compile)
  - `.evidence-integrity-baseline.txt` (the stub's grandfather row had to be deleted)
  - `docs/reports/seg_eval_v3.md` (this cycle's evidence)
- **Core Invariants**:
  - **A dry run writes no artifact.** No failure path may synthesise a plausible weight file.
  - **The exported graph matches the consumer's tensor contract**, not the base checkpoint's.
  - **Untrained weights cannot be exported or promoted silently** — provenance is machine-readable.
  - **A capability claim is proven by execution, not by an accepted `reshape()`.**

---

## 1. Context

Issue #399 was opened because `models/yolov8n_seg_openvino_416/yolov8n_seg.bin` was
65,536 bytes of `\x00\x00\x01\x00` repetition — entropy 0.81, two unique byte values. The
retraining gate still passed, because it reads hardcoded metrics, and CI passed, because the
64 KB file was grandfathered in `.evidence-integrity-baseline.txt`.

Reading the generator explained the whole failure in one line:

```python
bin_path.write_bytes(b"\x00\x00\x01\x00" * 16384)
```

That line lived inside `export_to_openvino_fp16(..., dry_run=True)` — the only branch that
ever executed. The "real" branch called `core.read_model(<.pt>)`, which OpenVINO cannot read,
and its `ImportError` fallback re-entered the stub writer. So the pipeline could not produce
a real model, and every failure mode produced something that *looked* like one.

## 2. Root causes

### A. Fabricating an artifact where the real one belongs
A dry run exists to avoid expensive work, not to substitute a fake for the deliverable. Writing
the placeholder to the **shipping directory** made the fraud indistinguishable from success for
every downstream reader: CI, reviewers, and the deploy script.

### B. Degrading failures into plausible output
`except ImportError: return export_to_openvino_fp16(..., dry_run=True)` converted "the runtime
is missing" into "here is a model". A failure that produces an artifact instead of an error is
worse than a crash, because the error surfaces later as a runtime mystery.

### C. Exporting the wrong graph for the consumer
`yolov8n-seg.pt` is the COCO checkpoint: 80 classes, `4 + 80 + 32 = 116` output channels.
`services/cv-engine/server.py` decodes exactly **42** rows (`data[10:42]` as mask
coefficients). Exporting the base checkpoint verbatim would have compiled cleanly, served
HTTP 200, and decoded garbage — a silent contract break that no unit test would catch.

### D. `reshape()` success treated as a dynamic-execution guarantee
`cv-engine` calls `model.reshape([-1, 3, -1, -1])` and, on success, advertises "dynamic" and
honours `X-Target-Resolution: 320`. A static Ultralytics IR **accepts** that reshape (shape
inference passes) and then dies inside the Intel CPU plugin on the stride-locked
`Multiply`/anchor constants:

```
[CPU] PowerStatic node with name 'Multiply_6259' Check 'input_shape[j] == 1' failed
Eltwise shape infer input shapes dim index: 2 mismatch
```

Result: `POST /analyze` with `X-Target-Resolution: 320` returned **HTTP 500** while 416 was
fine. The graph claimed a capability it could not execute.

### E. Exporter output paths are relative to the process CWD
Ultralytics derives the export directory from `model.pt_path` / `yaml_file`, and for a `.yaml`
source it strips the directory (`file = Path(file.name)`). A caller passing an absolute config
path still got `./yolov8n_seg_openvino_model/` written into the repository root, where it was
invisible to `.gitignore` rules written for `models/`.

## 3. Solution

1. **Real conversion only.** Rebuild the graph from the checkpoint's own YAML with
   `nc=6`, transplant every shape-compatible tensor via a local `_intersect_state_dicts`
   (381/417 tensors, 89.0 % of parameters — CSP backbone, PAN neck, box branch, mask branch,
   prototype branch), then export with Ultralytics `quantize=16` →
   `ov.save_model(..., compress_to_fp16=True)`. Output: 6,518,038 B `.bin`, entropy 7.4324,
   769 layers, `(1,42,3549)` + `(1,32,104,104)`.
2. **Dry run writes nothing.** `export_to_openvino_fp16(dry_run=True)` logs that export is
   skipped and returns `(None, None)`. No placeholder, ever.
3. **Fail closed on an untrained head.** If any tensor had to be re-initialised,
   `export_to_openvino_fp16` aborts with the count and the reason unless
   `--allow-untrained-head` is passed; even then it writes
   `projectkv.head_status: reinitialised_untrained` into `metadata.yaml`. A model whose head
   is random emits zero detections and makes the CV engine report `CLEAR` everywhere —
   silent false negatives in a flood-warning system.
4. **Verify by execution, per resolution.** `verify_openvino_ir()` checks size, entropy, byte
   diversity, periodic-stub repetition, XML layer count, `read_model`, `compile_model("CPU")`,
   output channel/stride shapes, and runs real inference at **both 416 px and 320 px**. A
   falsely-dynamic graph now fails at export time with "re-export with dynamic=True".
5. **Export dynamic by default** (`dynamic=True`) so the load-shedding contract holds; static
   remains available via `--static`. Measured cost on the dev host: p95 14.04 ms vs 12.79 ms.
6. **Private staging directory.** `model.model.pt_path` is pinned to an absolute path inside a
   `tempfile.mkdtemp()` staging dir, so the exporter cannot write into the caller's CWD; the
   IR pair is moved into the deployment directory only after a non-zero-size check.
7. **Ratchet entry deleted.** Removing the stub removed the grandfather row; `--mode gate` now
   fails on it as a stale entry until deleted, which is the ratchet doing its job.

## 4. Prevention rules

1. **Never write a placeholder into a shipping path.** If the real artifact is unavailable,
   produce nothing and fail loudly. A plausible fake defeats every downstream check at once.
2. **Never let an exception handler produce a deliverable.** `except ImportError: generate
   stand-in` is a silent contract with the future: it moves the failure to production.
3. **Export the graph the consumer decodes, not the checkpoint you downloaded.** Derive the
   head width (`4 + nc + 32`) from the service's decoder and assert on the real inference
   shapes.
4. **Prove capability claims by execution.** `reshape()` accepting a shape, or a compiled model
   existing, is not evidence that a given resolution runs. Execute every supported shape.
5. **Make provenance machine-readable.** `head_status` next to the weights answers "were these
   trained?" without reading a training log — and lets a guard refuse promotion.
6. **Isolate exporter staging.** Third-party exporters choose their own output directory; pin
   it explicitly and move artifacts into place atomically.
7. **A gate that can pass on hardcoded metrics is not a gate.** Treat default metric arguments
   as documentation, and feed measured latency/size from the artifact under test.

## 5. Verification & evidence

- `python3 -m unittest tools.train.train_yolov8_seg_test` — 12 tests, including
  `test_verify_openvino_ir_rejects_repeating_stub`,
  `test_committed_ir_is_authentic_and_loadable`,
  `test_committed_metadata_records_training_provenance`,
  `test_export_refuses_untrained_head_by_default`.
- `python3 tools/ci/check_evidence_integrity.py --mode gate` — PASS over 34 artifacts after
  deleting the stale stub row.
- Live consumer: `services/cv-engine/server.py` served `POST /analyze` with HTTP 200 at both
  416 px (`yolov8n_seg_openvino_416`) and 320 px (`yolov8n_seg_openvino_320`); evidence in
  `docs/reports/seg_cv_engine_e2e.json`.
- Rollback: rename `yolov8n_seg.xml` → `.xml.bak`, fall back to `yolov8n.xml`, restore — PASS.
- Honest limit: the head is `reinitialised_untrained`; `data/yolo_seg/images/{train,val}` hold
  **0** frames (301 labels). Empirical IoU is still pending and the artifact is not promotable.
