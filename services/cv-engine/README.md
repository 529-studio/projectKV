# ProjectKV CV Engine

Standalone Computer Vision inference service optimized for non-AVX x86_64 hardware (Intel Celeron N2930 / Silvermont) and ARM architectures.

## Architecture

- **Runtime**: Python 3.11 with Intel OpenVINO CPU Plugin using SSE4.2 vector instructions.
- **Port**: `8000` (Internal Docker network only, `traefik.enable=false`).
- **Resource Constraints**: Cgroups limits: `512MB RAM`, `1.0 CPU`.
- **Reference**: Evaluated and benchmarked in [`docs/spike-cv-runtime.md`](../../docs/spike-cv-runtime.md) (Issue #104).

## Endpoints

- `GET /healthz`: Health check endpoint.
- `GET /info`: Runtime engine and device information.
- `POST /predict`: Accepts raw JPEG snapshot image bytes and returns vehicle counts, confidence, and traffic severity.

## Local Build & Run

```bash
docker build -t projectkv-model -f services/cv-engine/Dockerfile .
docker run --rm -p 8000:8000 --memory=512m --cpus=1.0 projectkv-model
```
