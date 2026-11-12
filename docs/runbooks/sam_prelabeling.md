# SAM-Assisted Polygon Pre-Labeling Batch Toolchain Runbook

## 1. Overview & Context

This runbook documents the operational procedure for executing the SAM-assisted polygon pre-labeling toolchain (Sprint 9 Issue #298 / S9-T2 / ADR-011 D3).

Hand-drawing 500+ segmentation masks on raw traffic camera frames requires significant manual labor (10-15s per polygon). The pre-labeling toolchain automates candidate proposal generation:
- **Roadway**: Proposes the drivable carriageway polygon using the largest stable central segment below the estimated horizon line.
- **Vehicles**: Detects bounding boxes via YOLOv8n and refines them into instance segmentation polygons using MobileSAM box prompts.
- **Water puddles**: Proposes candidate specular regions as unverified proposals (`proposal-only`), leaving final ground-truth approval to human annotators (per ADR-009 §4.3).

> [!IMPORTANT]
> **Strict Provenance & Human Verification (ADR-011 D3)**: Pre-labeled candidates are never automatically promoted to ground truth. All candidate payloads are stamped with `annotator: "sam_prelabel"` and `source: "sam_prelabel"`. In the Web Labeler UI (`/labeler`), they render as editable polygon overlays displaying the `[Tự động: SAM]` badge. Human review, correction, and explicit submission are mandatory before labels become training data.

---

## 2. Infrastructure & Host Guardrails

To protect production stability and enforce hardware standards (ADR-011 D3 / AGENTS.md Rule 2):
1. **Host Disqualification**: The production ingress host (Intel Celeron) lacks AVX2 vector instructions and must never execute batch AI annotation workloads that could degrade the live 30-second camera polling loop.
2. **Authorized Host**: Inference runs offline on `inference worker` (AMD Ryzen 7 6800HS with AVX2), sharing cgroups with the CV engine and GitHub Actions runner.
3. **Host Guard**: `prelabel_sam.py` inspects host CPU flags. On Linux x86_64, missing AVX2 halts execution immediately with an exit code. On developer machines (e.g. macOS Apple Silicon), pass `--allow-unsafe-host` to bypass the x86 guard for local development.
4. **Off-Peak Guard**: After the host guard, `prelabel_sam.py` refuses to run batch pre-labeling outside the authorized nocturnal window (01:00-05:00 ICT / 18:00-22:00 UTC) so MobileSAM never contends with the live hotset sweep (ADR-012 §3 (S10-T8) / §1.2 (F5)). `--dry-run` bypasses the window check because it performs no inference, and `--allow-daytime` overrides it for manual daytime profiling or debugging after logging an explicit override warning. If the policy module cannot be imported the guard fails closed rather than allowing the batch.

---

## 3. Weight Provisioning (AGENTS.md Rule 8)

Hardcoding paths or embedding weights in git is strictly prohibited. Configure the model ONNX paths in `.env`:

```env
SAM_PRELABEL_ENCODER_ONNX=data/models/mobile_sam_image_encoder.onnx
SAM_PRELABEL_DECODER_ONNX=data/models/sam_mask_decoder_multi.onnx
SAM_PRELABEL_DETECTOR_ONNX=data/models/yolov8n_640.onnx
```

Model specifications:
- **Encoder**: MobileSAM ViT-Tiny image encoder (~27 MB, ONNX opset 17).
- **Decoder**: MobileSAM promptable mask decoder (~16 MB, ONNX opset 17).
- **Detector**: YOLOv8n detector (~12 MB, ONNX, input 640x640).

---

## 4. Operational Execution

### A. Direct Streaming from Cloudflare R2

Streams camera frames from R2, runs inference in memory, and writes candidate label JSONs back to `R2:warded-proofs/labels/<date>/`:

```bash
# Dry run: verify plan without network I/O or inference
python3 tools/seg/prelabel_sam.py --date 2026-10-16 --dry-run

# Run batch pre-labeling on 50 frames with R2 upload
python3 tools/seg/prelabel_sam.py --date 2026-10-16 --limit 50 --upload-r2 --concurrency 4
```

### B. Offline Execution via Local Frame Mirror

If camera frames have already been harvested locally (under `data/proofs/<date>/`):

```bash
python3 tools/seg/prelabel_sam.py \
  --local-frames data/proofs \
  --date 2026-10-16 \
  --limit 50 \
  --upload-r2 \
  --concurrency 4 \
  --allow-unsafe-host
```

### C. Makefile Shortcuts

```bash
# Run unit tests
make test-seg

# Run prelabel CLI
make prelabel-sam DATE=2026-10-16 LIMIT="--limit 50" EXTRA="--upload-r2"
```

---

## 5. Measured Empirical Throughput

Benchmarked against real camera frames from the Saigon camera network (1280x720 and 1920x1080 JPEG):

| Pipeline Stage | Model / Algorithm | Avg Latency / Frame |
|:---|:---|:---:|
| Detection | YOLOv8n (640x640) | ~58 ms |
| Image Embedding | MobileSAM ViT Encoder | ~360 ms |
| Mask Decoding | MobileSAM Multi-Mask Decoder | ~300 ms |
| Post-processing | Polygon Douglas-Peucker & Normalization | ~8 ms |
| **Total per Frame** | **Full SAM Pre-label Pipeline** | **~725 ms** |

Throughput scaling:
- **1 worker thread**: ~1.38 frames/sec (~82 frames/min).
- **4 concurrent workers**: ~5.20 frames/sec (~312 frames/min).
- **50-frame batch execution time**: ~9.6 seconds wall time.

---

## 6. Output Contract & Validation

Candidate labels conform strictly to the S9-T1 `LabelPayload` contract:
- File key on R2: `labels/<date>/<camera_id>_sam_prelabel_<timestamp>.json`
- Schema properties:
  - `snapshot_key`: `<date>/<camera_id>/<timestamp>_sample.jpg`
  - `annotator`: `"sam_prelabel"`
  - `traffic_level`: Calculated from vehicle PCU counts.
  - `annotations`: List of polygon annotations:
    - `class`: One of `roadway`, `water_puddle`, `motorcycle`, `car`, `bus`, `truck`.
    - `polygon`: List of `[x, y]` vertices normalized to `[0.0, 1.0]`, vertex count between 3 and 64.
    - `source`: `"sam_prelabel"`.

Verification command:
```bash
python3 -m unittest discover -s tools/seg -p "*_test.py"
```

---

## 7. Handoff: Pre-Label, Review, Materialize, Train (S12-T5)

SAM output is a proposal, never ground truth. The chain that turns proposals into a
trainable Dataset v3 is:

1. **Pre-label** the harvested date: `make prelabel-sam DATE=2026-11-08`.
2. **Human review** in `/labeler`: correct every `water_puddle` boundary, remove phantom
   puddles, fix roadway/vehicle polygons, and save with a named human `annotator`. The
   validation frames get a second independent review.
3. **Materialize** the reviewed set:
   `make materialize-seg DATASET_DATES=2026-10-23,2026-10-24`. Frames resolve by exact
   `snapshot_key` (local proof roots before R2), labels pair on
   `(camera_id, timestamp)` only, and the camera/date group split is frozen in
   `split.json`. The CLI pairs against the frame keys it scans from the local proof
   roots, so a stray label on a known camera with an unknown timestamp fails as
   camera-only instead of binding to unrelated pixels. `--allow-synthetic` does not
   exist for v3, and there is no `--allow-unreviewed` flag: a proposal-only annotator,
   a missing `reviewed_at` or a retired template polygon fails the build.
4. **Train** on a CUDA host: `make train-seg DATA=data/yolo_seg/dataset.yaml EPOCHS=100 SEED=42`.
   AC10 evidence needs **≥ 50 completed epochs**; a shorter run is refused by the gate
   (there is no promotion bypass).
5. **Gate and export**: `make export-seg-openvino SEG_CHECKPOINT=... EXTRA="--metrics .../val_metrics.json"`,
   then `make bench-seg-avx2` on the AVX2 worker.

### Retirement of the October label sets

`data/yolo_seg`, `data/yolo_seg_v2` and `data/seg_prelabels` are **audit inputs only**. The
October frames no longer exist on R2, so their 301 labels join to 0/301 proof frames, and 261
of the v2 labels were template-generated. Re-running the pairing against them is the negative
control for AC3:

```bash
python3 tools/eval/materialize_seg_dataset.py --dates 2026-10-16 --min-frames 1 \
  --labels-dir data/seg_prelabels --dry-run --limit 5
# non-zero exit, every key listed, nothing written
```

Never re-enable those labels by relaxing the materializer. Re-annotate the frames instead.
