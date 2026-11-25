# Lesson 553: Secondary Edge INT8 Deployment Automation and Runtime Verification

## Navigation Aid & Quick Reference
- **Domain**: Edge inference deployment, INT8 ONNX Runtime CPU execution, artifact integrity verification, SHA-256 manifest generation.
- **Relevant Issues**: Issue #553 (S19-T5), Issue #528 (S18-T5 INT8 Static Quantization).
- **Key Files**:
  - `docker-compose.cv-edge.yml` (CPU-only secondary edge worker compose configuration with `CV_PRECISION=int8`, `CV_REQUIRE_CUDA=0`)
  - `tools/cv/deploy_edge_int8.py` (Pre-deployment artifact validator checking >1MB size, Shannon entropy $H \ge 3.0$, byte diversity, and ONNX Runtime CPU smoke test)
  - `tools/cv/test_deploy_edge_int8.py` (Unit test suite verifying integrity gates and manifest schema)
  - `services/cv-engine/README.md` (Operational documentation for secondary edge worker deployment)
- **Core Invariants**:
  - **Zero-GPU Edge Portability**: `docker-compose.cv-edge.yml` drops NVIDIA device reservations (`CV_REQUIRE_CUDA=0`) and binds `ORT_NUM_THREADS=2` within a 1 GB memory limit so quantized models run smoothly on secondary edge nodes.
  - **Cryptographic & Entropy Gates**: `deploy_edge_int8.py` blocks zero-filled stubs or truncated models before deployment by enforcing AGENTS.md Rule 10 bounds ($>1\text{ MB}$, $H \ge 3.0$ bits/byte, $\ge 32$ unique bytes/64KB).

---

## 1. Context & Deployment Gap

Sprint 18 produced INT8 quantized ONNX weights (`tools/cv/quantize_int8.py`) and updated `services/cv-engine/server.py` with `CV_PRECISION=int8` auto-resolution. However, `docker-compose.cv.yml` remained hardcoded to require an NVIDIA GPU device reservation (`CV_REQUIRE_CUDA=1`), preventing deployment to secondary CPU-only edge nodes, and lacked a pre-flight integrity verifier to guard against corrupt or stub weight transfers.

---

## 2. Verification & Empirical Proof

1. **Compose Schema Validation**:
   - Verified `docker-compose.cv-edge.yml` cleanly parses and resolves defaults via `ruby -ryaml` / `docker compose config`.
2. **Unit Tests (`tools/cv/test_deploy_edge_int8.py`)**:
   - Ran `python3 -m unittest tools/cv/test_deploy_edge_int8.py`: 4/4 passing (entropy bounds, small model rejection, zero-stub rejection, valid artifact manifest generation).
3. **Existing CV Engine Tests**:
   - Verified `services/cv-engine/test_server.py` continues to pass.
