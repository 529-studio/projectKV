# CUDA 12.4 / cuDNN 9 runtime base. The inference host runs driver 550, which tops out
# at CUDA 12.4; a newer CUDA user-space cannot execute there. This exact base is the
# one verified with onnxruntime-gpu 1.19.2 on the Pascal P106-100 (sm_6.1), because the
# pip `nvidia-*-cu12` wheels resolved to CUDA 12.8 and broke cuDNN convolution.
# OpenVINO stays installed alongside it: `CV_BACKEND=openvino` remains the default and
# each backend is loaded lazily.
FROM nvidia/cuda:12.4.1-cudnn-runtime-ubuntu22.04

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    wget \
    ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY services/cv-engine/requirements.txt /app/requirements.txt
RUN pip3 install --no-cache-dir -r /app/requirements.txt

COPY services/cv-engine/requirements-ort.txt /app/requirements-ort.txt
RUN pip3 install --no-cache-dir -r /app/requirements-ort.txt

# `server.py` imports its sibling `hazard_classifier` at module load, so both files
# must be in the image; copying only server.py makes the container die on import.
# `calibration.json` (§2.2) ships as the committed empty template so the loader always
# finds a parseable file at its default path; operators override it with a bind mount.
COPY services/cv-engine/server.py services/cv-engine/hazard_classifier.py services/cv-engine/calibration.json /app/

ENV PORT=8000 \
    MODEL_DIR=/app/models/yolov8n_openvino_320 \
    CV_BACKEND=openvino \
    PYTHONUNBUFFERED=1

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8000/healthz || exit 1

ENTRYPOINT ["python3", "/app/server.py"]
