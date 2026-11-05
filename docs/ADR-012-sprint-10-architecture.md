# ADR-012: Sprint 9 Audit Reconciliation & Sprint 10 Execution Architecture — Directional Flow, Forecast Maturation & Flywheel Scaling

**Status**: Accepted
**Date**: 2026-11-05
**Authors**: ProjectKV Engineering & Architecture Team
**Auditor**: Senior Technical Auditor & Devil's Advocate
**Governing Documents**: `AGENTS.md` (Rules 1–11), `docs/GO_BACKEND_ARCHITECTURE.md`, `docs/ADR-011-sprint8-audit-and-sprint9-plan.md`, `docs/reports/sprint9_report.md`
**Related**:
- [`sprint9_report.md`](reports/sprint9_report.md) (Sprint 9 completion, verification and findings F1–F7)
- [`seg_eval_v1.md`](reports/seg_eval_v1.md) (dataset v1, gate and latency evidence)
- [`forecast_prototype_v1.md`](reports/forecast_prototype_v1.md) and [`forecast_feature_store_v1.md`](reports/forecast_feature_store_v1.md) (backtest method and feature contract)
- [`sprints/sprint9_execution_guide.md`](sprints/sprint9_execution_guide.md) (predecessor wave map)
- [`labeling_guidelines.md`](labeling_guidelines.md) and [`taxonomy_traffic_and_flood_labeling.md`](taxonomy_traffic_and_flood_labeling.md)
- [`lessons/INDEX.md`](lessons/INDEX.md) (L62–L68; guardrails extended here)
- [`mlops_automated_annotation_and_bulk_approval.md`](mlops_automated_annotation_and_bulk_approval.md)
- Proof artifacts: [`s9_t2_sam_prelabel_ui_evidence.png`](proofs/s9_t2_sam_prelabel_ui_evidence.png), [`s9_t7_directional_roadway_polygons.png`](proofs/s9_t7_directional_roadway_polygons.png)

---

## 1. Context & Retrospective Reality-Check Audit

Sprint 9 merged six tickets (#297–#301, #309) and completed #302 implementation-complete on an uncommitted branch. While core engineering proved sound (zero polygon fabrication, MobileSAM AVX2 pre-labeling, quantitative segmentation gate clearing IoU 0.582 vs 0.000 null baseline, and 3/3 forecast backtest horizons beating persistence), the audit revealed **capability without operational truth**: gates measured harnesses rather than live traffic, directional polygons lacked downstream consumers, and live history held zero rows at boot. Sprint 10 bridges these artifacts into verified production truth.

### 1.1 Acceptance Verdicts

| Ticket | Scope | Verdict | Binding Condition |
|---|---|---|---|
| #297 | Polygon contract and canvas editor | Accepted | Exporter glob fallback removed in PR #369 (L66). |
| #298 | SAM pre-labeling toolchain | Accepted with conditions | Human review mandatory; co-tenancy verified in T8. |
| #299 | Staging autonomous harvester & status | Accepted with conditions | 72h soak acceptance log and runbook committed in T4. |
| #300 | Dataset v1, training, OpenVINO gate | Accepted with conditions | Candidate weights, not final; v2 gate and deployment evidence in T7. |
| #301 | Telegram parity (notify-preview) | Accepted | Bot token provisioning (#246) remains operator duty. |
| #309 | Directional roadway polygons | Partially accepted | Contract accepted; runtime ingestion deferred to T3. |
| #302 | Short-horizon forecast prototype | Not accepted | Synthetic fixture gate; superseded by T1/T4 live gate. |

### 1.2 Reality-Check Findings (F1–F7)

- **F1. Zero live history at boot and cold-start barrier.** `hotset_verdict_history` is created only after boot migrations. Provenance recorded 0 live rows on 2026-11-05; backtest ran on synthetic sine fixtures. A cold boot returns 422 `insufficient_history`. *Consequence:* every camera is unservable until a cycle runs, and a partially populated table would serve synthetic coefficients as traffic. *Action:* Assert schema at boot, expose coverage API, and serve only models fitted on live history.
- **F2. Directional roadway polygons unconsumed downstream.** Directional labels and `roadway_regions.json` exist only in labeling tooling. `cv-engine` and `internal/forecast` lack per-direction metrics, averaging jammed and free carriageways. *Consequence:* the S9-T7 ground truth earns nothing operationally and a one-side jam stays invisible. *Action:* Build a camera geometry registry, multipart inference, and directional PCU/SI in Go verdicts.
- **F3. Weather and precipitation omitted from feature store v1.** Feature store v1 omits rainfall despite available Open-Meteo and RainViewer data. Weather impacts cannot be separated from traffic peaks. *Consequence:* rain-driven congestion is unmodelable and no ablation has tested whether rain matters. *Action:* Persist weather observations on a 10m cadence, join as-of with age bounds, and run an empirical ablation.
- **F4. Zone subscription semantic mismatch (Sprint 8 F6 carry-over).** Subscriptions match `cam.Zone` (traffic zones like `Khu vực Nội thành TP.HCM`), but migration 0006 documented them as districts. District subscriptions silently receive zero alerts. *Consequence:* a subscriber who names their district is never told the alert exists, and a second deferral makes the path untrustworthy by policy. *Action:* Separate operational `zone` and administrative `ward`, migrate legacy strings via alias tables, and quarantine orphans.
- **F5. Single-worker hardware concentration.** One AVX2 host (Ryzen 7 6800HS, 8C/16T, 3.8 GB RAM) co-hosts `cv-engine`, GHA runner, and SAM batch labeling. P95 14.8 ms latency was measured without concurrent load. *Consequence:* idle headroom is not a serving guarantee, and the host is a single point of failure for inference and annotation. *Action:* Benchmark under co-load, cap worker threads, enforce off-peak batching, and code-enforce degradation.
- **F6. Segmentation dataset v2 volume gap.** Dataset v1 contains 289 annotated frames of 550 harvested (261 skipped, 47% skip rate), below the 500 target. Puddle labels dominate; directional roadway labels are sparse (61 instances). *Consequence:* retraining at this volume cannot separate model improvement from noise, and the skip rate is unexplained. *Action:* Annotate skipped frames, capture rain/night strata, and enforce group-aware splits.
- **F7. Forecast endpoint divergence and UI exposure gap.** Issue #302 specified `/api/v1/cameras/hotset/forecast`, but the branch implemented `/api/forecast` and root `/forecast`. No frontend UI exists and the payload carries no availability or validation provenance. *Consequence:* two truths exist before the first consumer, and a client cannot distinguish no data from not validated. *Action:* Standardize on the canonical route, delete legacy aliases, add a server-driven availability state, and deliver a bilingual, accessible timeline.

---

## 2. Architecture Decisions

### D1. Directional Runtime Ingestion in CV-Engine & Hot-Set Verdicts

- **Registry.** Approved directional polygons export to `data/roadway_regions/<camera_id>.json` via `tools/eval/build_roadway_registry.py`. Each region specifies `region_id`, `direction` (`a`/`b`), normalized `polygon`, and `capacity_share` (must sum to 1.0 +- 0.01; invalid shares reject registration). There is no implicit 50/50 split: unregistered cameras report `direction_coverage: none` and stay frame-wide.
- **Modulith contracts.** Leaf package `camera` loads and validates the registry at boot and embeds regions on `camera.Camera`. `hotset.Camera` carries regions; `cvclient` forwards them.
- **CV-engine endpoint.** `POST /predict` accepts optional multipart metadata (`meta` JSON with `regions`) while the legacy `application/octet-stream` body stays valid. Responses add `regions[]` with per-region vehicle counts and water ratio intersected with the region polygon.
- **Go verdict & severity.** Directional saturation: `SI_direction = PCU_direction / (BaseCapacity * capacity_share)`. `Verdict` carries `Directions []DirectionalVerdict`. Overall frame severity is pinned to the **worst direction**, preventing jam dilution.
- **Database schema.** Migration `0008_hotset_directional_history.sql` adds `direction` and `region_id` to `hotset_verdict_history` with unique constraint `(camera_id, direction, observed_at)`.

### D2. Live Time-Series Warmup & Contiguous Data Gate

- **Boot assertion.** Schema migration assertion runs in `cmd/server` immediately after `store.Open`. Missing history tables trigger immediate fatal exit. `/healthz` reports `schema.history_ready`.
- **Coverage API.** `GET /api/v1/hotset/history/coverage?camera_id=&window=72h` reports observation bounds, bin count, coverage ratio, longest gap, and per-horizon eligibility.
- **Serving & refit gates.** Online serving requires >= 80% coverage over trailing 60m and >= 15m valid history. Model refitting requires >= 72h contiguous history with >= 70% bin coverage and >= 30 samples/horizon.
- **Provenance.** Forecast models record `fit_source` (`live_history` vs `synthetic_fixture`), rows, window, and SHA-256. Serving returns 503 `model_unvalidated` unless `fit_source=live_history`; `FORECAST_ALLOW_SYNTHETIC=true` is a development-only escape hatch that forces `experimental: true` and is never set on staging or production.
- **Flywheel soak & retention.** Harvester status exposes `missed_slots`, `last_success_at` and `history_coverage_hours`; a slot missed beyond two intervals is surfaced. A 72h staging soak logs three fixed slots daily plus one rain run to `docs/proofs/sprint10/harvester_observation.json`. A 30-day raw-data retention job runs daily.

### D3. Weather & Precipitation Feature Store v2

- **Persistence.** Migration `0009_weather_observations.sql` records append-only weather snapshots (precipitation mm, weather code, temperature, humidity, source, payload hash). A 10m background ticker in `cmd/server` samples Open-Meteo and RainViewer, independent of the HTTP cache TTL.
- **Radar scope.** RainViewer stays a city-scale rain indicator (`radar_active`, frame age), not per-camera reflectivity; no decoder maps radar tiles to a camera footprint in this sprint.
- **As-of join discipline.** Feature extraction joins the closest weather observation within a 30m maximum age bound; missing observations remain null (no forward fill, L67).
- **Ablation protocol.** Three model variants on identical sets: A (traffic lags), B (A + weather), C (B + directional). Weather features are retained only if variant B improves MAE by >= 2% without degrading any horizon by > 5%.

### D4. Canonical Zone & Ward Subscription Taxonomy Reconciliation

- **Taxonomy.** Two distinct tiers: operational `zone` (traffic grouping from `camera.DefaultZones`) and administrative `ward` (`phuong_name`, 2025 boundary GeoJSON). There is no district tier in the catalog or the 2025 administrative model; the legacy wording is removed from the contract and the alert struct.
- **Validation.** `target_type` is restricted to `{all, chokepoint, zone, ward}`. Endpoints reject invalid combinations with 400 and provide taxonomy via `GET /api/subscriptions/taxonomy`.
- **Migration & quarantine.** Migration `0010_subscription_taxonomy.sql` maps legacy district strings to canonical zones and quarantines unresolvable targets in `flood_subscriptions_legacy_0010`. `cmd/subscriptions-audit` verifies zero dropped subscribers.
- **Alert struct.** `notify.FloodAlert.District` is renamed to `Zone`, aligning backend telemetry with frontend presentation.

### D5. Web UI Short-Horizon Forecast Timeline & Uncertainty Display

- **Route contract.** Canonical `GET /api/v1/cameras/:id/forecast` returns multi-horizon predictions (10, 20, 30 min), 95% confidence intervals, model provenance, and availability status (`ok`, `insufficient_history`, `model_unvalidated`, `stale`). Batch `GET /api/v1/cameras/hotset/forecast` serves the map at the same shape. Root `/forecast` and `/api/forecast` are removed.
- **Fail-closed UI.** Client renders a disabled notice explaining data requirements when status is not `ok`. Predictions are never rendered from unvalidated models. A `stale` forecast dims the series and halves confidence, mirroring verdict staleness.
- **Frontend architecture.** Strict 4-file structure: `types/forecast.ts`, `utils/forecastApi.ts`, `hooks/useForecast.ts`, and `components/ForecastTimeline.tsx`. Monochromatic design, bilingual `vi`/`en` strings (`FORECAST_I18N`), 48 px touch targets, and zero emojis.

### D6. Segmentation Dataset v2 Expansion & Model Retraining

- **Dataset targets.** Expand dataset to >= 550 annotated frames (>= 175 rain/wet, >= 120 dusk/night, >= 30 dual-carriageway). All 261 skipped frames from v1 are audited and annotated.
- **Grouped split.** Stratified 80/20 train/validation split grouped strictly by `camera_id` and capture date to prevent burst leakage (L68).
- **Benchmark gate.** Retrained `yolov8n-seg` must achieve `water_puddle` IoU >= 0.612 (+0.03 over v1), mask mAP50-95 >= 0.496, no class IoU drop beyond 0.02, and p95 inside the 30 s / 57-camera budget at 416 px; a directional sidecar consumer test covers the export to registry to cv-engine path. A failed gate publishes the negative result and error analysis; thresholds are not renegotiated. Deploy via OpenVINO FP16 with documented rollback.

---

## 3. Sprint 10 Backlog Waves

Sprint 10 spans 2026-11-09 to 2026-11-27 (3 weeks). Total estimated active effort is **106–134 hours** (13–17 working days) plus 12 hours of annotation review, assuming two engineers. Wave 0 establishes contracts and data integrity; Wave 1 ingests directional data and runs the history soak; Wave 2 delivers weather features, retraining and UI; Wave 3 hardens co-tenancy.

The exit criterion is not "features merged". It is: the flywheel runs unattended on staging with contiguous history, directional ground truth is consumed by inference and the forecast, and a model re-fitted on live traffic beats persistence at one horizon.

| Wave | ID | Issue | Title | Labels | Est. (Rule 3) | Window | Depends | Closes on merge |
|:---:|:---:|:---:|---|---|:---:|---|---|---|
| 0 | S10-T1 | #372 | `[go-server] Forecast Contract Consolidation & Fail-Closed Real-Data Gate` | `backend, sprint-10, wave-0` | 8–10h (1.5d) | Nov 09–10 | #302 branch | #302 + #372 |
| 0 | S10-T2 | #373 | `[go-server + ops] Canonical Zone & Ward Subscription Taxonomy Reconciliation` | `backend, cross-cutting, bug, sprint-10, wave-0` | 12–16h (2.5d) | Nov 09–11 | — | #373 |
| 1 | S10-T3 | #374 | `[go-server + cv-engine] Directional Runtime Ingestion: Registry, Polygon Assignment & Per-Direction Verdicts` | `backend, tooling, cross-cutting, sprint-10, wave-1` | 22–26h (4d) | Nov 11–17 | #372 | #374 |
| 1 | S10-T4 | #375 | `[ops + go-server] History Warmup, Coverage API & 72-Hour Flywheel Soak` | `backend, tooling, cross-cutting, sprint-10, wave-1` | 10–12h (2d + soak) | Nov 12–20 | #372, #374 | #375 |
| 2 | S10-T5 | #376 | `[go-server + tooling] Weather & Precipitation Feature Store v2 with Ablation` | `backend, tooling, sprint-10, wave-2` | 14–18h (3d) | Nov 16–20 | #374, #375 | #376 |
| 2 | S10-T6 | #377 | `[frontend] Short-Horizon Forecast Timeline & Uncertainty Display` | `frontend, cross-cutting, sprint-10, wave-2` | 14–18h (3d) | Nov 18–24 | #372, #375 | #377 |
| 2 | S10-T7 | #378 | `[tooling + cv-engine] Segmentation Dataset v2 Expansion & Retraining Gate` | `tooling, cross-cutting, sprint-10, wave-2` | 18–24h (4d + 12h label) | Nov 13–25 | #374 | #378 |
| 3 | S10-T8 | #379 | `[ops + tooling] Inference Worker Co-Tenancy Benchmark & Load-Shedding Policy` | `tooling, backend, cross-cutting, sprint-10, wave-3` | 8–10h (2d) | Nov 23–26 | #374, #378 | #379 |

Every PR opens with `Close #<its issue>`; T1 additionally opens with `Close #302`. Progress is tracked on the ProjectKV board (Projects v2), not a Kanban card format.

### Ticket Specifications

- **S10-T1 (#372): Forecast Contract Consolidation & Fail-Closed Real-Data Gate.** Standardize `/api/v1/cameras/:id/forecast` multi-horizon payload; delete `/api/forecast` and `/forecast`; add `availability` status and model provenance; commit #302 branch; enforce 503 for unvalidated synthetic fixtures. Estimate: 8–10h active (1.5 working days), Nov 09–10. Closes #302 and #372 upon merge.
- **S10-T2 (#373): Canonical Zone & Ward Subscription Taxonomy Reconciliation.** Enforce `{all, chokepoint, zone, ward}` enum; build `0010_subscription_taxonomy.sql` alias and quarantine tables; rename `FloodAlert.District` to `Zone`; update `cmd/subscriptions-audit`. Estimate: 12–16h active (2.5 working days), Nov 09–11. Closes #373 upon merge.
- **S10-T3 (#374): Directional Runtime Ingestion.** Build `tools/eval/build_roadway_registry.py`; load geometry in `camera` package; extend `/predict` with multipart metadata; compute directional PCU/SI in Go verdicts; apply migration 0008. Estimate: 22–26h active (4 working days), Nov 11–17. Closes #374 upon merge.
- **S10-T4 (#375): History Warmup, Coverage API & 72-Hour Flywheel Soak.** Assert migration schema at boot; add `GET /api/v1/hotset/history/coverage`; run 72h staging observation; commit `staging_harvester_flywheel.md`; implement 30-day retention. Estimate: 10–12h active (2 working days plus soak window), Nov 12–20. Closes #375 upon merge.
- **S10-T5 (#376): Weather & Precipitation Feature Store v2 with Ablation.** Create `weather_observations` migration 0009; sample Open-Meteo on 10m ticker; implement as-of joins with 30m cutoff; run 3-way ablation backtest. Estimate: 14–18h active (3 working days), Nov 16–20. Closes #376 upon merge.
- **S10-T6 (#377): Short-Horizon Forecast Timeline & Uncertainty Display.** Implement 4-file frontend structure; render monochrome timeline with 95% confidence bands; show disabled states on unvalidated data; add bilingual `FORECAST_I18N`. Estimate: 14–18h active (3 working days), Nov 18–24. Closes #377 upon merge.
- **S10-T7 (#378): Segmentation Dataset v2 Expansion & Retraining Gate.** Annotate 261 skipped frames; assemble 550+ frame dataset; enforce group-aware camera/date split; train `yolov8n-seg`; gate IoU >= 0.612; export OpenVINO FP16. Estimate: 18–24h active (4 working days plus 12h label review), Nov 13–25. Closes #378 upon merge.
- **S10-T8 (#379): Inference Worker Co-Tenancy Benchmark & Load-Shedding Policy.** Benchmark p95 under concurrent sweep, SAM batch, and CI; configure CPU affinity and cgroups; enforce automatic 320 px fallback. Estimate: 8–10h active (2 working days), Nov 23–26. Closes #379 upon merge.

---

## 4. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|:---:|:---:|---|
| Directional geometry missing for production cameras | High | High | T3 builds registry and pilots 3 corridors; unregistered cameras fall back to frame-wide. |
| Live history accumulation delayed by staging downtime | Medium | High | T4 establishes 72h soak early; coverage gate strictly returns 422/503 rather than guessing. |
| UI displays uncalibrated predictions | Low | High | Server-driven availability enum; client fails closed on non-OK states. |
| Weather upstream outage or rate limits | Medium | Medium | As-of join with 30m bound; missing records remain null without crashing pipelines. |
| Single AVX2 worker resource exhaustion | High | High | T8 benchmarks co-tenancy, isolates cgroups, and schedules SAM pre-labeling off-peak. |
| Dataset v2 fails IoU improvement gate | Medium | Medium | Maintain v1 weights in service; commit negative findings; never lower threshold. |
| Taxonomy migration drops legacy subscribers | Low | High | Reversible migration with dedicated quarantine table and zero deletion. |
| Sprint capacity insufficient at 106–134 hours | Medium | High | Two-engineer assumption stated; Wave 3 and T7 retraining are the declared slip order; Waves 0–1 stay intact. |

---

## 5. Consequences

- **Positive.** Closes the loop between ground truth and runtime inference: directional roadway annotations directly drive per-direction congestion metrics. Replaces synthetic backtest gates with real-traffic validation. Reconciles chronic subscription routing ambiguities. Delivers an accessible, honest user interface.
- **Negative / Costs.** Requires 106–134 hours of engineering effort and 12 hours of annotation review across two engineers. Migration 0008 alters the history index requiring rehearsal. Model retraining may yield a defensible null result. At one engineer, Wave 3 and T7 retraining slip one week.
- **Neutral.** Existing frame-wide metrics, map layers, and v1 segmentation weights remain fully functional during transition.
