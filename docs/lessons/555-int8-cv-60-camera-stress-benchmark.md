# Lesson 555: 60-Camera Concurrent Stream Stress Test & INT8 Latency Benchmark

## Navigation Aid & Quick Reference
- **Domain**: Performance benchmarking, concurrent CV inference stress testing, SLA latency percentiles (`P50`, `P90`, `P95`, `P99`), admission control.
- **Relevant Issues**: Issue #555 (S19-T7), Issue #553 (Secondary Edge INT8 Deployment), Issue #528 (INT8 Quantization).
- **Key Files**:
  - `tools/eval/benchmark_int8_stress.py` (60-worker ThreadPoolExecutor stress generator targeting `POST /predict` with `X-Camera-ID` and priority headers)
  - `tools/eval/test_benchmark_int8_stress.py` (Unit and 60-camera live HTTP stress harness verification)
  - `services/cv-engine/server.py` (Increased `CVServer.request_queue_size` from Python's default `5` to `128` to prevent TCP SYN drops during 60-camera bursts)
- **Core Invariants**:
  - **Real HTTP Load Contract**: Dispatches authentic `image/jpeg` payloads with `X-Camera-ID`, `X-Capture-Timestamp`, and `X-Corridor-Priority` headers matching `services/cv-engine/server.py`.
  - **TCP Listen Backlog Floor**: `CVServer.request_queue_size = 128` prevents connection errors and SYN-retransmit latency spikes (`>1000ms` P95 observed with `request_queue_size = 5`) under 60 concurrent streams.
  - **Latency & Error Gate**: Fails cleanly (exit code 1) if `P95 > 200.0 ms` or unhandled error rate exceeds `1%`.

---

## 1. Context & Performance Need

During severe storm events across Ho Chi Minh City, up to 60 priority flood-chokepoint cameras dispatch snapshot frames simultaneously to `cv-engine`. Ensuring our INT8 ONNX pipeline sustains sub-200ms P95 latency without connection drops requires a repeatable concurrent load harness.

---

## 2. Verification & Empirical Proof

1. **Automated Unit & Live Concurrency Stress Test**:
   - Executed `python3 -m unittest tools/eval/test_benchmark_int8_stress.py`: Verified 60 concurrent streams / 180 frames with 0 errors and sub-200ms P95 latency.
2. **CLI Stress Execution**:
   - Executed `python3 tools/eval/benchmark_int8_stress.py --local-sim-server --concurrency 60 --requests 300`: Clean pass.
