FROM python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY services/cv-engine/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

# ONNX Runtime GPU runtime (CV_BACKEND=ort). `onnxruntime-gpu` is a separate wheel
# from `onnxruntime`; it dlopen()s the CUDA 12 / cuDNN 9 shared libraries when a
# session is created. The nvidia-* wheels below ship exactly those libraries under
# site-packages/nvidia, so the image stays on a slim Python base instead of a
# multi-GB CUDA base image. OpenVINO remains installed alongside it: the default
# backend has to keep working in this same image, and each backend is loaded lazily.
COPY services/cv-engine/requirements-ort.txt /app/requirements-ort.txt
RUN pip install --no-cache-dir -r /app/requirements-ort.txt

# ORT resolves its CUDA dependencies through LD_LIBRARY_PATH. The paths are the
# layout the nvidia-* wheels install (site-packages/nvidia/<lib>/lib); keeping them
# explicit means a missing wheel fails at session creation with a clear dlopen
# error instead of silently falling back without a trace.
ENV NVIDIA_LIBS=/usr/local/lib/python3.11/site-packages/nvidia
ENV LD_LIBRARY_PATH=${NVIDIA_LIBS}/cuda_runtime/lib:${NVIDIA_LIBS}/cublas/lib:${NVIDIA_LIBS}/cudnn/lib:${NVIDIA_LIBS}/cufft/lib:${NVIDIA_LIBS}/curand/lib:${NVIDIA_LIBS}/cuda_nvrtc/lib:${NVIDIA_LIBS}/nvjitlink/lib

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
