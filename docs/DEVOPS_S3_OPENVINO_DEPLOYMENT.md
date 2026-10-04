# DevOps Deployment Runbook: S3 Self-Hosted Vehicle Analyzer (OpenVINO)

This document provides step-by-step instructions for DevOps and infrastructure engineers to deploy, configure, and verify the self-hosted OpenVINO inference service (`warded-model`) alongside the Go backend (`warded-backend`) in Staging and Production environments.

---

## 1. Architectural Context & Prerequisites

### Architecture Transition
- **Previous state**: Backend used Gemini Cloud Multimodal VLM (`ANALYZER=vlm`) for traffic analysis.
- **Sprint 3 (Issue #137 - S3)**: Vehicle congestion detection runs on-premise inside container `warded-model` via Intel OpenVINO CPU runtime (`ANALYZER=onnx`), eliminating external API token costs and rate limits.

### Hardware Baseline
- **Target host**: `local-homesrv` (Intel Celeron N2930 @ 1.83GHz, 4 cores, 8GB RAM, non-AVX, SSE4.2 vector support).
- **Hard Resource Isolation**: `warded-model` is bounded by Docker cgroups to **`cpus: "1.00"`** and **`memory: "512M"`**.
- **Capacity decision**: **Option 1b** is active by default (4 priority corridors = **57 cameras** on a **30-second cycle**, consuming ~25.1s per cycle with **16% headroom**).

> [!CAUTION]
> **Zero Direct Core Starvation**: Do NOT raise `MODEL_CPU_LIMIT` above `1.00` on `local-homesrv`. Finding 2 in ADR-004 confirmed that `local-homesrv` is a Single Point of Failure (SPOF) hosting the reverse proxy (Traefik), Redis, and SQLite database. OpenVINO must remain isolated to at most 1 single core.

---

## 2. Model Preparation & Volume Ingestion

The `warded-model` container requires the OpenVINO IR model files (`.xml` graph and `.bin` weights) mounted at runtime.

### Step 2.1: Model Specification
- **Architecture**: YOLOv8n (nano)
- **Input resolution**: $320 \times 320$ pixels
- **Precision**: FP16 (compressed)
- **Directory name**: `yolov8n_openvino_320/`
- **Required files**:
  - `yolov8n.xml` (~200 KB)
  - `yolov8n.bin` (~6.1 MB)

### Step 2.2: Export Model (if generating from source checkpoint)
If exporting from clean stock weights:
```bash
python3 -m pip install ultralytics openvino
yolo export model=yolov8n.pt format=openvino imgsz=320 half=True
# Output folder: yolov8n_openvino_model/
```

### Step 2.3: Populate Docker Volume on Server Host
Copy the model directory into the Docker volume `warded-stg_warded-model-weights`:

```bash
# 1. Create the named volume if not already existing
docker volume create warded-stg_warded-model-weights

# 2. Transfer model files into the volume via temporary worker container
docker run --rm \
  -v warded-stg_warded-model-weights:/app/models \
  -v $(pwd)/yolov8n_openvino_model:/source:ro \
  alpine sh -c "mkdir -p /app/models/yolov8n_openvino_320 && cp /source/* /app/models/yolov8n_openvino_320/"

# 3. Verify files exist in the volume
docker run --rm -v warded-stg_warded-model-weights:/app/models alpine ls -lh /app/models/yolov8n_openvino_320/
```
Expected output:
```text
-rw-r--r--    1 root     root        6.1M yolov8n.bin
-rw-r--r--    1 root     root      205.1K yolov8n.xml
```

---

## 3. Environment Configuration (`.env`)

Add or update the following environment variables in your server's `.env` deployment file:

```dotenv
# ==============================================================================
# SPRINT 3: OPENVINO SELF-HOSTED VEHICLE ANALYZER
# ==============================================================================
ANALYZER=onnx
CV_ENGINE_URL=http://warded-model:8000

# Option 1b Capacity Tuning (4 Corridors = 57 Cameras @ 30s cycle)
HOTSET_INTERVAL_SECONDS=30
HOTSET_FIXED_MAX_CAMERAS=57
HOTSET_MAX_PARALLEL=10

# Model volume configuration
MODEL_VOLUME_NAME=warded-stg_warded-model-weights
MODEL_DIR=/app/models/yolov8n_openvino_320
MODEL_PORT=8000
```

---

## 4. Deployment Execution

Deploy or reload the stack via `docker-compose.stg.yml`:

```bash
# Pull newest images
docker compose -f docker-compose.stg.yml pull

# Start or recreate services
docker compose -f docker-compose.stg.yml up -d warded-model warded-backend
```

---

## 5. Post-Deployment Verification Checklist

Execute these checks in order to confirm clean operations:

### Check 1: Model Health & Standby Guard
Query the `warded-model` health endpoint directly:
```bash
curl -s http://localhost:8000/healthz | jq
```
**Expected response**:
```json
{
  "status": "healthy",
  "service": "cv-engine",
  "openvino_available": true,
  "model_loaded": true,
  "timestamp": 1728000000
}
```
> [!WARNING]
> If `"model_loaded": false`, the container is in **Standby Mode** because model files were not found in `MODEL_DIR`. `POST /predict` will return **HTTP 503**, and the backend will not record verdicts. Verify volume mounting in Step 2.3.

### Check 2: Single-frame Inference Test
Send a test snapshot to `POST /predict`:
```bash
curl -i -X POST \
  -H "Content-Type: application/octet-stream" \
  --data-binary @tools/eval/fixtures/frames/cam_5a825b7d5058170011f6ead4_1790858415.jpg \
  http://localhost:8000/predict
```
**Expected response**:
- Status: `HTTP/1.1 200 OK`
- Body:
  ```json
  {
    "vehicles_detected": 14,
    "traffic_severity": "MODERATE",
    "confidence": 0.88,
    "latency_ms": 425.8,
    "model": "yolov8n_openvino_320"
  }
  ```

### Check 3: Go Backend Initialization Logs
Inspect the Go server startup log:
```bash
docker logs warded-backend 2>&1 | grep "ANALYZER=onnx"
```
**Expected output**:
```text
[INIT] ANALYZER=onnx active (url=http://warded-model:8000, interval=30s, maxParallel=10, fixedCameras=57). Hot Set scheduler will run self-hosted OpenVINO inference.
```

### Check 4: Verdict Provenance Validation
Query the live traffic status API:
```bash
curl -s http://localhost:3000/api/traffic-status | jq '.verdicts[0]'
```
**Expected attributes**:
- `"source": "onnx"`
- `"severity"` is one of `"CLEAR"`, `"MODERATE"`, `"HEAVY"`, `"CONGESTED"`
- `"observed_at"` timestamp is fresh ($\le 30$ seconds)

---

## 6. Rollback / Emergency Fallback Plan

If `warded-model` crashes, encounters corrupted weights, or host CPU pressure spikes unexpectedly:

1. **Option A: Fall back to Cloud Vision (Gemini VLM)**
   In `.env`:
   ```dotenv
   ANALYZER=vlm
   CLOUD_VISION_API_KEY=<valid_gemini_key>
   ```
   Restart backend:
   ```bash
   docker compose -f docker-compose.stg.yml up -d warded-backend
   ```

2. **Option B: Emergency Disable (Zero-Inference Mode)**
   In `.env`:
   ```dotenv
   ANALYZER=none
   ```
   Restart backend:
   ```bash
   docker compose -f docker-compose.stg.yml up -d warded-backend
   ```
   *Result*: The backend serves camera snapshots and live catalog normally without scheduling any background CV inference.
