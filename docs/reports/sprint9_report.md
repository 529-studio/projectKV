# Sprint 9 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)
**Document Version:** 1.0.0 (Audited & Reconciled with `dev` Branch)
**Date:** 2026-11-05
**Target Audience:** Technical Leads, System Architects, AI Evaluation Agents, Developers
**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/ADR-011-sprint8-audit-and-sprint9-plan.md`](../ADR-011-sprint8-audit-and-sprint9-plan.md) (Sprint 9 charter, D1-D8)
- [`docs/sprints/sprint9_execution_guide.md`](../sprints/sprint9_execution_guide.md) (wave sequencing and ticket map)
- [`docs/reports/sprint8_report.md`](sprint8_report.md) (predecessor record and carry-over findings)
- [`docs/reports/seg_eval_v1.md`](seg_eval_v1.md) (segmentation dataset, gate and latency)
- [`docs/reports/forecast_prototype_v1.md`](forecast_prototype_v1.md) (backtest method and gate)
- [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons 67-68; register L62-L68)
- GitHub PR ledger: PRs #304, #308, #327, #358, #361, #363, plus #311/#369; Issue #302

> **Verification basis.** Figures re-derived on 2026-11-05 from `dev` at `1dedbfa` and the GitHub
> API. Suites re-run: Go 22 packages under `-race`, web 167, seg 71, eval 76, train 4, cv-engine 27,
> forecast 57. S9-T6 was audited from an uncommitted working tree; claims not re-derivable from the
> repository are gaps in §3.

---

## 1. Executive Summary

Sprint 9 executed three fronts chartered by ADR-011: **Closing the Ground-Truth Segmentation
Flywheel**, the **Autonomous Staging Data Flywheel**, and the **Short-Horizon Forecasting
Foundation**. The first two are merged and closed; the third is implementation-complete but
uncommitted, and its gate passed on a synthetic fixture, not on traffic history.

1. **S9-T1 (Issue #297, PR #304)**: canonical polygon annotation contract, server-side class
   canonicalization and validation, exporter synthetic-geometry deprecation, and a dependency-free
   SVG canvas editor in `/labeler`.
2. **S9-T2 (Issue #298, PR #358)**: offline SAM-assisted pre-labeling with MobileSAM ONNX, R2
   streaming, AVX2 host guard, provenance stamping, and mandatory human review.
3. **S9-T3 (Issue #299, PRs #308 and #311)**: `GET /api/harvester/status`, boot-time missed-slot
   catch-up, run bookkeeping, and staging compose propagation with the harvester enabled.
4. **S9-T4 (Issue #300, PR #361)**: dataset v1 manifest, first off-box `yolov8n-seg` training, the
   quantitative mAP/IoU gate, OpenVINO FP16 export and cv-engine segmentation precedence.
5. **S9-T5 (Issue #301, PR #327)**: `notify.BuildFloodAlert` pure helper plus `cmd/notify-preview`,
   removing the hand-copied Telegram message from the staging verification script.
6. **S9-T7 (Issue #309, PR #363)**: optional `direction` (`a`/`b`) on `roadway` polygons, bilingual
   labeler selector, validation, and a per-split `roadway_regions.json` sidecar.
7. **S9-T6 (Issue #302, branch `feat/s9-t6-congestion-forecast-prototype`)**: append-only verdict
   history, feature store, chronological backtest harness, and guarded `/forecast` endpoint. No PR
   exists; Issue #302 remains open.

Six issues are closed (#297-#301, #309); S9-T6 has no commit, review or CI run, so it is not
counted as delivered.

### Quantitative Scorecard

| Metric | Planned (ADR-011) | Delivered | Evidence |
|---|:---:|:---:|---|
| Core Sprint 9 tickets | 7 | **7 implementation-complete; 6 merged + 1 branch-only** | PRs #304, #308/#311, #327, #358, #361, #363; Issue #302 open |
| Ticket coverage | 7 | **#297, #298, #299, #300, #301, #309 closed; #302 open** | GitHub ledger |
| PRs merged in window (10-25 to 11-05) | — | **46** (+17,715 / -14,988) | GitHub ledger |
| Open PRs / open issues | — | **0 / 1** (#302) | GitHub ledger |
| Go backend suite | all packages under `-race` | **22/22 passing**, `gofmt -l` empty | `go test -race ./...` |
| Web unit tests | 155+ | **167/167 passing** | `npm test --prefix apps/web` |
| CV engine / tooling suites | 0 failures | seg **71**, eval **76**, train **4**, cv-engine **27** | per-suite `unittest` |
| Python forecast suite | 0 failures | **57/57** (36 backtest, 21 extraction) | `tools.forecast.*_test` |
| Segmentation dataset | >= 500 masked frames | **289 annotated** of 550 harvested | `seg_dataset_v1_manifest.json` |
| Segmentation gate | `water_puddle` IoU > null baseline | **IoU 0.582** vs 0.000; mAP50 **0.718**, mAP50-95 0.496 | `seg_eval_v1.md` |
| Inference latency | inside 30 s / 57-camera cycle | **p95 14.8 ms** at 416 px; 0.84 s sweep (2.8% of budget) | `seg_eval_v1.md` §5 |
| Forecast gate | beat persistence on >= 1 horizon | **3/3 horizons**: +52.0%, +44.7%, +37.3% MAE | `forecast_prototype_v1.md` §4 |
| Live forecast history | — | **0 rows** on 2026-11-05 | fixture README |

---

## 2. Wave-by-Wave Audit of Delivered Work

### Wave 0 — Critical Path: Polygon Contract & Canvas Editor (S9-T1, Issue #297, PR #304)

- **Contract.** `LabelPayload.annotations[]` carries `PolygonAnnotation{class, polygon, source}` with
  coordinates normalized to `[0,1]`. `SaveLabel` canonicalizes aliases server-side (`flood` to
  `water_puddle`, `road` to `roadway`), enforces the six-class taxonomy, 3-64 vertices and at most 32
  annotations, and rejects pixel coordinates with HTTP 400.
- **Exporter gate.** `tools/eval/export_yolo_seg.py` no longer fabricates geometry by default.
  Metadata-derived polygons require `--allow-synthetic` and are stamped as fabricated; the default
  path skips annotation-less labels and reports `skipped_no_annotations` (ADR-011 D2, closing Sprint
  8 finding F2).
- **Schema and editor.** Harvester Draft-07 (`internal/harvester/schema.go`, `label_schema.json`)
  accepts the same optional `annotations` object. `PolygonCanvas.tsx` is a zero-dependency SVG
  overlay: click-to-add, close on first vertex or Enter, Esc cancel, drag, delete, per-class
  monochromatic tones. Layout measures with `getBoundingClientRect` on mount, image load and resize;
  `ResizeObserver` is enhancement only because the embedded browser never delivered callbacks, a
  defect found in E2E.
- **Evidence.** A live go-server plus Next dev session drew and persisted a `water_puddle` polygon
  that re-rendered after reload. Exporting four labels produced two train and one validation YOLO
  files with the exact polygons, one counted skip, zero synthesized masks, `--verify` 100%. 1,306
  added and 58 removed lines across 18 files.
- **Post-merge correction (PR #369).** The exporter's recursive `rglob` fallback could re-split an
  existing `train/`/`val/` tree under the same seed, moving a validation frame into training; it was
  removed (L66).

### Wave 1 — Data Acceleration & Autonomous Flywheel (S9-T2, Issue #298, PR #358; S9-T3, Issue #299, PR #308)

**S9-T2 — SAM-assisted pre-labeling (PR #358).**

- `tools/seg` separates the annotation contract (`contract.py`), mask-to-polygon geometry
  (`image_geometry.py`: Douglas-Peucker and normalization), MobileSAM ViT-Tiny ONNX runtime
  (`sam_runtime.py`), and the batch CLI (`prelabel_sam.py`). Frames stream from R2; candidates upload
  with `annotator: sam_prelabel` and `source: sam_prelabel`.
- Guardrails: a CPU-feature check rejects non-AVX2 Linux hosts, keeping ViT inference off the Celeron
  ingress host (ADR-011 D3); uploads are token-bucket paced with exponential backoff; `--dry-run`,
  `--limit`, `--date` and `--local-frames` bound every run.
- Roadway is proposed from the largest stable central region, vehicles from detector boxes, and
  puddles stay proposals only: specular wet asphalt cannot be separated from standing water without
  human judgment.
- Evidence: 50 real frames from `2026-10-16` produced 50 validated candidates with 0 errors and
  synchronized 2,500 snapshot frames to R2. Throughput is 1.38 frames/s on one worker and 5.20 on
  four (9.6 s per 50-frame batch); 71 tests pass. Local-cache runs had written filesystem paths into
  `snapshot_key`, breaking raw snapshot fetches; payloads are now normalized to
  `<date>/<camera_id>/<file>` (L62), evidenced by
  `docs/proofs/s9_t2_sam_prelabel_ui_evidence.png`.

**S9-T3 — Staging flywheel (PR #308, PR #311).**

- `GET /api/harvester/status` returns schedule slots, next slot, per-slot runs with scope and
  duration, harvested/failed counts, rain-watcher state and cooldown, lifetime totals, and a bounded
  error ring; a nil coordinator degrades to 503.
- `HARVESTER_CATCHUP_MINUTES` (default 120, 0 disables) fires today's missed slots exactly once at
  scheduler start; slot marking prevents double firing. `internal/harvester/stats.go` records
  thread-safe runs for scheduled, catch-up, rain and manual scopes with slot attribution.
- Evidence: the overnight rehearsal fired the 07:30 slot to the second and auto-labeled 56 of 57
  cameras. A live boot one minute past a configured slot logged `catch-up firing missed slot`,
  harvested 42 of 57 cameras in 29.3 s against the live DOT upstream, and served the full status
  payload; an unauthenticated request returned 401.
- Enablement: PR #311 added the missing `HARVESTER_*` variables to `docker-compose.stg.yml`
  (`HARVESTER_ENABLED=true`, `07:30,12:00,17:30`, concurrency 15, 60-minute rain cooldown); before it
  the container carried none, so the subsystem was silently disabled. The three-slot, full-day
  acceptance observation remains unarchived (Section 4).

### Wave 2 — First Segmentation Training & Staging Deployment (S9-T4, Issue #300, PR #361)

- **Dataset v1.** 550 real frames across four partitions (`2026-10-16`, `2026-10-15`, `2026-10-13`,
  `2026-10-08`); 289 annotated, 261 skipped for missing geometry; deterministic 80/20 split of 234
  train and 55 validation. 408 instances: 192 `water_puddle`, 73 `car`, 61 `roadway`, 51
  `motorcycle`, 19 `truck`, 12 `bus`. Stratification: 100 rain/wet, 450 dry, 120 dusk/night, 24
  corridors. Manifest and `yolo_seg_dataset_v1.yaml` are committed; images are not.
- **Training.** `yolov8n-seg.pt`, 50 epochs, 416 px, batch 16, AdamW (`lr0=0.001`,
  `weight_decay=0.0005`), mosaic 0.5, HSV jitter, horizontal flip 0.5, rotation plus/minus 10
  degrees. Export is OpenVINO FP16 at 6.42 MB, under the 10 MB ceiling.
- **Gate.** Mask mAP50 0.718 and mAP50-95 0.496; `water_puddle` mask mAP50 0.689 and mean IoU 0.582
  against a 0.000 null baseline (+58.2 points).
- **Latency.** p50 12.4 ms and p95 14.8 ms at 416 px; a 57-camera sweep costs 0.84 s against the
  30 s budget (97.2% headroom). The 320 px and 640 px configurations were measured; 416 px was
  selected for small-puddle detail.
- **Deployment.** `cv-engine` prefers `yolov8n_seg.xml` over `yolov8n.xml`; `/healthz` reports the
  loaded model; `/predict` emits `water_ratio`, `depth_estimated_cm` and `flood_assessment`; rollback
  is a rename plus restart. Repository evidence covers precedence, response shape and rollback; the
  operator-side weight promotion itself is not captured in the repo.
- **Post-merge correction (PR #369).** The two precedence tests rebuilt the candidate list inside the
  test body and could not fail when precedence was reversed. The decision was extracted into
  `select_model_xml` and `build_flood_assessment`, and both tests now exercise the shipped path (L66).

### Wave 3 — Debt Cleanup, Directional Ground Truth & Forecast Prototype (S9-T5, S9-T7, S9-T6)

**S9-T5 — Telegram single-source parity (Issue #301, PR #327).**

- Root cause: the Sprint 8 script hand-copied camera metadata and hardcoded the fixture district
  `Bình Thạnh`, while `dispatchFloodAlert` renders `cam.Zone`, which resolves to
  `Khu vực Nội thành TP.HCM` for the staging camera. Both copies shared the same wrong source.
- Fix: alert precedence (DisplayName, then Title, then CameraID; district from `cam.Zone`; ward from
  `cam.PhuongName`; chokepoint depth) moved into `notify.BuildFloodAlert`. `cmd/notify-preview` loads
  the catalog and chokepoint fixture and prints the production message; script Check 3 invokes it
  through `go run` and fails fast without a Go toolchain.
- Evidence: the CLI emits the production-exact message, the script passes 5/5 against a mock Bot API,
  and `go test -race ./...` is green. The contract test pins `Thạnh Mỹ Tây, Khu vực Nội thành TP.HCM`
  and the runbook states the Area line is the traffic zone, not the administrative district. Remaining
  work is the operator token provisioning of #246, not code.

**S9-T7 — Directional roadway polygons (Issue #309, PR #363).**

- Contract: `PolygonAnnotation` gained optional `Direction` (`a` or `b`), canonicalized from
  `dir_a`/`traffic_dir_a`/`near` and `dir_b`/`traffic_dir_b`/`far`. Direction is valid only on
  `roadway`; non-roadway classes and unknown values return 400, and an absent field decodes to empty.
- UI: selecting `roadway` exposes a bilingual direction selector (none, Direction A near lane,
  Direction B far lane), directional polygons render direction badges in both languages, and client
  validation mirrors the server rule.
- Exporter: YOLO segmentation text is unchanged (`roadway` stays class 0). Directional geometry goes
  to a per-split `roadway_regions.json` sidecar with normalized coordinates, direction and label id,
  omitted when no directional polygon exists. PR #369 later made the exporter skip sidecars by name.
- Purpose: the two carriageways are labeled separately, so a jammed direction no longer dilutes a
  free-flowing one. Evidence: `docs/proofs/s9_t7_directional_roadway_polygons.png`.

**S9-T6 — Short-horizon congestion forecast (Issue #302, branch only).**

- Why a new table: `camera_verdicts` upserts one row per camera, so every 30-second analysis
  overwrote the PCU and saturation index. Migration `0007_hotset_verdict_history.sql` adds an
  append-only table with `UNIQUE(camera_id, observed_at)` and `ON CONFLICT DO NOTHING`, fixed-width
  RFC3339 UTC timestamps, nullable PCU/SI, and observation/recording indexes.
- Feature store: `tools/forecast/extract_features.py` opens SQLite read-only, bins to 5 or 10
  minutes, anchors lags to the raw timeline with an explicit age bound, and emits a CSV plus a
  metadata sidecar; missing lags stay null rather than carrying stale values forward (L67).
- Backtest: `tools/forecast/backtest.py` uses a global chronological 70/30 split over distinct bins,
  purges training samples whose target crosses the cutoff per horizon, requires exact future-bin
  targets, and scores all models on one common eligible set; the gate fails on a tie and still writes
  diagnostics (L68).
- Results on the fixture: 10 min MAE 0.0044 versus 0.0092 (+52.0%), directional accuracy 83.6%;
  20 min 0.0102 versus 0.0184 (+44.7%); 30 min 0.0172 versus 0.0275 (+37.3%). Scored test samples
  are 342, 338 and 334, with 4, 8 and 12 training rows purged at the boundary.
- Endpoint: `GET /api/forecast` with a root `/forecast` alias accepts `camera_id`, `horizon` or
  `horizon_minutes`, `timestamp` and `model`. Age above 15 minutes marks the response stale and halves
  confidence; no history returns 422, an unknown camera 404, an unreachable repository 503.
- Ledger state: uncommitted on `feat/s9-t6-congestion-forecast-prototype`, 0 commits ahead of `dev`,
  no PR, no review, Issue #302 open. The fixture is synthetic because the live table held 0 rows;
  `internal/forecast/model.go` coefficients are calibrated on it.

---

## 3. Reality-Check Audit Findings — What Is Still Missing

### F1. Zero Live SQLite History at Boot / Cold-Start Horizon Barrier

- **Evidence.** `hotset_verdict_history` is created only when `store.Open` runs the embedded
  migrations at backend boot; the local `data/camera-view.db` last written on 2026-10-13 still holds
  four tables and no history table. The forecast provenance note records **0 rows on 2026-11-05**,
  and the backtest ran on `tools/forecast/fixtures/synthetic_congestion_5m.csv`. Writes require
  `!Stale` and a PCU/SI series; the endpoint reads a 45-minute window capped at 50 rows and returns
  422 `insufficient_history` when empty.
- **Consequence.** Every camera answers 422 until a hot-set cycle populates rows, and lag features
  need at least 15 minutes of valid bins. A multi-day series is required before the gate can be
  re-run on traffic, so until then the endpoint can serve coefficients fitted to a sine wave: a
  correctness risk, not a cosmetic one.
- **Required action.** Assert migration-before-first-verdict in the boot path, expose contiguous
  history coverage per camera, and keep `/forecast` disabled or labelled until a real-data re-fit
  beats persistence.

### F2. Directional Roadway Polygons Unconsumed by Downstream CV-Engine & Forecast

- **Evidence.** `direction` and `roadway_regions.json` appear only in the labeler, exporter, JSON
  schemas and documentation. `services/cv-engine/server.py` has no direction handling, and
  `internal/forecast` has no per-direction feature or target. Hotset PCU remains frame-wide.
- **Consequence.** S9-T7 creates truthful per-direction ground truth but no per-direction
  measurement. A one-side jam is still averaged with the free-flowing carriageway, so the forecast
  cannot learn the asymmetry that motivated the ticket, and the sidecar has no consumer.
- **Required action.** Assign detections to polygons in cv-engine, carry per-direction PCU into
  hotset verdicts, and ingest `roadway_regions.json` in the feature store before any directional
  claim is made.

### F3. Precipitation and Weather Signals Omitted from Feature Store v1

- **Evidence.** The committed feature columns are `lag_5m`, `lag_10m`, `lag_15m`,
  `rolling_mean_15m`, `hour_of_day` and `day_of_week`. `forecast_prototype_v1.md` §6 states the v1
  feature store carries no rain columns, although Issue #302 listed rain among candidate features.
  The platform already ingests Open-Meteo and RainViewer reflectivity.
- **Consequence.** Rain-driven congestion and wet-surface slowdown are outside the model; the ridge
  fit cannot separate a weather event from a normal peak, and the ablation needed to prove rain
  matters has not been run: a material omission for a flood-monitoring system.
- **Required action.** Join per-bin precipitation and weather class from the existing weather
  service and harvester metadata, then re-run the gate with and without the weather block.

### F4. Zone Subscription Semantic Mismatch Unresolved (Sprint 8 Carry-Over)

- **Evidence.** `GetSubscribersForChokepoint` matches `target_type='zone'` subscriptions against
  `cam.Zone`, a traffic-zone classification (`Khu vực Nội thành TP.HCM`, `TP. Thủ Đức`), while the
  subscription taxonomy and user-facing wording describe administrative districts. Sprint 8 finding
  F6 recorded the same defect; `internal/subscription` is unchanged in Sprint 9, and the Telegram
  runbook documents traffic-zone semantics without reconciling the API.
- **Consequence.** A subscriber registering a district identifier never matches an alert and
  receives nothing, silently. The S9-T5 parity fix makes the Area line correct, which makes the
  mismatch easier to observe but does not resolve it.
- **Required action.** Choose one canonical geography (traffic zone plus ward), normalize stored
  `target_id` values with a migration, and add a contract test proving a district-shaped
  subscription matches the alert it names.

### F5. Single-Worker Hardware Concentration on the Inference Worker

- **Evidence.** One AVX2 host (Ryzen 7 6800HS, 8C/16T, 3.8 GB RAM, 19 GB SSD) runs `warded-cv-engine`
  (2 vCPU / 2 GB cgroup), the GitHub Actions self-hosted runner, and SAM batch pre-labeling (four
  workers at 5.20 frames/s). The host guard disqualifies the non-AVX2 Celeron, so no second site can
  absorb the work. The p95 14.8 ms figure was measured without concurrent batch or CI load; the
  token bucket paces uploads, not CPU.
- **Consequence.** Concurrent pre-labeling, a 57-camera hot-set sweep and a CI job compete for the
  same cores and memory. Idle headroom is not a serving guarantee, and the host is a single point of
  failure for both live inference and annotation throughput.
- **Required action.** Benchmark p95 under representative co-load, cap SAM worker count and CPU
  affinity, schedule batches off-peak, and enforce a degradation path (320 px or skip the slot) in
  the scheduler.

### New Engineering Lessons (L62-L68)

| # | Lesson | Guardrail now enforced |
|---|---|---|
| [L62](../lessons/L62-offline-sam-prelabeling-provenance-and-dual-layer-storage-parity.md) | SAM provenance and storage parity | AVX2 host guard; normalized `snapshot_key`; mandatory `annotator`/`source`; no auto-promotion |
| [L63](../lessons/L63-vlm-structured-output-variance-and-array-unmarshal-resilience.md) | VLM structured-output variance | Bounded one-element array fallback; other element counts are named errors |
| [L64](../lessons/L64-first-segmentation-cycle-openvino-fp16-and-map-gating.md) | First segmentation cycle | Annotation purity by default; `water_puddle` IoU must beat null; rename-based rollback |
| [L65](../lessons/L65-directional-roadway-polygons-sidecar-and-per-direction-ground-truth.md) | Directional polygons and sidecars | `direction` only on `roadway`; YOLO text untouched; sidecar per split |
| [L66](../lessons/L66-self-referential-tests-and-silent-fallbacks-hide-the-live-path.md) | Self-referential tests and silent fallbacks | Extract the decision into a named function; assert delegation; fallbacks narrow or refused |
| [L67](../lessons/L67-upsert-only-state-tables-cannot-feed-time-series-models.md) | Upsert-only tables cannot feed time series | Append-only history, idempotent key, fixed-width UTC, lag age bounds returning null |
| [L68](../lessons/L68-chronological-split-horizon-boundary-leakage.md) | Chronological splits still leak | Purge samples whose target crosses the cutoff; score all models on one eligible set |

The costly defects of this sprint were measurement defects: a test that rebuilt the code it tested, a
fixture that stood in for traffic, a state table that erased history, a split that leaked at the
horizon, and an uncommitted runbook. Each made green mean less than it
appeared. The pattern is now codified: decisions are addressable functions,
evidence is an artifact, and absent evidence fails closed.

---

## 4. Carry-Over Ledger

| Item | State | Owner | Blocking |
|---|---|---|---|
| Issue #302 / S9-T6 | Implementation complete in the working tree; needs commit, PR, review and merge | Engineering | Review capacity |
| S9-T6 real-data gate | Fixture-only pass; 0 live history rows; regenerate from `features_5m.csv` | Engineering | Collection time |
| Forecast endpoint contract | `/api/forecast` implemented; Issue #302 specifies `/api/v1/cameras/hotset/forecast` | Architecture | Contract decision |
| Segmentation dataset v2 | 289 masked frames against a 500 target; no committed justification or evaluation artifact | Engineering | Labeling throughput |
| Harvester acceptance | Three slots plus one rain run with R2 counts unarchived; `staging_harvester_flywheel.md` absent | Operator + Engineering | Observation window |
| Issue #246 | Live bot token in Stack 14, group confirmation, evidence capture | Operator | Telegram credentials |
| F4 zone semantics | Administrative district versus traffic zone, open since Sprint 8 F6 | Engineering | Taxonomy decision |
| F5 co-tenancy budget | No p95 measurement under SAM batch plus CI load | Engineering | Measurement window |
| Directional consumption (F2) | No cv-engine or forecast consumer for `direction` | Engineering | Sprint 10 scope |

---

## 5. Sprint Bridge to Sprint 10 (ADR-012)

ADR-012 succeeds ADR-011; this section records the bridge scope that Sprint 9's
evidence implies.

Sprint 10 converts Sprint 9's artifacts into operational truth in one sequence:

1. **Operationalize directional inference in cv-engine.** Assign detections to directional `roadway`
   polygons, compute per-direction PCU, and publish directional saturation in the hot-set verdict.
   Without this, S9-T7 remains a labeling feature and the one-side-jam case stays invisible.
2. **Accumulate live hot-set history on staging.** Verify the migration runs before the first
   verdict, keep the harvester enabled through a multi-day window, add a missed-slot watchdog, and
   re-fit the forecast on real bins. The synthetic gate is a harness proof, not a traffic proof.
3. **Align district and zone subscriptions.** Pick one geography, migrate stored targets, and prove
   with a contract test that a district-shaped subscription matches its alert before any campaign.
4. **Expose the forecast UI only after the real-data gate.** A dashboard panel or map layer is
   justified when the candidate beats persistence at one horizon on staging history and the
   staleness guard is demonstrated end to end.

Parallel obligations: close evidence debt (dataset v2 >500 frames, committed
evaluation artifact, harvester runbook/acceptance log) and settle co-tenancy on
the AVX2 host. Sprint 10 exit: the flywheel runs unattended on staging with
per-direction ground truth consumed by inference and a forecast validated on real history.

