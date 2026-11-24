# Lesson 528: Post-Training Static INT8 Quantization on ONNX Models & Head Preservation

## Navigation Aid & Quick Reference
- **Domain**: Computer vision inference optimization, INT8 post-training static quantization (PTQ), ONNX Runtime execution, GPU memory footprint reduction, detection head preservation.
- **Relevant Issues**: Issue #528 (S18-T5), Issue #379 (S15-T5), Issue #104 (Spike CV Runtime).
- **Key Files**:
  - `tools/cv/quantize_int8.py` (Static QDQ and dynamic INT8 post-training quantization pipeline using real camera calibration frames)
  - `tools/cv/test_quantization.py` (Parity benchmark evaluation suite: cosine similarity, matched IoU, latency, Gate 3 invariants)
  - `tools/cv/test_quantize_unit.py` (Hermetic unit test suite for math and calibration helpers)
  - `services/cv-engine/server.py` (`resolve_onnx_model_path`, `resolve_precision`, `effective_precision`, `/info` telemetry, graceful baseline fallback)
  - `services/cv-engine/test_server.py` (`TestINT8QuantizationResolution`, `/info` endpoint precision checks)
- **Core Invariants**:
  - **Conv-Only QDQ Quantization for Non-Linear Heads**: Never apply unrestricted MinMax activation quantization across non-linear output heads (Sigmoid, Softmax, Proto linear combination). Quantizing `Conv` nodes captures >99% of weights and FLOPs while preserving >99.9% prediction tensor fidelity.
  - **Authentic CCTV Calibration Frames**: Calibration data readers must exclusively consume authentic camera frames from `data/proofs/`, never synthetic noise or uniform tensors.
  - **Fail-Safe Runtime Fallback**: If an INT8 quantized model fails to initialize or encounters runtime provider incompatibilities, the engine must gracefully fall back to the FP32/FP16 baseline without dropping requests.
  - **Gate 3 Model Weight Invariants**: Every quantized model binary must satisfy: size in $[1\text{ MB}, 10\text{ MB}]$, Shannon entropy $H \ge 3.0$, byte diversity $\ge 32$, and gzip compressibility ratio $\ge 0.60$.

---

## 1. Context & Problem Statement

During Sprint 13 and Sprint 14 co-tenancy evaluations, running full-precision YOLOv8n object detection and segmentation models across Ho Chi Minh City's 837 traffic cameras consumed substantial GPU VRAM (~2.4 GB FP32/FP16 per instance). When concurrent background tasks (such as frame archiving, ingestion, and congestion forecasting refits) executed on shared edge instances, VRAM saturation led to CUDA out-of-memory errors and throughput throttling.

Issue #528 targeted static INT8 post-training quantization (PTQ) to reduce memory consumption by $\ge 40\%$ and improve integer throughput on Tensor Cores / VNNI vector units, while constraining prediction fidelity loss to $\le 1.5\%$.

---

## 2. Root Cause Discovery: The YOLO Head Quantization Trap

When initially applying unrestricted static quantization (`quantize_static`) with MinMax calibration across all operator types in the YOLOv8 ONNX graph:
1. **Detection Score Collapse**: Maximum output class probabilities collapsed from $0.898$ in FP32 to $0.000$ in INT8.
2. **Root Cause**: The YOLOv8 head performs non-linear operations (Sigmoid activation on class scores, multiplication with anchor grid strides, continuous linear combination for segmentation prototypes). Quantizing activation ranges across these final layers with coarse 8-bit dynamic ranges clamped probability scores near zero.
3. **Architectural Resolution**:
   - In convolutional neural networks, `Conv` operators account for over 99% of the parameter count and over 90% of multiply-accumulate FLOPs.
   - Restricting quantized op types to `op_types_to_quantize=['Conv']` keeps the entire backbone and feature pyramid neck in 8-bit integer QDQ representation, while leaving the lightweight final classification and coordinate transformation heads in 32-bit floating point.
   - Result: Model size dropped by **71.9%** (12.26 MB $\to$ 3.45 MB for detection; 13.14 MB $\to$ 3.68 MB for segmentation), while tensor cosine similarity jumped from 0.0 to **0.9993** (fidelity drop $< 0.07\%$).

---

## 3. Implementation Architecture

### A. Calibration & Quantization Toolchain (`tools/cv/quantize_int8.py`)
- `CCTVCalibrationDataReader`: Feeds authentic CCTV frames from `data/proofs/`, applying letterboxing to model native resolution.
- `quantize_onnx_model`: Configures `onnxruntime.quantization.quantize_static` with `QuantFormat.QDQ`, per-channel symmetric weights (`QuantType.QInt8`), unsigned activations (`QuantType.QUInt8`), and `op_types_to_quantize=['Conv']`.
- Supports `--mode static` and `--mode dynamic` with automated input dimension inspection (`inspect_model_input_metadata`).

### B. Verification & Parity Suite (`tools/cv/test_quantization.py`)
- Evaluates output tensors across benchmark camera frames.
- Asserts primary tensor cosine similarity $\ge 0.985$ (achieved $0.9993$ on detection and $0.9952$ on segmentation).
- Measures matched bounding box IoU ($\ge 0.93$) and latency speedup.
- Enforces Gate 3 invariants (Shannon entropy $\ge 3.0$, model size $< 10\text{ MB}$ and $> 1\text{ MB}$, byte diversity $\ge 32$).

### C. CV Engine Runtime Integration (`services/cv-engine/server.py`)
- Introduces `CV_PRECISION` environment configuration (`auto`, `int8`, `fp16`, `fp32`).
- Implements `resolve_onnx_model_path()`: automatically selects INT8 quantized model peer when available, with precedence rules for explicit operator overrides.
- Implements graceful fallback: if an INT8 model fails during session initialization, logs a warning and automatically instantiates the FP32 baseline.
- Exposes runtime precision and quantization state via `GET /info` (`precision`, `quantized`).

---

## 4. Empirical Benchmark Results

Evaluation on authentic CCTV frames (`data/proofs/`):

| Metric | Baseline FP32 | Quantized INT8 | Impact / Delta |
| :--- | :--- | :--- | :--- |
| **Detection Model Size** | 12.26 MB | 3.45 MB | **-71.9% reduction** |
| **Segmentation Model Size** | 13.14 MB | 3.68 MB | **-72.0% reduction** |
| **Shannon Entropy** | 6.85 bits/byte | 6.78 bits/byte | Pass ($H \ge 3.0$) |
| **Byte Diversity** | 256 unique | 256 unique | Pass ($\ge 32$) |
| **Detection Cosine Sim** | 1.0000 | 0.9993 | **0.068% drop** ($\le 1.5\%$) |
| **Segmentation Cosine Sim** | 1.0000 | 0.9952 | **0.482% drop** ($\le 1.5\%$) |
| **Matched Detection IoU** | 1.0000 | 0.9324 | High geometric parity |
| **Gate 3 Verification** | PASS | PASS | 100% compliant |

---

## 5. Guardrails & Best Practices
1. **Never quantize YOLO head nodes with default global activations**: Always verify output class score distributions after static quantization before deploying.
2. **Always test against authentic camera frames**: Synthetic black/white or uniform noise frames corrupt calibration histograms and cause severe activation clipping.
3. **Preserve fail-safe FP32 fallback**: Never hard-fail the entire service container if an optional INT8 weight artifact is missing or corrupted.
