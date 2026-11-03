# L64 — First Segmentation Cycle: OpenVINO FP16 Deployment and mAP Gating

## Context

During heavy rainfall in Ho Chi Minh City, flood verification previously depended entirely on the Gemini multimodal cloud API. Under rapid consecutive rain events, Gemini's quota limits (10 RPM / 300 RPD) triggered HTTP 429 rate limits, causing chokepoint environmental verification to abort.

Sprint 9 authorized training and deploying the first self-hosted `yolov8n_seg` model on inference worker hardware under ADR-011 D4. Prior to S9-T4, `yolov8n_seg` had never been fine-tuned on real CCTV footage.

## Root cause

Manual polygon labeling for 500+ frames is operationally prohibitive (10–15 minutes per image). Without automated SAM pre-labeling (S9-T2), segmentation datasets cannot reach critical mass. Furthermore, training on unannotated classification frames or fabricated synthetic geometry (ADR-011 D2) pollutes loss functions and produces degenerate masks.

## Guardrail

- **Strict Dataset Annotation Purity**: `tools/eval/export_yolo_seg.py` strictly skips classification-only frames and enforces verified polygon geometry contracts for all training splits. Fabricated metadata-derived geometry is restricted to smoke testing via `--allow-synthetic`.
- **Quantitative Deployment Gate**: A newly trained segmentation model must beat the null baseline (`water_puddle` IoU > 0.000) on a held-out gold split before deployment. If the gate fails, iterate on dataset curation rather than deploying noisy weights.
- **Microservice Model Precedence with Single-Command Rollback**: `services/cv-engine/server.py` probes for `yolov8n_seg.xml` before `yolov8n.xml`. Rollback requires only renaming the segmentation XML and restarting the container to restore legacy vehicle detection without downtime or code changes.
