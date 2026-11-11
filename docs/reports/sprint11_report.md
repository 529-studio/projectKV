# Sprint 11 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0 (Audited & Reconciled with Git Ledger & Test Evidence)  
**Date:** 2026-11-10  
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents, Developers  
**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/ADR-013-sprint-11-architecture.md`](../ADR-013-sprint-11-architecture.md) (Sprint 11 charter, D1–D6)
- [`docs/reports/sprint10_report.md`](sprint10_report.md) (predecessor record and carry-over findings F1–F6)
- [`docs/reports/seg_eval_v3.md`](seg_eval_v3.md) (authentic OpenVINO FP16 model export, untrained head guard)
- [`docs/reports/seg_export_evidence.json`](seg_export_evidence.json) (model weights Shannon entropy and layer verification)
- [`docs/reports/seg_cv_engine_e2e.json`](seg_cv_engine_e2e.json) (multi-resolution 416/320 px inference telemetry)
- [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons L77–L82)
- GitHub PR ledger: PRs #403, #404, #405, #407, #408, #409, #410; Issues #395–#399, #411

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Wave-by-Wave Audit of Delivered Engineering](#2-wave-by-wave-audit-of-delivered-engineering)
3. [Reality-Check Audit Findings (The Uncomfortable Truths F1–F5)](#3-reality-check-audit-findings-the-uncomfortable-truths-f1f5)
4. [End-User Utility Audit (Saigon Commuter Reality)](#4-end-user-utility-audit-saigon-commuter-reality)
5. [Quantitative Verification Evidence & Test Ledger](#5-quantitative-verification-evidence--test-ledger)
6. [Engineering Lessons Learned (L77–L82)](#6-engineering-lessons-learned-l77l82)
7. [Carry-Over Ledger & Strategic Bridge to Sprint 12](#7-carry-over-ledger--strategic-bridge-to-sprint-12)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 11 executed the architectural mandate of ADR-013: replacing simulated or mock pipelines with authentic empirical verification across data collection, traffic congestion forecasting, and computer vision deployment.

Five primary sprint issues plus two operational hygiene fixes landed across seven merged Pull Requests:

1. **S11-T1 (#395, PR #403)**: Evidence Integrity CI Gate (`tools/ci/check_evidence_integrity.py`), prohibiting synthetic fixtures, enforcing non-future soak timestamps ($t_{gen} \ge t_{window.end}$), and Shannon entropy $H \ge 3.0$.
2. **S11-T2 (#396, PR #404)**: 14-day unattended staging soak and ingestion of 11,905 authentic frames into SQLite WAL mode without query degradation.
3. **S11-T3 (#397, PR #405)**: Directional roadway registry scale-out covering 100% of Hot Set cameras (59 cameras) with strict geometric non-overlap ($\text{IoU} < 0.05$) and capacity sum normalization ($1.0 \pm 0.01$).
4. **Fix (#407)** & **Chore (#408)**: Baseline ratchet expiration hardening, superseded artifact lifecycle support, and camera health audit resynchronization.
5. **S11-T4 (#398, PR #409)**: Automated live history forecast refit pipeline (`tools/forecast/train_live_model.py`), chronological persistence gate, and atomic promotion to `prototype_v1.json`, unlocking `/api/v1/cameras/:id/forecast` 200 OK.
6. **S11-T5 (#399, PR #410)**: Real YOLOv8n-seg OpenVINO FP16 deployment (6.52 MB, entropy 7.4667, 381/417 tensors transplanted), dynamic shape resolution at 416 px and 320 px, and fail-closed untrained-head guardrail.

---

## 2. Wave-by-Wave Audit of Delivered Engineering

### Wave 1: Evidence Integrity & Data Ingestion (S11-T1, S11-T2)
- Replaced synthetic benchmark fixtures with an active CI gate evaluating 36 distinct operational artifacts.
- Prohibited mock data and fabricated timestamps via strict JSON schema validation and Shannon entropy checks.
- Continuous background harvester recorded 11,905 frames into SQLite WAL mode with zero database lock contention.

### Wave 2: Calibration & Model Refit (S11-T3, S11-T4)
- Scaled directional roadway polygon registry to dual coverage across all 59 Hot Set cameras.
- Re-architected traffic forecasting from a mock prototype to an automated Ridge refit pipeline using standard-library linear algebra, evaluated on chronological train/val splits.

### Wave 3: Authentic Neural Network Deployment (S11-T5)
- Purged the legacy 64 KB null-byte stub (`\x00\x00\x01\x00 * 16384`) in `models/yolov8n_seg_openvino_416/yolov8n_seg.bin`.
- Built graph transplantation logic in `tools/train/train_yolov8_seg.py`, adapting the 80-class COCO architecture to the 6-class Saigon traffic schema while satisfying `cv-engine`'s 42-channel output contract.
- Verified live inference at 416 px (22.0 ms warm) and 320 px load shedding (19.5 ms warm) without runtime failures.

---

## 3. Reality-Check Audit Findings (The Uncomfortable Truths F1–F5)

1. **F1: Head Re-Initialization & Zero Detections**:
   Transplanting weights from 80-class COCO to 6-class traffic architecture left 36 head tensors randomly re-initialized. Until real GPU fine-tuning runs on the 550+ frame dataset, the model produces ~0.5 logits and zero vehicle detections. Tracked in follow-up issue #411.
2. **F2: Static Shape CPU Plugin Crash**:
   Exporting without dynamic axes baked static spatial constants into element-wise nodes. While `model.reshape([-1, 3, -1, -1])` succeeded, execution at 320 px triggered HTTP 500 in the OpenVINO CPU plugin. Resolved by exporting with `dynamic=True`.
3. **F3: Silent Dry-Run Downgrades**:
   Prior training scripts caught missing dependencies or constrained memory and degraded silently to dry-run stub creation, exiting with code 0 and claiming success. Resolved by enforcing explicit `--dry-run` requirements and failing closed with `RuntimeError`.
4. **F4: Ornamental Retraining Gates**:
   Evaluating unmeasured canned metrics against baseline thresholds previously printed `PASSED 100%`. Resolved by labeling unmeasured baseline evaluations as `UNVERIFIED` and failing closed unless `--allow-untrained-head` is supplied.
5. **F5: Git-Ignored Model Artifacts in Clean CI**:
   Because compiled model binaries are gitignored, CI runners lacking the artifact volume skip compilation tests. Local pre-push verification and checksum tracking in `seg_export_evidence.json` remain essential.

---

## 4. End-User Utility Audit (Saigon Commuter Reality)

- **Forecast Usability**: Commuters querying `/api/v1/cameras/:id/forecast` now receive real live-history Ridge predictions with calibrated 95% confidence intervals instead of 503 errors.
- **Directional Clarity**: Roadway congestion is now reported per compass direction (e.g. Inbound vs Outbound), preventing false all-clear readings when one direction is locked.
- **Model Stability**: Under heavy monsoon load, the CV engine safely sheds resolution to 320 px without throwing 500 internal server errors, preserving uptime.

---

## 5. Quantitative Verification Evidence & Test Ledger

| Metric / Test | Baseline (S10) | Achieved (S11) | Status |
|---|---:|---:|---|
| OpenVINO `.bin` File Size | 65,536 B (stub) | 6,518,038 B (6.52 MB) | **PASS** |
| Model Weight Shannon Entropy | 0.81 bits/byte | 7.4667 bits/byte | **PASS** |
| Unique Byte Diversity | 2 / 256 | 256 / 256 | **PASS** |
| Graph XML Layers | 3 | 769 | **PASS** |
| Load-Shedding Ingestion (320 px) | HTTP 500 | HTTP 200 (19.5 ms) | **PASS** |
| Evidence Gate Evaluated Artifacts | 0 | 36 artifacts | **PASS** |
| Hot Set Directional Coverage | 24% (14 / 59) | 100% (59 / 59) | **PASS** |
| Directional Polygon Overlap | Unenforced | $\text{IoU} < 0.05$ | **PASS** |
| Rollback Detection Fallback | Manual | Automated & Verified | **PASS** |

---

## 6. Engineering Lessons Learned (L77–L82)

- **L77**: [Evidence Integrity Gate & Synthetic Prohibition](../lessons/L77-evidence-integrity-gate-and-synthetic-prohibition.md)
- **L78**: [Unattended Staging Soak & Proof Archive Ingestion](../lessons/L78-unattended-soak-and-proof-archive-ingestion.md)
- **L79**: [Directional Roadway Registry Scale-Out & Geometry Invariants](../lessons/L79-directional-roadway-registry-scaleout.md)
- **L80**: [Automated Forecast Refit & Live History Deployment](../lessons/L80-automated-forecast-refit-and-live-deployment.md)
- **L81**: [Temporal Baseline Ratchet Expiry & Manual Approval Gate Concurrency](../lessons/L81-temporal-baseline-ratchet-and-approval-gate-concurrency.md)
- **L82**: [Authentic Model Artifacts & Untrained Heads](../lessons/L82-authentic-model-artifacts-and-untrained-head-guard.md)

---

## 7. Carry-Over Ledger & Strategic Bridge to Sprint 12

The remaining deferred work transitions into Sprint 12:

1. **#400 (S11-T6 $\rightarrow$ S12-T2)**: Commuter Last-Mile Geolocation (`apps/web` Leaflet GPS view, 48px touch ergonomics).
2. **#401 (S11-T7 $\rightarrow$ S12-T3)**: Motorbike Flood Stall Depth Classifier (physical tire submersion geometry to cm depth).
3. **#402 (S11-T8 $\rightarrow$ S12-T4)**: Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI.
4. **#411 (S11-T5b $\rightarrow$ S12-T5)**: Off-box GPU fine-tuning on 550+ frames to train the 36 head tensors and benchmark real AVX2 p95 latency.
5. **S12-T1**: Automated Cron / Sidecar Scheduling for `make train-forecast`.
