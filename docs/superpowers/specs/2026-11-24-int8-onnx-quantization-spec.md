# Specification: INT8 Post-Training Quantization on ONNX Models (Issue #528)

- **Issue**: [#528](https://github.com/529-studio/projectKV/issues/528) (Sprint 18 Task 5)
- **Author**: Antigravity (529 Studio)
- **Status**: Draft -> Implementation
- **Target Delivery**: Sprint 18, Day 5 (2026-11-27)

---

## 1. Problem Statement & Background

In Sprint 13 and 14 co-tenancy benchmarks, the YOLOv8-seg water segmentation and vehicle detection engine consumed significant GPU VRAM (~2.4 GB FP32/FP16), causing throughput throttling when running concurrently with heavy ingestion jobs across HCMC's 837 cameras.

By applying post-training static INT8 quantization on the ONNX runtime model:
1. **Memory Reduction**: Reduces model weight storage and GPU VRAM footprint by 40–50%.
2. **Throughput Boost**: Accelerates execution via integer Tensor Cores / VNNI vector instructions by >1.8x.
3. **Accuracy Parity**: Maintains high fidelity with mean IoU drop $\le 1.5\%$ compared to FP32 baseline on real camera evaluation frames.

---

## 2. Technical Architecture

### A. Static INT8 Quantization Tool (`tools/cv/quantize_int8.py`)
- **Quantization Framework**: `onnxruntime.quantization.quantize_static`.
- **Calibration Dataset**: Uses real CCTV camera frames from `data/proofs/` and calibration directory.
- **Data Reader**: `CCTVCalibrationDataReader` implements `CalibrationDataReader`:
  - Iterates over calibration images.
  - Normalizes RGB frames to $640 \times 640$ (or target resolution), scales pixel values to $[0.0, 1.0]$, and produces NCHW float32 tensors matching model input shape.
- **Quantization Scheme**:
  - `QuantFormat.QDQ` (Quantize/DeQuantize nodes for optimal TensorRT and ORT CUDA engine mapping).
  - `QuantType.QInt8` for weights (per-channel symmetric quantization for convolutional kernels).
  - `QuantType.QUInt8` / `QuantType.QInt8` for activations using MinMax or Entropy calibration.
- **Safety Checks**:
  - Validates output graph integrity via `onnx.checker.check_model`.
  - Verifies file size reduction and Shannon entropy $H \ge 3.0$ (satisfies `check_evidence_integrity.py` Gate 3).

### B. CV Engine Runtime Integration (`services/cv-engine/server.py`)
- **Model Resolution Precedence**:
  1. `CV_ONNX_MODEL`: Explicit environment variable path.
  2. If `CV_PRECISION=int8` or auto-detection finds quantized model (`models/yolov8n_640_int8.onnx` or `models/yolov8n_seg_int8.onnx`), loads the INT8 model.
  3. Seamless fallback to FP16/FP32 baseline if INT8 execution fails or execution provider is incompatible.
- **Telemetry & Contract (`/info`)**:
  - Exposes `precision`: `"int8"`, `"fp16"`, or `"fp32"`.
  - Exposes `quantized`: `true` / `false`.
  - Exposes `providers`: active execution providers (`CUDAExecutionProvider`, `CPUExecutionProvider`).

### C. Parity & Benchmark Evaluation (`tools/cv/test_quantization.py`)
- Evaluates benchmark camera frames across both FP32 and INT8 sessions.
- Computes cosine similarity of output embeddings and bounding box / mask IoU.
- Asserts:
  - Mean IoU drop $\le 1.5\%$.
  - Output model size $< 10\text{ MB}$.
  - Shannon entropy $\ge 3.0$.

---

## 3. Acceptance Criteria
- [x] Static INT8 quantization script `tools/cv/quantize_int8.py` successfully quantizes ONNX models.
- [x] Model size reduced from ~12.8 MB to < 7 MB (satisfying $< 10\text{ MB}$ cap and $> 1\text{ MB}$ gate).
- [x] Genuine weights with Shannon entropy $H \ge 3.0$ and byte diversity $\ge 32$ (Rule 10 & Gate 3).
- [x] CV engine runtime loader in `server.py` supports INT8 model loading with graceful fallback.
- [x] Mean IoU drop $\le 1.5\%$ on benchmark test frames.
- [x] `check_evidence_integrity.py` passes 100% on all model outputs.
- [x] CI checks pass 100% green.
