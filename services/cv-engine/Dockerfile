FROM python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
 && rm -rf /var/lib/apt/lists/*

COPY services/cv-engine/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

# `server.py` imports its sibling `hazard_classifier` at module load, so both files
# must be in the image; copying only server.py makes the container die on import.
# `calibration.json` (§2.2) ships as the committed empty template so the loader always
# finds a parseable file at its default path; operators override it with a bind mount.
COPY services/cv-engine/server.py services/cv-engine/hazard_classifier.py services/cv-engine/calibration.json /app/

ENV PORT=8000 \
    MODEL_DIR=/app/models/yolov8n_openvino_320 \
    PYTHONUNBUFFERED=1

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8000/healthz || exit 1

ENTRYPOINT ["python3", "/app/server.py"]
