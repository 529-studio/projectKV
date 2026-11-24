# Implementation Plan: INT8 Post-Training Quantization on ONNX Models (Issue #528)

- **Issue**: [#528](https://github.com/529-studio/projectKV/issues/528) (Sprint 18 Task 5)
- **Active Effort Estimate**: 5–7 engineering hours (AGENTS.md Rule 3).
- **Delivery Window**: 2026-11-27 (Sprint 18, Day 5).

---

## Proposed Changes

### 1. ONNX Static Quantization Pipeline (`tools/cv/quantize_int8.py`)
- Implement `CCTVCalibrationDataReader` utilizing real CCTV frames from `data/proofs/`.
- Preprocess images with letterboxing/resize to $640 \times 640$, normalize to $[0, 1]$, convert to NCHW float32.
- Configure `onnxruntime.quantization.quantize_static`:
  - `QuantFormat.QDQ`
  - Per-channel weight quantization (`per_channel=True`)
  - `weight_type=QuantType.QInt8`
  - `activation_type=QuantType.QUInt8`
- Export to target path (e.g. `models/yolov8n_640_int8.onnx`).
- Add command-line interface with flags: `--input`, `--output`, `--calibration-dir`, `--num-samples`.

### 2. Parity & IoU Verification Suite (`tools/cv/test_quantization.py`)
- Load both baseline FP32 model and quantized INT8 model via ONNX Runtime.
- Run inference across 50–100 benchmark camera frames.
- Compare output tensors:
  - Numerical deviation (MAE, cosine similarity).
  - Bounding box / mask prediction IoU.
  - Assert mean IoU drop $\le 1.5\%$.
- Verify Gate 3 model invariants:
  - Shannon entropy $\ge 3.0$
  - Model size $< 10\text{ MB}$ and $> 1\text{ MB}$
  - Byte diversity $\ge 32$

### 3. CV Engine Runtime Loader (`services/cv-engine/server.py`)
- Detect INT8 quantized models from `CV_ONNX_MODEL` or default model search path.
- Add `precision` metadata to loaded model and report via `GET /info`.
- Handle fallback to FP16/FP32 if provider fails or file is absent.
- Add unit tests in `services/cv-engine/test_server.py`.

### 4. Makefile & CI Target Integration
- Add `make quantize-int8` and `make test-quantize` targets.
- Integrate into test suite.
- Audit evidence integrity via `make evidence-lint`.

### 5. Living Documentation & Lesson (`docs/lessons/528-int8-onnx-quantization.md`)
- Record engineering findings, memory savings, latency profiles, and INT8 calibration insights.
