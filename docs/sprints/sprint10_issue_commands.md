# Sprint 10 Issue Creation Commands (ADR-012)

**Status: executed on 2026-11-06.** The eight tickets were filed as issues #372–#379
and each now carries a `Close Linkage (AGENTS.md Rule 9)` section. This file is the
reproducible specification of what was filed; re-running the create commands against
`529-studio/projectKV` would duplicate the issues.

| Ticket | Issue | Title |
|---|---|---|
| S10-T1 | #372 | Forecast contract consolidation and fail-closed real-data gate |
| S10-T2 | #373 | Canonical zone and ward subscription taxonomy reconciliation |
| S10-T3 | #374 | Directional runtime ingestion: registry, polygon assignment, per-direction verdicts |
| S10-T4 | #375 | History warmup, coverage API and 72-hour flywheel soak |
| S10-T5 | #376 | Weather and precipitation feature store v2 with ablation |
| S10-T6 | #377 | Short-horizon forecast timeline and uncertainty display |
| S10-T7 | #378 | Segmentation dataset v2 expansion and retraining gate |
| S10-T8 | #379 | Inference worker co-tenancy benchmark and load-shedding policy |

Commands for the eight Sprint 10 tickets chartered by
[`docs/ADR-012-sprint-10-architecture.md`](../ADR-012-sprint-10-architecture.md).
Each body carries the Rule 3 estimate, problem, solution, scope, acceptance criteria
and the PR close-linkage contract. Every PR must open with `Close #<issue>` per
AGENTS.md Rule 9; progress is tracked on the ProjectKV board (Projects v2).

## 0. Labels

```bash
gh label create sprint-10 --description "Sprint 10 backlog (ADR-012)" --color "1D76DB" || true
```

The `wave-0` to `wave-3`, `backend`, `frontend`, `tooling`, `cross-cutting`, `bug`
and `enhancement` labels already exist.

## 1. S10-T1

```bash
gh issue create \
  --title "[go-server] S10-T1: Forecast Contract Consolidation & Fail-Closed Real-Data Gate" \
  --label "backend,sprint-10,wave-0" \
  --body "$(cat <<'BODY'
## Problem

S9-T6 (#302) implemented `GET /api/forecast` plus a root `/forecast` alias while the issue specified `/api/v1/cameras/hotset/forecast`. The branch is uncommitted, has no PR, and can serve ridge coefficients fitted to `tools/forecast/fixtures/synthetic_congestion_5m.csv` as if they were traffic. The payload carries no availability or validation provenance, so no client can tell "no data" from "not validated".

## Solution

One canonical multi-horizon contract under `/api/v1/cameras`, alias removal before merge, and a fail-closed serving gate keyed to model provenance.

## Scope / Files

- `services/go-server/internal/forecast/{handler.go,service.go,deps.go,model.go}`
- `services/go-server/cmd/server/main.go` (route registration, root alias removal)
- `services/go-server/docs/` (regenerated Swagger)
- `services/go-server/internal/forecast/*_test.go` (golden tests for four availability states)
- `README.md` (state the real exposure), `docs/reports/forecast_prototype_v1.md` (provenance note)

## Acceptance Criteria

- [ ] `GET /api/v1/cameras/:id/forecast` returns current SI plus 10/20/30-minute horizons with 95% intervals and `model.{fit_source,validated,mae}`.
- [ ] `/api/forecast` and `/forecast` return 404.
- [ ] A synthetic artifact returns 503 `model_unvalidated` under default configuration.
- [ ] `FORECAST_ALLOW_SYNTHETIC=true` returns 200 with `experimental: true` and is absent from staging/production compose.
- [ ] Unknown camera 404, invalid horizon 400, empty history 422, repository failure 503.
- [ ] `go test -race ./...` and `make test-forecast` green; `make swagger` run; CI green.
- [ ] The branch is committed, reviewed and merged with a PR that opens `Close #302`.

## Dependencies

Existing branch `feat/s9-t6-congestion-forecast-prototype`.

## Estimation (Rule 3)

- Active Effort: 8-10 hours
- Delivery Window: 1.5 working days (2026-11-09 to 2026-11-10)

## Close Linkage

PR must begin with `Close #302` and `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 2. S10-T2

```bash
gh issue create \
  --title "[go-server + ops] S10-T2: Canonical Zone & Ward Subscription Taxonomy Reconciliation" \
  --label "backend,cross-cutting,bug,sprint-10,wave-0" \
  --body "$(cat <<'BODY'
## Problem

`GetSubscribersForChokepoint` matches `target_type='zone'` against `cam.Zone`, a traffic-zone value, while migration `0006` documents `zone` as a district and `notify.FloodAlert` names the field `District`. A district-shaped subscription therefore never matches an alert and fails silently. The catalog holds 2025 ward-level boundaries and has no district field, so the advertised district tier does not exist. Carried from Sprint 8 F6.

## Solution

Fix the taxonomy at the contract: traffic zone as the operational geography, ward as the administrative geography, an explicit alias map for legacy district strings, and a contract test that proves delivery.

## Scope / Files

- `services/go-server/internal/subscription/{handler.go,sqlite.go}` (enum validation, taxonomy endpoint)
- `services/go-server/internal/notify/{alert.go,alert_builder.go,telegram.go}` (`District` to `Zone`)
- `services/go-server/internal/store/migrations/0010_subscription_taxonomy.sql` (alias + quarantine tables)
- `services/go-server/cmd/subscriptions-audit/main.go` (classification report)
- `services/go-server/cmd/server/main.go` (wiring)
- `docs/runbooks/telegram_alerts.md`, `README.md`

## Acceptance Criteria

- [ ] `target_type` accepted only from `{all, chokepoint, zone, ward}`; unknown returns 400 with the canonical list.
- [ ] `zone` targets must exist in `/api/zones`; `ward` targets must exist in `/api/phuong`.
- [ ] `GET /api/subscriptions/taxonomy` returns valid targets.
- [ ] Contract tests: zone subscription receives a camera-in-zone alert; ward subscription receives its ward; legacy `Quận 7` resolves to `Quận 7 - Nhà Bè`; orphan rows land in `flood_subscriptions_legacy_0010` with a reason.
- [ ] After migration, zero stored `target_id` values fall outside the canonical set; audit output attached to the PR.
- [ ] `make test-go` and `make swagger` green; runbook wording updated.

## Dependencies

None.

## Estimation (Rule 3)

- Active Effort: 12-16 hours
- Delivery Window: 2.5 working days (2026-11-09 to 2026-11-11)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 3. S10-T3

```bash
gh issue create \
  --title "[go-server + cv-engine] S10-T3: Directional Runtime Ingestion — Registry, Polygon Assignment & Per-Direction Verdicts" \
  --label "backend,tooling,cross-cutting,sprint-10,wave-1" \
  --body "$(cat <<'BODY'
## Problem

S9-T7 created directional `roadway` polygons and a per-split `roadway_regions.json` sidecar, but no runtime consumer exists. `services/cv-engine/server.py` has no direction handling, `internal/forecast` has no per-direction target, and `PCUTotal` is frame-wide. A jammed carriageway is averaged with a free-flowing one, so the asymmetry that motivated #309 is invisible.

## Solution

Move directional geometry into a per-camera runtime registry, assign detections inside cv-engine, and compute per-direction PCU/SI in Go where the calibrated weights already live.

## Scope / Files

- `tools/eval/build_roadway_registry.py` (approved labels to registry, `--verify`)
- `data/roadway_regions/<camera_id>.json` (region_id, direction, polygon, capacity_share, source_label_ids)
- `services/go-server/internal/camera/{catalog.go,registry.go}` (leaf loader and validation)
- `services/go-server/internal/hotset/scheduler.go` (Verdict fields), `internal/cvclient/{cvengine.go,pcu.go}`
- `services/go-server/internal/store/migrations/0008_hotset_directional_history.sql`
- `services/cv-engine/server.py` (optional multipart body, per-region response)
- `tools/forecast/extract_features.py` (direction column compatibility)
- `docs/`, `README.md`

## Acceptance Criteria

- [ ] Registry rejects a camera whose `capacity_share` does not sum to 1.0 +/- 0.01; there is no implicit 50/50 split.
- [ ] `POST /predict` accepts `multipart/form-data` (`frame` + `meta.regions`) and still accepts the legacy `application/octet-stream` body unchanged.
- [ ] Response adds per-region `vehicle_counts` and `water_ratio`; per-region water ratios reconcile with the frame mask.
- [ ] A two-carriageway fixture with direction A jammed and B free yields frame severity from A and distinct A/B SI.
- [ ] Migration 0008 is additive, idempotent, backfills existing rows with `direction=''`, and preserves frame-wide queries.
- [ ] Three pilot corridors are labelled with both directions and show distinct A/B SI on staging.
- [ ] `go test -race ./...`, the cv-engine suite and `make swagger` are green.

## Dependencies

S10-T1.

## Estimation (Rule 3)

- Active Effort: 22-26 hours
- Delivery Window: 4 working days (2026-11-11 to 2026-11-17)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 4. S10-T4

```bash
gh issue create \
  --title "[ops + go-server] S10-T4: History Warmup, Coverage API & 72-Hour Flywheel Soak" \
  --label "backend,tooling,cross-cutting,sprint-10,wave-1" \
  --body "$(cat <<'BODY'
## Problem

`hotset_verdict_history` exists only after boot migrations run, and nothing asserts it. The forecast provenance records 0 live rows on 2026-11-05, so every camera answers 422 and the only passing gate used a synthetic fixture. The harvester status has no missed-slot or coverage signal, the runbook cited by S9-T3 is absent, and the history table has no retention policy.

## Solution

Assert the schema before the first verdict, expose contiguous coverage, add a missed-slot watchdog, and run a 72-hour staging soak that produces archived evidence and a real-data refit.

## Scope / Files

- `services/go-server/internal/store/{store.go,history_repo.go}` (schema assertion, retention)
- `services/go-server/internal/hotset/handler.go` (coverage endpoint), `cmd/server/main.go` (boot ordering, retention job)
- `services/go-server/internal/harvester/{stats.go,handler.go,coordinator.go}` (`missed_slots`, `last_success_at`, `history_coverage_hours`)
- `tools/forecast/{extract_features.py,backtest.py}` (real-data artifacts)
- `docs/runbooks/staging_harvester_flywheel.md`, `docs/proofs/` (72-hour log), `docs/reports/forecast_prototype_v2.md`

## Acceptance Criteria

- [ ] Boot fails with a named error when the history table or a required column is missing; `/healthz` reports `schema.history_ready`.
- [ ] `GET /api/v1/hotset/history/coverage` returns first/last observation, rows, coverage ratio, longest gap, per-horizon eligibility and per-direction counts.
- [ ] A gap fixture marks the affected horizon ineligible.
- [ ] 72-hour staging log covers three fixed slots and at least one rain-triggered run; missed slots beyond two intervals are surfaced as warnings.
- [ ] The runbook is committed with the acceptance log.
- [ ] A real-data refit ships with `dataset_sha256` matching the committed report; no synthetic model is served.
- [ ] The 30-day retention job is batched, logged and tested.

## Dependencies

S10-T1; directional rows require S10-T3.

## Estimation (Rule 3)

- Active Effort: 10-12 hours
- Delivery Window: 3 working days plus a 72-hour observation window (2026-11-12 to 2026-11-20)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 5. S10-T5

```bash
gh issue create \
  --title "[go-server + tooling] S10-T5: Weather & Precipitation Feature Store v2 with Ablation" \
  --label "backend,tooling,sprint-10,wave-2" \
  --body "$(cat <<'BODY'
## Problem

Feature store v1 carries `lag_5m`, `lag_10m`, `lag_15m`, `rolling_mean_15m`, `hour_of_day` and `day_of_week`. Open-Meteo and RainViewer are called only through an in-memory TTL cache, so precipitation, rain state and wet-surface slowdown are absent from the model. The ridge fit cannot separate a rain event from a normal peak, and no ablation has tested whether rain matters.

## Solution

Persist weather observations on a fixed cadence, join them as-of with an age bound and no forward fill, add weather and directional columns, and run a three-way ablation on one eligible set.

## Scope / Files

- `services/go-server/internal/store/migrations/0009_weather_observations.sql`
- `services/go-server/internal/weather/{service.go,recorder.go}`, `internal/store/weather_repo.go`
- `services/go-server/cmd/server/main.go` (10-minute sampler)
- `tools/forecast/{extract_features.py,backtest.py}`
- `docs/reports/forecast_feature_store_v2.md`, `docs/reports/forecast_ablation_v2.md`

## Acceptance Criteria

- [ ] `weather_observations` receives rows at a 10-minute cadence for 24 hours, independent of the HTTP cache TTL.
- [ ] As-of join tests cover the 30-minute boundary and a null case; no forward fill anywhere.
- [ ] Feature v2 adds `precip_mm_15m`, `precip_mm_60m`, `rain_flag`, `hours_since_rain`, `weather_code_class`, `radar_active`, `direction`, `peer_si`, `si_delta_ab`.
- [ ] Ablation reports A (v1), B (+weather) and C (+directional) on one eligible set with per-horizon and per-direction MAE.
- [ ] Weather is declared kept only if B improves one horizon by >= 2% without degrading any horizon by > 5%; otherwise the null result is recorded and the columns stay unused.
- [ ] RainViewer is documented as a city-scale indicator, not per-camera reflectivity.
- [ ] `make test-forecast` and `make test-go` green.

## Dependencies

S10-T3, S10-T4.

## Estimation (Rule 3)

- Active Effort: 14-18 hours
- Delivery Window: 3 working days (2026-11-16 to 2026-11-20)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 6. S10-T6

```bash
gh issue create \
  --title "[frontend] S10-T6: Short-Horizon Forecast Timeline & Uncertainty Display" \
  --label "frontend,cross-cutting,sprint-10,wave-2" \
  --body "$(cat <<'BODY'
## Problem

No frontend consumes a forecast. Issue #302 specified `/api/v1/cameras/hotset/forecast` while S9-T6 implemented `/api/forecast`, and the payload has no availability signal, so a client cannot distinguish "no data" from "not validated". Exposing the prototype directly would calcify the divergence and show fabricated confidence.

## Solution

Build the timeline against the canonical multi-horizon contract only, gate rendering on the server-reported availability, and display uncertainty rather than a single number.

## Scope / Files

- `apps/web/src/types/forecast.ts` (DTO)
- `apps/web/src/utils/forecastApi.ts` (fetch plus runtime shape validation)
- `apps/web/src/hooks/useForecast.ts` (abortable, 60 s refresh while open)
- `apps/web/src/components/ForecastTimeline.tsx` (timeline, band, states, bilingual copy)
- `apps/web/src/components/CameraModal.tsx`, `apps/web/src/config/constants.ts`
- `apps/web/src/utils/forecast.test.ts`, component tests

## Acceptance Criteria

- [ ] Renders 10/20/30-minute horizons with the 95% band, current SI, severity chips and model provenance.
- [ ] `availability` values `ok`, `insufficient_history`, `model_unvalidated` and `stale` each render a distinct state; no numeric prediction appears unless `ok`.
- [ ] `stale` dims the series and states the observation age.
- [ ] Copy is bilingual through a `FORECAST_I18N` constant and a `lang` prop; no i18n dependency added.
- [ ] Minimum 48 px touch targets, visible keyboard focus, `prefers-reduced-motion` respected, zero emoji.
- [ ] Checked at 390 px and desktop widths; screenshots of `ok` and `insufficient_history` committed under `docs/proofs/`.
- [ ] `npm test --prefix apps/web` green; no reference to `/api/forecast` remains.

## Dependencies

S10-T1, S10-T4.

## Estimation (Rule 3)

- Active Effort: 14-18 hours
- Delivery Window: 3 working days (2026-11-18 to 2026-11-24)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 7. S10-T7

```bash
gh issue create \
  --title "[tooling + cv-engine] S10-T7: Segmentation Dataset v2 Expansion & Retraining Gate" \
  --label "tooling,cross-cutting,sprint-10,wave-2" \
  --body "$(cat <<'BODY'
## Problem

Dataset v1 holds 289 annotated frames of 550 harvested; 261 were skipped for missing geometry and the reason is unrecorded. Stratification is thin (100 rain/wet, 120 dusk/night) and directional roadway truth sits in 61 labels. ADR-011 D4 targeted at least 500 masked frames, and retraining at this volume cannot separate improvement from noise.

## Solution

Annotate the skipped and rain-stratified frames to the target volume, audit the skip reasons, enforce a group-aware split, and gate v2 against v1 on the same held-out gold split. Publish a negative result rather than loosen the gate.

## Scope / Files

- `tools/eval/{export_yolo_seg.py,seg_dataset_v2_manifest.json,yolo_seg_dataset_v2.yaml}`
- `tools/seg/prelabel_sam.py` (batch provenance and review queue)
- `tools/train/train_yolov8_seg.py` (fixed seed, manifest hash)
- `docs/reports/seg_eval_v2.md`, `docs/reports/seg_dataset_v2_audit.md`
- `models/` (OpenVINO FP16 export), rollback note in the runbook

## Acceptance Criteria

- [ ] At least 550 annotated frames with at least 175 rain/wet, 120 dusk/night and 30 dual-carriageway frames carrying both directions.
- [ ] Every skipped frame records `no_geometry` or `unusable_frame`; the 47% skip rate is explained.
- [ ] The split is grouped by `camera_id` + capture date and verified so no group spans train and validation.
- [ ] Retraining is deterministic from the manifest hash.
- [ ] Gate: `water_puddle` IoU >= 0.612, no class IoU drop > 0.02, mAP50-95 >= 0.496, p95 inside the 30 s / 57-camera budget at 416 px.
- [ ] `docs/reports/seg_eval_v2.md` reports per-class IoU, rain/night strata and a directional sidecar consumer test.
- [ ] Deployment only on a passed gate; a failed gate ships the error analysis and keeps v1.

## Dependencies

S10-T3 (directional strata); labeling may start in Wave 1.

## Estimation (Rule 3)

- Active Effort: 18-24 hours plus 12 hours of annotation review
- Delivery Window: 6 working days, labeling from 2026-11-13, closure 2026-11-25

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 8. S10-T8

```bash
gh issue create \
  --title "[ops + tooling] S10-T8: Inference Worker Co-Tenancy Benchmark & Load-Shedding Policy" \
  --label "tooling,backend,cross-cutting,sprint-10,wave-3" \
  --body "$(cat <<'BODY'
## Problem

One AVX2 host (Ryzen 7 6800HS, 8C/16T, 3.8 GB RAM, 19 GB SSD) runs cv-engine (2 vCPU / 2 GB cgroup), the GHA self-hosted runner and SAM batch pre-labeling at four workers. The p95 14.8 ms figure was measured without concurrent load; the token bucket paces uploads, not CPU. The AVX2 guard prevents the Celeron ingress host from absorbing work.

## Solution

Measure the real co-tenancy envelope, cap workers and pin affinity, schedule batches off-peak, and enforce 320 px / skip-slot degradation in code rather than documentation.

## Scope / Files

- `tools/eval/bench_co_tenancy.py` (four-scenario matrix, p50/p95)
- `tools/seg/prelabel_sam.py` (worker cap, affinity, off-peak guard)
- `services/cv-engine/server.py` (320 px degradation flag honoured at runtime)
- `services/go-server/internal/harvester/coordinator.go` (skip-slot degradation)
- `docker-compose.stg.yml` (CPU limits and SAM worker configuration)
- `docs/reports/co_tenancy_benchmark.md`, `docs/runbooks/inference_worker.md`

## Acceptance Criteria

- [ ] Benchmark matrix covers idle, hot-set sweep only, sweep + four-worker SAM batch, and sweep + SAM + CI; p50/p95 published.
- [ ] A measured serving envelope states the maximum concurrent load that keeps p95 inside the 30 s / 57-camera budget.
- [ ] SAM workers default to a conservative cap with explicit environment configuration and CPU affinity.
- [ ] The 320 px fallback and the skip-slot path are triggered in a live test, not only documented.
- [ ] The staging compose file carries cgroup limits that match the published envelope.
- [ ] The runbook records the decision table and the rollback path.

## Dependencies

S10-T3, S10-T7.

## Estimation (Rule 3)

- Active Effort: 8-10 hours
- Delivery Window: 2 working days (2026-11-23 to 2026-11-26)

## Close Linkage

PR must begin with `Close #<this issue>`. Tracked on ProjectKV.
BODY
)"
```

## 9. After creation

Add each created issue to the ProjectKV board:

```bash
gh project item-add 2 --owner 529-studio --url "$ISSUE_URL"
```
