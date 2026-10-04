# Step-by-Step Guide: Running OpenVINO Locally via Standalone Container (`docker run`)

> **Target Environment**: Local macOS (Apple Silicon / Intel) with **Colima** and **Docker CLI**.  
> **Recommended Method**: Standalone Container via `docker run` for rapid local development and testing without Compose network overhead.

---

## 1. Prerequisites

- **Colima**: Installed via Homebrew (`colima version`)
- **Docker CLI**: Installed (`docker --version`)

If Colima is not running, start it:
```bash
colima start --cpu 2 --memory 4
```

Verify that the Docker engine is responsive:
```bash
docker ps
```

---

## 2. Step 1: Prepare OpenVINO Model Weights

The inference engine expects the OpenVINO IR files in a directory mounted to `/app/models/yolov8n_openvino_320`.

Create a local directory `yolov8n_openvino_model/` in the project root:
```bash
mkdir -p yolov8n_openvino_model
```

Place the two model files into `yolov8n_openvino_model/`:
- `yolov8n.xml` (~200 KB — computational graph)
- `yolov8n.bin` (~6.1 MB — FP16 weights)

*(If obtaining weights from DevOps `@giangdq202` or pre-exported artifact, simply copy them into this folder).*

---

## 3. Step 2: Build the `cv-engine` Docker Image

Build the lightweight container image locally:

```bash
docker build -t projectkv-cv-engine -f services/cv-engine/Dockerfile .
```

---

## 4. Step 3: Run Standalone Container via `docker run` (Recommended for Local Dev)

Launch the container with your local model weights mounted directly in read-only mode:

```bash
docker run --rm -d \
  --name warded-model \
  -p 8000:8000 \
  -v $(pwd)/yolov8n_openvino_model:/app/models/yolov8n_openvino_320:ro \
  -e MODEL_DIR=/app/models/yolov8n_openvino_320 \
  -e PORT=8000 \
  projectkv-cv-engine
```

### Check Startup Logs
```bash
docker logs warded-model
```

Expected log output:
```text
[INFO] OpenVINO Core initialized successfully
[INFO] Successfully loaded and compiled OpenVINO model from /app/models/yolov8n_openvino_320/yolov8n.xml
[INFO] ProjectKV CV Engine running on port 8000...
```

---

## 5. Step 4: Verify Live Snapshot Inference via curl

### 5.1 Health Check
```bash
curl -s http://localhost:8000/healthz | jq .
```

Expected response:
```json
{
  "status": "healthy",
  "service": "cv-engine",
  "openvino_available": true,
  "model_loaded": true
}
```

### 5.2 Test Inference with a Real Camera Frame
Send a test snapshot fixture from the repository:
```bash
curl -i -X POST \
  -H "Content-Type: application/octet-stream" \
  --data-binary @tools/eval/fixtures/frames/cam_5a825b7d5058170011f6ead4_1790858415.jpg \
  http://localhost:8000/predict
```

Expected HTTP 200 response:
```json
{
  "vehicles_detected": 14,
  "vehicle_counts": {
    "motorcycle": 8,
    "car": 4,
    "bus": 1,
    "truck": 1,
    "container": 0
  },
  "traffic_severity": "MODERATE",
  "confidence": 0.88,
  "latency_ms": 42.5,
  "model": "yolov8n_openvino_320"
}
```

---

## 6. Step 5: Connect Local Go Backend (`ANALYZER=onnx`)

Now connect your local Go backend to the running container:

```bash
ANALYZER=onnx \
CV_ENGINE_URL=http://localhost:8000 \
HOTSET_INTERVAL_SECONDS=10 \
HOTSET_FIXED_MAX_CAMERAS=3 \
make dev-go
```

### Verify Output in Browser or Terminal
```bash
curl -s http://localhost:3000/api/traffic-status | jq .verdicts[0]
```
The verdict will display:
- `"source": "onnx"`
- `"severity"`: calculated via the PCU Saturation Index from the per-class counts produced by the container.

---

## 7. Step 6: Teardown & Cleanup

Stop the container when finished:
```bash
docker stop warded-model
```

Stop Colima to free system memory:
```bash
colima stop
```

---

## Reference: Alternative Launch via Docker Compose

If you ever need to run via Docker Compose instead of `docker run`:
```bash
# Using local zero-config compose:
docker compose -f docker-compose.local.yml up -d
docker compose -f docker-compose.local.yml logs -f
docker compose -f docker-compose.local.yml down
```
