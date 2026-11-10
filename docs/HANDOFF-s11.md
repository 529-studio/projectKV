# S11 Handoff & Sprint 12 Horizon

**Document**: `docs/HANDOFF-s11.md`  
**Date**: 2026-11-10  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 11 final state, merged deliverables, settled decisions, pitfalls encountered, open backlog, and Sprint 12 tasks.

---

## 1. Branch & Commit Status

- **Active branch**: `dev`
- **Head commit**: `1cec25b` (`Merge pull request #410 from 529-studio/feat/s11-t5-gpu-yolov8n-seg-openvino`)
- **Worktree**: clean, synchronized with `origin/dev`
- **Merged in Sprint 11**:
  - PR #403 (`tooling/s11-t1-evidence-integrity-gate`, commit `4d52e5a`)
  - PR #404 (`ops/s11-t2-unattended-soak-and-proof-ingestion`, commit `b17a159`)
  - PR #405 (`tooling/s11-t3-directional-roadway-registry`, commit `c4da6bb`)
  - PR #407 (`fix/ci-evidence-baseline-expiry`, commit `5cb961a`)
  - PR #408 (`chore/camera-catalog-resync-20261110-31`, commit `7a8c8e2`)
  - PR #409 (`feat/s11-t4-forecast-live-refit-pipeline`, commit `b04f01c`)
  - PR #410 (`feat/s11-t5-gpu-yolov8n-seg-openvino`, commit `1cec25b`)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :--- | :------------------- |
| **#403** | **S11-T1** (#395) | `tools/ci`, CI workflows | **Evidence Integrity CI Gate**: Enforces `fit_source == "live_history"`, non-future soak timestamps ($t_{gen} \ge t_{window.end}$), Shannon entropy $H \ge 3.0$, and baseline ratchet via `.evidence-integrity-baseline.txt`. Shipped L77. |
| **#404** | **S11-T2** (#396) | `tools/harvester`, `go-server` | **14-Day Soak & Proof Ingestion**: 24/7 background harvester daemon in SQLite WAL mode; replayed 11,905 authentic frames into `hotset_verdict_history` (11,962 records total). Shipped L78. |
| **#405** | **S11-T3** (#397) | `data/roadway_regions`, `tools` | **Directional Roadway Registry Scale-Out**: 100% Hot Set camera dual coverage (59 cameras); strict non-overlap ($\text{IoU} < 0.05$); capacity sum normalized to $1.0 \pm 0.01$. Shipped L79. |
| **#407** | Fix | `tools/ci`, workflows | **CI Baseline Ratchet Hardening**: Prohibits clock-dependent baseline entries; supports SUPERSEDED artifact lifecycle; scopes deployment concurrency. |
| **#408** | Chore | `cameras_*.json` | **Automated Camera Health Resync**: Health audit resynchronization of camera catalog metadata. |
| **#409** | **S11-T4** (#398) | `tools/forecast`, `go-server` | **Automated Forecast Refit Pipeline**: Standard-library Ridge pipeline (`tools/forecast/train_live_model.py`); promoted `2026.11.10-live` weights to `prototype_v1.json`; unlocked `/api/v1/cameras/:id/forecast` 200 ok. Shipped L80. |
| **#410** | **S11-T5** (#399) | `tools/train`, `models`, `cv-engine` | **Real YOLOv8n-seg OpenVINO FP16 Model**: Replaced 64 KB stub with authentic 6.52 MB FP16 model (769 layers, entropy 7.4667, 381/417 tensors transplanted); dynamic 416/320 px inference; fail-closed untrained-head guard. Shipped L82. |

---

## 3. Open Issues & Backlog

- **#400 (S11-T6)**: `[frontend] Commuter Last-Mile Geolocation (GPS 'Near-Me' Flood Proximity View)` — deferred to Sprint 12 to prioritize model integrity and CI gates.
- **#401 (S11-T7)**: `[cv-engine + go-server] Motorbike Flood Stall Hazard Level Classifier (cm Depth)` — physical tire submersion geometry; deferred to Sprint 12.
- **#402 (S11-T8)**: `[ops + frontend] Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI` — webhook notifications and citizen modal; deferred to Sprint 12.
- **#411 (S11-T5b)**: `[cv-engine + infra] Off-box GPU Fine-Tuning on 550+ Frames & AVX2 Worker Latency Benchmark` — materializing images into `data/yolo_seg/images/`, fine-tuning on GPU host to reach held-out IoU $\ge 0.612$, and validating AVX2 Linux worker p95 latency $\le 25.0\text{ ms}$.

---

## 4. Settled Architectural Decisions

- **ADR-013 D1 (Evidence Integrity CI Gate)**: Synthetic fixtures and fake future timestamps are strictly forbidden from release qualification (`tools/ci/check_evidence_integrity.py`).
- **ADR-013 D2 (14-Day Soak & Autonomous Harvester)**: Continuous background WAL writes without locking read queries; replay data provides authentic operational history.
- **ADR-013 D3 (100% Directional Roadway Registry)**: Dual directional polygon regions for all 59 Hot Set cameras with zero geometric overlap ($\text{IoU} < 0.05$) and normalized capacity ($1.0 \pm 0.01$).
- **ADR-013 D4 (Deterministic Standard-Library Forecast Refit)**: `make train-forecast` runs pure standard-library Ridge regression with chronological persistence gate and atomic artifact promotion.
- **ADR-013 D5 (Zero-Leak Dataset v2 Splitting)**: Group-aware train/val split strictly by camera ID and date to guarantee 0% spatial or temporal leakage.
- **ADR-013 D6 (Fail-Closed Default Serving)**: Serving endpoints fail closed with explicit error envelopes (`model_unvalidated` 503, `insufficient_history` 422) unless valid authentic models and $\ge 4$ observations exist.

---

## 5. Pitfalls Encountered

- **F4 Root Cause & 64 KB Null-Byte Stub**: The legacy export path caught `ImportError` on `.pt` files and silently fell back to dry-run dummy stub generation (`\x00\x00\x01\x00 * 16384`). Resolved in PR #410 by rebuilding the 6-class graph, transplanting 381 tensors from YOLOv8n-seg, and making all unflagged errors fail closed.
- **Static Shape Trap in OpenVINO CPU Plugin**: Calling `model.reshape([-1, 3, -1, -1])` succeeded on static graphs but failed during execution at 320 px with HTTP 500 (`input shapes dim index: 2 mismatch`). Fixed by exporting with `dynamic=True` and enforcing multi-resolution runtime verification.
- **Concurrent PR Merge Collisions in Documentation**: Concurrent merges across PR #405, #407, and #409 created duplicate lesson numbers and pushed `INDEX.md` over the 20,480 B cap. Resolved by renumbering and establishing strict byte headroom tracking.
- **Silent Dry-Run Downgrade & Ornamental 100% Retraining Gate**: The training orchestrator silently fell back to dry-run when dependencies were missing, reporting `train_completed: True` with exit code 0, and evaluated canned metrics as `PASSED 100%`. Resolved by raising `RuntimeError` and marking unmeasured metrics as `UNVERIFIED`.
- **Background Ingestion Mutating SQLite State During Live Boot**: Harvester startup altered `camera-view.db` SHA-256 hashes during test assertions. Resolved by isolating test databases.

---

## 6. Sprint 12 Horizon: next tasks

1. **S12-T1: Automated Cron / Sidecar Scheduling for `make train-forecast`** — `tools/forecast/train_live_model.py` and systemd/k8s CronJob configuration.
2. **S12-T2: Commuter Last-Mile Geolocation (GPS 'Near-Me' View)** — `apps/web` Leaflet GPS view, 48px touch targets, 1.5 km proximity bottom sheet (Issue #400).
3. **S12-T3: Motorbike Flood Stall Hazard Level Classifier (cm Depth)** — `services/cv-engine` and `services/go-server` tire submersion depth translation into Safe / Warning / Danger hazard levels (Issue #401).
4. **S12-T4: Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI** — `services/go-server/internal/notify` Telegram Bot API webhook and subscription modal (Issue #402).
5. **S12-T5: Off-box GPU YOLOv8n-seg Fine-Tuning & AVX2 Latency Benchmark** — materialize `data/yolo_seg/images/`, fine-tune on GPU, and qualify real AVX2 p95 $\le 25.0\text{ ms}$ (Issue #411).
6. **S12-T6: Rain & Flood Feature Store Ingestion for Short-Horizon Forecasting** — ingest real-time rainfall rate (mm/h) and crowdsourced water depths into Ridge delta features.
7. **S12-T7: Multi-Camera Corridor Congestion Propagation Modeling** — downstream spillback modeling across adjacent arterial corridor cameras.
8. **S12-T8: High-Availability SQLite Streaming Replication (Litestream / R2)** — real-time WAL replication to Cloudflare R2 for zero-RPO disaster recovery.

---

## 7. Open Questions For The Human

- **GPU Compute Allocation for S12-T5 (#411)**: Which environment should be used to run the 50–100 epoch fine-tuning run (Google Colab Pro, vast.ai, or dedicated on-premise GPU workstation)?
- **Telegram Bot Token Provisioning for S12-T4 (#402)**: Is a production BotFather bot token and channel ID ready for live webhook binding, or should local integration use a sandbox bot?
