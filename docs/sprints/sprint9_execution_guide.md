# Sprint 9 — Wave-by-Wave agy CLI Execution Guide

> **Status**: All 7 tickets have official issues on GitHub
> ([#297](https://github.com/529-studio/projectKV/issues/297),
> [#298](https://github.com/529-studio/projectKV/issues/298),
> [#299](https://github.com/529-studio/projectKV/issues/299),
> [#300](https://github.com/529-studio/projectKV/issues/300),
> [#301](https://github.com/529-studio/projectKV/issues/301),
> [#302](https://github.com/529-studio/projectKV/issues/302),
> [#309](https://github.com/529-studio/projectKV/issues/309)),
> aligned with [ADR-011](../ADR-011-sprint8-audit-and-sprint9-plan.md) (D1-D8) and audit findings in
> [Sprint 8 Report](../sprint8_report.md). Document is ready for operations.
> Issue #309 (S9-T7) added 2026-10-25: PolygonCanvas E2E review revealed that
> directional taxonomy `dir_a`/`dir_b` only existed in classification labels while the model counted vehicles frame-wide —
> one-way congestion ("one-side jam") could not be isolated; `roadway` polygon with `direction` attribute
> unlocks per-direction vehicle counting as input for S9-T6.
> Track progress on GitHub Projects v2 board **ProjectKV** (ADR-011 D6; Plane Hub decommissioned).

Operational guide for AI agents (`agy` CLI) and developers executing the entire Sprint 9 Backlog per [ADR-011](../ADR-011-sprint8-audit-and-sprint9-plan.md) (Ground-Truth Segmentation Flywheel & Live Model Retraining), [GO_BACKEND_ARCHITECTURE.md](../GO_BACKEND_ARCHITECTURE.md), and [AGENTS.md](../../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Sprint 9 Epic List

| Epic | Epic Name | Child Issues | Primary Objective |
|---|---|---|---|
| **epic:eval-tooling** | `[Epic] Ground-Truth Segmentation Flywheel` | S9-T1, S9-T2, S9-T4, S9-T7 | Polygon annotation contract + canvas editor, SAM-assisted pre-labeling on inference worker, first segmentation training with mAP gate and weights deployment; directional roadway polygons (`direction`) as baseline for per-direction congestion measurement |
| **epic:infra-resilience** | `[Epic] Autonomous Staging & Alert Hygiene` | S9-T3, S9-T5 | Autonomous harvester running on scheduled hours on staging + terminal-free observability; fix Telegram script parity bug |
| **(stretch)** | `[Epic] Forecast Foundation` (stretch) | S9-T6 (consumes S9-T7 output) | 10-30 min traffic congestion forecast prototype from PCU time-series (direction-separated once S9-T7 completes), beating persistence baseline |

---

### Execution Wave Distribution Table

| Wave | ID | GitHub Issue | Title | Epic | Labels | Estimate (Rule 3) | Dependencies |
|:---:|:---:|:---:|---|---|---|:---:|:---:|
| **0** | **S9-T1** | [#297](https://github.com/529-studio/projectKV/issues/297) | `[go-server + web + tooling] S9-T1: Segmentation Polygon Annotation Contract & Canvas Editor in /labeler` | epic:eval-tooling | `backend, frontend, cross-cutting` | 16-20h (3d) | Start immediately (backend contract in progress on branch `feat/polygon-labeler-annotation-contract`) |
| **1** | **S9-T2** | [#298](https://github.com/529-studio/projectKV/issues/298) | `[tooling] S9-T2: SAM-Assisted Polygon Pre-Labeling Batch Toolchain (inference worker offline)` | epic:eval-tooling | `tooling` | 8-10h (1.5d) | S9-T1 |
| **1** | **S9-T3** | [#299](https://github.com/529-studio/projectKV/issues/299) | `[ops + go-server] S9-T3: Staging Autonomous Data Flywheel — Scheduled Harvester, Auto-Label & Status Observability` | epic:infra-resilience | `tooling, backend, cross-cutting` | 6-8h (1.5d) | Start immediately (runs parallel to Wave 0) |
| **2** | **S9-T4** | [#300](https://github.com/529-studio/projectKV/issues/300) | `[tooling + cv-engine] S9-T4: First Off-Box YOLOv8-Seg Training, mAP Benchmark Gate & Staging Weights Deployment` | epic:eval-tooling | `tooling, cross-cutting` | 10-12h (2d) | S9-T1, S9-T2, S9-T3 |
| **3** | **S9-T5** | [#301](https://github.com/529-studio/projectKV/issues/301) | `[go-server + ops] S9-T5: Telegram Verification Script Single-Source Parity Rework (notify-preview)` | epic:infra-resilience | `backend, tooling` | 4h (0.5d) | **Completed (PR #327)**: `internal/notify.BuildFloodAlert` + `cmd/notify-preview` CLI + script Check 3 single-source parity |
| **3** | **S9-T7** | [#309](https://github.com/529-studio/projectKV/issues/309) | `[go-server + web + tooling] S9-T7: Directional Roadway Polygons (direction attribute) for Per-Direction Congestion Ground Truth` | epic:eval-tooling | `backend, frontend, cross-cutting` | 6-8h (1d) | S9-T1 (merged); should complete before S9-T6 |
| **3** | **S9-T6** | [#302](https://github.com/529-studio/projectKV/issues/302) | `[go-server] S9-T6: Short-Horizon Congestion Forecast Prototype (PCU Time-Series, 10-30 min) — stretch` | (stretch) | `backend, enhancement` | 12h (2d) | Start only when Wave 0-2 unblocked; directional PCU time-series if S9-T7 completed |

---

## 2. Environment Preparation

Before executing any ticket, sync the latest `dev` branch:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

Hardware facts to remember (ADR-011 §1.6, Outline `[Arch]` docs):
- **inference worker** (Ryzen 7 6800HS, AVX2): all batch inference (SAM pre-label, benchmark) runs here, off-peak hours.
- **edge host** (Celeron N2930, non-AVX): ingress + fallback only; forbidden from running prebuilt PyTorch/ONNX and SAM.

---

## Wave 0 — Critical Path: Polygon Contract & Canvas Editor (S9-T1)

### Terminal 1 — S9-T1 (Polygon Contract + Canvas Editor in /labeler — Closes Issue #297)

```bash
cd ~/repos/projectKV

agy "Implement issue #297: [go-server + web + tooling] S9-T1: Segmentation Polygon Annotation Contract & Canvas Editor in /labeler.

Read the full issue at https://github.com/529-studio/projectKV/issues/297 for complete Technical Details and Definition of Done.

Key requirements:
1. Backend (internal/archiver): PolygonAnnotation{class, polygon normalized [0,1], source} on LabelPayload;
   SaveLabel canonicalizes alias classes and validates (6-class taxonomy, 3-64 vertices, <=32 annotations,
   reject pixel coordinates). Note: an uncommitted draft exists on branch feat/polygon-labeler-annotation-contract —
   rebase onto it, do not duplicate.
2. tools/eval/export_yolo_seg.py: synthesis becomes opt-in via --allow-synthetic; default skips labels without
   explicit annotations and reports skipped_no_annotations (Lesson 39 extended to geometry).
3. internal/harvester/schema.go + tools/eval/label_schema.json: optional annotations property.
4. apps/web: PolygonCanvas.tsx (SVG overlay, zero new dependencies; click-add vertices, close, drag, delete;
   6-class selector, monochromatic tones; normalized coords); labeler/page.tsx segmentation mode toggle +
   merge annotations into POST payload + provenance badge [Tu dong: SAM]; labelerUtils polygon helpers + tests.
5. Run verification:
   - make test-go
   - cd apps/web && npm test && npm run build
   - python3 -m unittest discover -s tools/eval -p '*_test.py'
   - make swagger
   - Browser E2E: draw a real polygon on a local snapshot -> save -> GET labels shows geometry ->
     export produces YOLO txt with the real polygon and zero synthesized masks (screenshots as evidence).

Branch: feat/s9-t1-polygon-annotation-contract (continue from feat/polygon-labeler-annotation-contract if unmerged)
PR Title: feat(labeler): segmentation polygon annotation contract and canvas editor
PR Description: Must strictly start with 'Close #297' per AGENTS.md Rule 9 (GitHub-native linking; KV format retired by ADR-011 D6). Do NOT merge."
```

---

## Wave 1 — Data Acceleration & Autonomous Flywheel (S9-T2, S9-T3)

Two tickets run in parallel: T2 depends on T1, T3 is independent (ops).

### Terminal 1 — S9-T2 (Offline SAM-assisted pre-labeling on inference worker)

```bash
cd ~/repos/projectKV

agy "Implement issue #298: [tooling] S9-T2: SAM-Assisted Polygon Pre-Labeling Batch Toolchain (inference worker offline).

Read the full issue at https://github.com/529-studio/projectKV/issues/298 for complete Technical Details and Definition of Done.

Key requirements:
1. tools/seg/prelabel_sam.py following the auto_harvest.py / batch_auto_label.py pattern: stream R2 frames via boto3,
   run EfficientViT-SAM/MobileSAM class model (ONNX/OpenVINO; weights path from environment per Rule 8),
   emit candidate labels with annotations (ADR-011 D1 contract) and annotator=sam_prelabel to R2.
2. Roadway proposed via largest stable central region; water_puddle proposal-only; vehicles via existing detector bboxes.
3. Guardrails: token bucket, exponential backoff, --dry-run, --limit, --date; host guard refusing non-AVX machines.
4. Runbook docs/runbooks/sam_prelabeling.md with measured throughput on inference worker (>= 50 real frames).
5. Run verification: python3 -m unittest discover -s tools/seg -p '*_test.py'

Branch: feat/s9-t2-sam-prelabel-toolchain
PR Title: feat(tooling): SAM-assisted polygon pre-labeling batch toolchain
PR Description: Must strictly start with 'Close #298'. Do NOT merge."
```

### Terminal 2 — S9-T3 (Autonomous harvester on staging + observability)

```bash
cd ~/repos/projectKV

agy "Implement issue #299: [ops + go-server] S9-T3: Staging Autonomous Data Flywheel — Scheduled Harvester, Auto-Label & Status Observability.

Read the full issue at https://github.com/529-studio/projectKV/issues/299 for complete Technical Details and Definition of Done.

Key requirements:
1. Baseline first: run make dev-all locally, capture harvester behavior from the Go backend (localhost:3000) and
   web app (localhost:3001) logs BEFORE touching staging.
2. Go backend: add GET /api/harvester/status (behind dev auth): last/next slot, cameras harvested, frames/labels
   uploaded, recent errors. Unit tested.
3. Portainer Stack 14: HARVESTER_ENABLED=true, HARVESTER_SCHEDULE_HOURS=7,12,17, HARVESTER_CONCURRENCY=15;
   confirm CV_ENGINE_URL routing to inference worker engine with internal fallback.
4. Verify one full day: 3 scheduled slots in logs + R2 object counts per slot; one rain-trigger path with
   60-minute cooldown respected; docker stats evidence of no regression on edge host.
5. Runbook docs/runbooks/staging_harvester_flywheel.md incl. rollback (HARVESTER_ENABLED=false).
6. Run verification: make test-go

Branch: ops/s9-t3-staging-data-flywheel
PR Title: ops(harvester): autonomous staging data flywheel enablement and status observability
PR Description: Must strictly start with 'Close #299'. Do NOT merge."
```

---

## Wave 2 — First Training & Deploy (S9-T4)

After Wave 0 + 1 merge and staging flywheel is running (real mask data available).

### Terminal 1 — S9-T4 (Train off-box, mAP gate, deploy weights)

```bash
cd ~/repos/projectKV

agy "Implement issue #300: [tooling + cv-engine] S9-T4: First Off-Box YOLOv8-Seg Training, mAP Benchmark Gate & Staging Weights Deployment.

Read the full issue at https://github.com/529-studio/projectKV/issues/300 for complete Technical Details and Definition of Done.

Key requirements:
1. Dataset v1 via export_yolo_seg.py (post S9-T1): >= 500 masked frames target, stratified rain/dry + day/night;
   commit manifest only (images stay on R2/local).
2. Off-box training (Colab/GPU) via tools/train/train_yolov8_seg.py; document hyperparameters and checkpoint lineage.
3. Evaluation: mask mAP50-95 + per-class IoU on held-out gold split; report docs/reports/seg_eval_v1.md.
   Deployment gate: water_puddle IoU must beat null baseline.
4. Latency p95 at 416px on inference worker vs 30s/57-camera budget (fall back to 320px if exceeded).
5. Deploy yolov8n_seg weights into inference worker model volume per DEVOPS_S3_OPENVINO_DEPLOYMENT.md; verify /healthz
   model_loaded, /predict segmentation fields, POST /api/flood/verify segmentation-sourced verdict; rollback documented.
6. Run verification: make test-model; python3 -m unittest discover -s tools/eval -p '*_test.py'

Branch: feat/s9-t4-first-seg-training-and-deploy
PR Title: feat(cv-engine): first segmentation training cycle, mAP benchmark gate and weights deployment
PR Description: Must strictly start with 'Close #300'. Do NOT merge."
```

---

## Wave 3 — Debt Cleanup, Directional Ground Truth & Stretch (S9-T5, S9-T7, S9-T6)

### Terminal 1 — S9-T5 (Fix Telegram script parity via notify-preview)

```bash
cd ~/repos/projectKV

agy "Implement issue #301: [go-server + ops] S9-T5: Telegram Verification Script Single-Source Parity Rework (notify-preview).

Read the full issue at https://github.com/529-studio/projectKV/issues/301 for complete Technical Details and Definition of Done.

Key requirements:
1. Extract the dispatchFloodAlert mapping precedence (DisplayName->Title->CameraID; district=cam.Zone; ward=cam.PhuongName;
   chokepoint depth) into an exported pure helper in internal/notify (leaf-safe, primitives only) + unit tests;
   cmd/server/main.go consumes it (behavior unchanged).
2. New CLI services/go-server/cmd/notify-preview: -camera-id/-severity/-observed-at; loads camera catalog
   (config.ResolvePath, CAMERAS_FILE_PATH) + chokepoints fixture; prints FormatTelegramMessage output; fail-fast on unknown camera.
3. scripts/verify_staging_telegram.sh Check 3 generates the message via go run ./services/go-server/cmd/notify-preview
   (fail fast when Go toolchain missing); keep zero-emoji/bilingual assertions.
4. Update TestFormatTelegramMessage_StagingChokepointFixture to true production values
   (Area: Thạnh Mỹ Tây, Khu vực Nội thành TP.HCM); correct the runbook wording and Go prerequisite.
5. Run verification: make test-go; script 5/5 against a local mock Bot API.

Branch: fix/s9-t5-notify-preview-parity
PR Title: fix(notify): single-source staging verification message via notify-preview
PR Description: Must strictly start with 'Close #301'. Do NOT merge."
```

### Terminal 2 — S9-T7 (Directional roadway polygons — direction attribute)

```bash
cd ~/repos/projectKV

agy "Implement issue #309: [go-server + web + tooling] S9-T7: Directional Roadway Polygons (direction attribute) for Per-Direction Congestion Ground Truth.

Read the full issue at https://github.com/529-studio/projectKV/issues/309 for complete Technical Details and Definition of Done.

Key requirements:
1. Backend (internal/archiver): PolygonAnnotation gains optional Direction string; canonical values a/b with
   alias canonicalization (dir_a/traffic_dir_a/near -> a; dir_b/traffic_dir_b/far -> b); direction valid only
   when class == roadway; absent/empty passes (backward compatible); handler tests under -race; make swagger.
2. Schema: internal/harvester Draft-07 + tools/eval/label_schema.json optional direction (enum a/b, roadway only).
3. Web: when active class is roadway, show optional direction selector (none / Direction A - near lane /
   Direction B - far lane; i18n vi/en); direction suffix badge on polygon chips; labelerUtils validation
   mirrors the backend rule; unit tests.
4. Exporter (tools/eval/export_yolo_seg.py): YOLO txt unchanged (roadway stays one class id); additionally emit
   roadway_regions.json sidecar per split (normalized coords + direction + label id); python tests; zero
   behavior change when direction absent.
5. Run verification:
   - make test-go
   - cd apps/web && npm test && npm run build
   - python3 -m unittest discover -s tools/eval -p '*_test.py'
   - make swagger
   - Browser E2E: draw two roadway polygons (A + B) on a local snapshot -> save -> reload shows both with
     direction badges -> export sidecar contains both regions (screenshots as evidence).

Branch: feat/s9-t7-directional-roadway-polygons
PR Title: feat(labeler): directional roadway polygon semantics for per-direction congestion ground truth
PR Description: Must strictly start with 'Close #309'. Do NOT merge."
```

### Terminal 3 — S9-T6 (Forecast prototype — stretch, run only when Wave 0-2 complete)

```bash
cd ~/repos/projectKV

agy "Implement issue #302: [go-server] S9-T6: Short-Horizon Congestion Forecast Prototype (PCU Time-Series, 10-30 min) — stretch.

Read the full issue at https://github.com/529-studio/projectKV/issues/302 for complete Technical Details and Definition of Done.

Key requirements:
1. Feature store from SQLite hotset verdict history (PCU/SI per camera, 5-10 min bins); documented schema.
2. Backtest harness: horizons 10/20/30 min; baselines persistence + historical median; candidate = CPU-light
   gradient boosting / regularized regression; report docs/reports/forecast_prototype_v1.md.
3. GET /api/v1/cameras/hotset/forecast?horizon_minutes=30 with staleness guard (no fabricated values on stale data).
4. No UI exposure until the model beats persistence on >= 1 horizon.
5. Run verification: make test-go

Branch: feat/s9-t6-congestion-forecast-prototype
PR Title: feat(hotset): short-horizon congestion forecast prototype with backtest gate
PR Description: Must strictly start with 'Close #302'. Do NOT merge."
```

---

## 3. Compliance Checklist (Mandatory Rules 1-9)

Every PR in Sprint 9 must self-check this list before requesting review:

- [ ] **Rule 1 (Zero-Emoji & Multilingual)**: Absolutely zero emojis in UI, toast, modal, server logs, or Telegram notifications.
- [ ] **Rule 2 (Root-Cause Solutions)**: No temporary workarounds (no monkey-patching). Must resolve technical problems at their root cause.
- [ ] **Rule 3 (Mandatory Estimation)**: Always state explicit active hours and delivery window in the PR description.
- [ ] **Rule 5 (Git Flow)**: Create dedicated branches (`feat/*`, `fix/*`, `ops/*`), open PR targeting `dev`. Never auto-merge PRs.
- [ ] **Rule 6 (Living Documentation)**: Update affected `docs/*` and `README.md`; mirror the `README.vi.md` quickstart only when prerequisites, make targets, ports or endpoints change; record each lesson as a new `docs/lessons/L<NN>-*.md` plus one INDEX row, never appended to a monolithic register.
- [ ] **Rule 7 (Leaf Modulith Standard)**: No cross-imports between sub-packages in `services/go-server/internal/`. Use hand-written SQL only (no ORM).
- [ ] **Rule 8 (Secret Security)**: Do not hardcode tokens, API keys, or credential-bearing weights paths. Fallback value in code must strictly be `""`.
- [ ] **Rule 9 (PR Linking Syntax — Updated per ADR-011 D6)**: PR description must strictly start with `Close #<issue-number>`. Kanban format `closes [KV-<card-id>]` is retired due to Plane Hub decommission (10/2026); track progress on GitHub Projects v2 board ProjectKV.
