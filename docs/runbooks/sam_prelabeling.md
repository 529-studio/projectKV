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
1. **Host Disqualification**: The production ingress host (`home-srv` Intel Celeron) lacks AVX2 vector instructions and must never execute batch AI annotation workloads that could degrade the live 30-second camera polling loop.
2. **Authorized Host**: Inference runs offline on `dev-box` (AMD Ryzen 7 6800HS with AVX2), sharing cgroups with the CV engine and GitHub Actions runner.
3. **Host Guard**: `prelabel_sam.py` inspects host CPU flags. On Linux x86_64, missing AVX2 halts execution immediately with an exit code. On developer machines (e.g. macOS Apple Silicon), pass `--allow-unsafe-host` to bypass the x86 guard for local development.

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
