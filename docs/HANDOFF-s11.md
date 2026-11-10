# S11 Handoff & Sprint 12 Horizon

**Document**: `docs/HANDOFF-s11.md`  
**Date**: 2026-11-10  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 11 mid-flight state, merged deliverables, active issues, architectural invariants, pitfalls encountered, and next tasks for Sprint 12.

---

## 1. Branch & Commit Status

- **Active Tracking Branch**: `dev`
- **Head Commit on `dev`**: `b04f01c` (`Merge pull request #409 from 529-studio/feat/s11-t4-forecast-live-refit-pipeline`)
- **Local Worktree Status**: Clean, checked out on `dev` synchronized with `origin/dev`.
- **Merged Feature Branches**:
  - `tooling/s11-t1-evidence-integrity-gate` (PR #403)
  - `ops/s11-t2-unattended-soak-and-proof-ingestion` (PR #404)
  - `tooling/s11-t3-directional-roadway-registry` (PR #405)
  - `fix/ci-evidence-baseline-expiry` (PR #407)
  - `chore/camera-catalog-resync-20261110-31` (PR #408)
  - `feat/s11-t4-forecast-live-refit-pipeline` (PR #409)
- **Known Post-Merge Housekeeping Note**:
  - Concurrent merging of PR #405, #407, and #409 introduced duplicate row numbering in `docs/lessons/INDEX.md` (duplicate entries for directional scale-out, filename mismatch on L79/L80), temporarily raising `INDEX.md` to 20,626 B (> 20,480 B cap). A small housekeeping commit on `dev` will normalize lesson IDs and restore byte cap compliance before dispatching S11-T5.

---

## 2. Merged Deliverables (Sprint 11 Progress)

| PR | Issue / Task | Subsystems | Delivered Capabilities |
|---|---|---|---|
| **#403** | **S11-T1** (#395) | `tools/ci`, GitHub Actions | **Evidence Integrity CI Gate & Synthetic Prohibition**: Enforces `fit_source == "live_history"`, non-future soak timestamps ($t_{gen} \ge t_{window.end}$), model weight Shannon entropy $H \ge 3.0$, and ratchet enforcement via `.evidence-integrity-baseline.txt`. Shipped L77. |
| **#404** | **S11-T2** (#396) | `tools/harvester`, `go-server` | **14-Day Unattended Soak & 11,905 Proof Frame Ingestion**: 24/7 background harvester daemon running SQLite WAL mode; replayed 11,905 authentic frames into `hotset_verdict_history` (11,962 total records) and weather observations. Shipped L78. |
| **#405** | **S11-T3** (#397) | `data/roadway_regions`, `tools` | **Directional Roadway Registry Scale-Out**: 100% Hot Set camera dual coverage (59 cameras); strict geometric non-overlap ($\text{IoU} < 0.05$); capacity sum normalized to $1.0 \pm 0.01$; Go backend integration. Shipped L79. |
| **#407** | Fix | `tools/ci`, workflows | **CI Baseline Ratchet Hardening**: Prohibits clock-dependent baseline entries; supports SUPERSEDED artifact lifecycle; scopes deployment concurrency. |
| **#408** | Chore | `cameras_*.json` | **Automated Camera Health Resync**: Health audit resynchronization of camera catalog metadata. |
| **#409** | **S11-T4** (#398) | `tools/forecast`, `go-server` | **Automated Forecast Refit Pipeline & Live Model Deployment**: Standard-library Ridge pipeline (`tools/forecast/train_live_model.py`); chronological 80/20 persistence gate; promoted `2026.11.10-live` weights to `prototype_v1.json`; unlocked `GET /api/v1/cameras/:id/forecast` with 200 ok and 95% CIs. Shipped L80. |

---

## 3. Open Issues & Active Sprint 11 Backlog

All Pull Requests up to #409 are merged. The remaining open backlog consists of 4 sprint issues:

1. **Issue #399 (S11-T5, Wave 2)**: `[cv-engine + tooling] S11-T5: Real GPU YOLOv8n-seg Training & 6.4 MB OpenVINO Deployment`
   - *Effort Estimate*: 56–72 hours (Expected window: Day 15–24)
   - *Target*: Replace 64 KB null-byte stub in `models/yolov8n_seg_openvino_416/yolov8n_seg.bin` with authentic 6.4 MB OpenVINO FP16 weights trained on 550+ frame dataset v2, clearing `water_puddle` $\text{IoU} \ge 0.612$, $\text{Mask mAP}_{50\text{--}95} \ge 0.496$, and latency $\le 25.0\text{ ms}$.
2. **Issue #400 (S11-T6, Wave 3)**: `[frontend] S11-T6: Commuter Last-Mile Geolocation (GPS 'Near-Me' Flood Proximity View)`
   - *Effort Estimate*: 32–44 hours (Expected window: Day 20–26)
   - *Target*: 1-click GPS near-me view in `apps/web` (Leaflet, Geo API, 48px touch targets, bottom sheet within 1.5 km).
3. **Issue #401 (S11-T7, Wave 3)**: `[cv-engine + go-server] S11-T7: Motorbike Flood Stall Hazard Level Classifier (cm Depth)`
   - *Effort Estimate*: 48–60 hours (Expected window: Day 22–28)
   - *Target*: Physical motorbike tire submersion geometry to estimate flood depth in centimeters and categorize risk levels (Safe $< 10\text{ cm}$, Warning $10\text{--}20\text{ cm}$, Danger $> 20\text{ cm}$).
4. **Issue #402 (S11-T8, Wave 4)**: `[ops + frontend] S11-T8: Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI`
   - *Effort Estimate*: 40–50 hours (Expected window: Day 25–30)
   - *Target*: Telegram Bot API webhook, live corridor alerts, and subscription management UI.

---

## 4. Settled Architectural Decisions

- **ADR-013 D1 (Evidence Integrity CI Gate)**: Synthetic fixtures and fake future timestamps are strictly prohibited from release qualification. Every evidence artifact must have authentic provenance (`live_history`) and non-future soak timestamps ($t_{gen} \ge t_{window.end}$).
- **ADR-013 D2 (14-Day Soak & Autonomous Harvester)**: Background ingestion runs continuous WAL writes without locking read queries; replay data provides authentic operational history.
- **ADR-013 D3 (100% Directional Roadway Registry)**: Every Hot Set camera has distinct polygon regions for direction A and direction B with zero geometric overlap ($\text{IoU} < 0.05$) and normalized capacity sum ($1.0 \pm 0.01$).
- **ADR-013 D4 (Deterministic Standard-Library Forecast Refit)**: Invocable via `make train-forecast`, solving Ridge regression with standard-library Gaussian elimination, chronological time-series splitting, and atomic artifact promotion. Embedded artifact in Go leaf modulith runs natively without synthetic dev hatches.
- **ADR-013 D5 (Zero-Leak Dataset v2 Splitting)**: Group-aware train/val split strictly by camera ID and date to guarantee 0% temporal or spatial leakage.
- **ADR-013 D6 (Fail-Closed Default Serving)**: Serving endpoints fail closed with explicit error envelopes (`model_unvalidated` 503, `insufficient_history` 422) unless valid authentic models and $\ge 4$ observations exist.

---

## 5. Pitfalls Encountered & Resolved ("Bẫy đã gặp")

1. **Concurrent PR Merge Collision in `docs/lessons/INDEX.md`**:
   - *Root Cause*: Concurrent merging of PR #405, #407, and #409 created duplicate lesson numbers (two L80s), mislinked filenames, and pushed `INDEX.md` from 20,113 B to 20,626 B, breaching the 20,480 B cap.
   - *Takeaway*: When multiple subagents or engineers merge PRs concurrently, lesson numbers must be coordinated, or a post-merge synchronization pass is required before next work.
2. **Fabricated Metrics in Contract Documentation (Rule 6 Breach in Round 2)**:
   - *Root Cause*: Truncated mock figures (`0.06013 / 0.07682`) were copied into `s10_t1_forecast_contract_spec.md` instead of the actual metrics from `forecast_prototype_v1.json`. OpenCode adversarial audit immediately caught this as an X1 blocking defect.
   - *Takeaway*: Replaced all cells with verified 5-decimal rounded figures directly computed from the evaluation report. Living documentation must always reflect empirical truth.
3. **Stale Contract Invariants Surviving Architectural Upgrades**:
   - *Root Cause*: When upgrading the embedded model from synthetic prototype to live history, contract documentation still referenced `EmbeddedSyntheticArtifact()`, default 503 behavior, and S10 dev hatches.
   - *Takeaway*: Audited and updated §3.6, §6.3, §7.2, and scenario G3 to reflect `DefaultArtifact()` and default 200 ok boot.
4. **Keyword Denylists vs. Operational Source Allowlists (M1)**:
   - *Root Cause*: Initial provenance guard relied on a denylist of forbidden words (`synthetic`, `mock`). A database with `source='unit_test'` passed through.
   - *Takeaway*: Added `AUTHENTIC_OPERATIONAL_SOURCES = {"proof_archive", "harvester_daemon", "edge_worker", "inference_worker"}` and recorded `source_counts` in the artifact schema.
5. **Partial Horizon Promotion Leading to Silent Fallbacks (RB1)**:
   - *Root Cause*: If a model refit only included horizon 10, the Go server would boot but silently serve zero-weight persistence for horizons 20 and 30.
   - *Takeaway*: Enforced full coverage of all supported horizons `(10, 20, 30)` in both Python pre-promotion validation and Go `artifact.Validate()`.
6. **Background Ingestion Mutating SQLite State During Live Boot**:
   - *Root Cause*: Running `cmd/server` during testing booted the active harvester, which appended new observation rows to `camera-view.db`, altering its SHA-256 hash.
   - *Takeaway*: Pipeline must test against isolated read-only copies or take snapshot hashes prior to live server execution.

---

## 6. Sprint 12 Horizon: 5–10 Next Tasks

Sprint 12 will build upon Sprint 11's core foundations (live forecast refit, directional roadway calibration, 6.4 MB segmentation, and citizen notifications). The 8 planned tasks for Sprint 12 are:

1. **S12-T1: Automated Cron / Sidecar Scheduling for `make train-forecast`**:
   - Wire the S11-T4 refit pipeline into an autonomous nocturnal timer (systemd timer or k8s CronJob) with telemetry alert on gate failure.
2. **S12-T2: Rain & Flood Feature Store Ingestion for Short-Horizon Forecasting**:
   - Extend `train_live_model.py` and `service.go` to ingest real-time rainfall rate (mm/h) and flood sensor/crowdsourced water depth signals into Ridge delta features.
3. **S12-T3: Multi-Camera Corridor Propagation Modeling**:
   - Model downstream congestion spillback across adjacent cameras along major arterial corridors (e.g., Võ Văn Kiệt, Điện Biên Phủ).
4. **S12-T4: Motorbike Wheel Depth Calibrated Optical Model**:
   - Upgrade S11-T7 flood depth estimation using perspective transform calibration per camera geometry to achieve $\pm 2\text{ cm}$ precision.
5. **S12-T5: Commuter Push Notifications via Web Push / PWA**:
   - Enable browser-native Web Push notifications for citizen commuters subscribed to specific corridors without requiring Telegram.
6. **S12-T6: Edge Inference Worker Hardware Acceleration (NPU/iGPU)**:
   - Benchmark and optimize YOLOv8n-seg inference on Intel OpenVINO NPU / iGPU targets to reduce worker CPU load below 20%.
7. **S12-T7: Crowdsourced Verification Feedback Loop with Proof Snapshots**:
   - Allow commuters to validate or dispute congestion/flood warnings by submitting 1-click reports with geotagged timestamps.
8. **S12-T8: High-Availability Failover & Redundant SQLite Replication (Litestream / LiteFS)**:
   - Implement real-time asynchronous streaming replication of SQLite WAL to Cloudflare R2 for zero-RPO disaster recovery.
