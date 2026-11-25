# ProjectKV CV Engine

Standalone Computer Vision inference service optimized for non-AVX x86_64 hardware (Intel Celeron N2930 / Silvermont) and ARM architectures.

## Architecture

- **Runtime**: Python 3.11 with Intel OpenVINO CPU Plugin using SSE4.2 vector instructions.
- **Port**: `8000` (Internal Docker network only, `traefik.enable=false`).
- **Resource Constraints**: Cgroups limits: `512MB RAM`, `1.0 CPU`.
- **Reference**: Evaluated and benchmarked in [`docs/spike-cv-runtime.md`](../../docs/spike-cv-runtime.md) (Issue #104).

## Backends and inference concurrency

`CV_BACKEND` selects how the YOLOv8n graph executes; preprocessing, post-processing and the HTTP contract are identical on both paths.

- `openvino` (default): Intel CPU plugin, request threads run inference in parallel.
- `ort`: ONNX Runtime, CUDA first with CPU as the creation-time fallback. When the session resolves `CUDAExecutionProvider`, forward passes are **serialized inside the process** (`ORTModel._run_lock`). Concurrent `session.run` calls on the CUDA provider of this deployment (Pascal P106-100, CUDA 12.4) abort the process with `CUDA failure 700: an illegal memory access was encountered`, killing the context and every in-flight request. Serialization costs nothing measurable — YOLOv8n at 320x320 is a few milliseconds while `InferenceQueueManager` bounds admission — and it also makes a session rebuild safe, since swapping the session while another thread is inside `run` is the illegal state itself.
- A CUDA failure inside a run rebuilds the session once and retries; with `CV_REQUIRE_CUDA=1` a rebuild that resolves only `CPUExecutionProvider` exits so the container restarts with a fresh context instead of silently serving on CPU.

## Endpoints

- `GET /healthz`: Health check endpoint.
- `GET /info`: Runtime engine and device information.
- `POST /predict`: Accepts raw JPEG snapshot image bytes and returns vehicle counts, confidence, and traffic severity.

## Local Build & Run

```bash
docker build -t projectkv-model -f services/cv-engine/Dockerfile .
docker run --rm -p 8000:8000 --memory=512m --cpus=1.0 projectkv-model
```

## Secondary Edge INT8 Deployment (CPU Nodes)

Secondary edge nodes run statically quantized INT8 ONNX weights (`yolov8n_640_int8.onnx`) via `CPUExecutionProvider` using `docker-compose.cv-edge.yml`:

```bash
# Verify artifact integrity (Rule 10 size > 1MB & Shannon entropy >= 3.0) and emit manifest
python3 tools/cv/deploy_edge_int8.py --model models/yolov8n_640_int8.onnx --smoke-test

# Launch secondary CPU edge worker
docker compose -f docker-compose.cv-edge.yml up -d
```
