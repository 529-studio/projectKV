FROM python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY services/cv-engine/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

COPY services/cv-engine/server.py /app/server.py

ENV PORT=8000 \
    MODEL_DIR=/app/models/yolov8n_openvino_320 \
    PYTHONUNBUFFERED=1

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8000/healthz || exit 1

ENTRYPOINT ["python3", "/app/server.py"]
