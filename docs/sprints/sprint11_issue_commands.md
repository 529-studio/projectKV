# Sprint 11 Issue Creation Commands (ADR-013)

**Status: planned for Sprint 11 execution.** Chartered by
[`docs/ADR-013-sprint-11-architecture.md`](../ADR-013-sprint-11-architecture.md).
This file specifies the reproducible issue creation commands for tickets S11-T1 through S11-T8.

---

## Navigation Aid (Table of Contents)

1. [Ticket Overview & Issue Map](#1-ticket-overview--issue-map)
2. [Label Creation](#2-label-creation)
3. [Wave 0: S11-T1](#3-wave-0-s11-t1)
4. [Wave 1: S11-T2 & S11-T3](#4-wave-1-s11-t2--s11-t3)
5. [Wave 2: S11-T4 & S11-T5](#5-wave-2-s11-t4--s11-t5)
6. [Wave 3: S11-T6 & S11-T7](#6-wave-3-s11-t6--s11-t7)
7. [Wave 4: S11-T8](#7-wave-4-s11-t8)

---

## 1. Ticket Overview & Issue Map

| Ticket | Title | Wave | Subsystems | Effort Estimate |
|---|---|:---:|---|:---:|
| **S11-T1** | Evidence Integrity CI Gate & Synthetic Data Prohibition | Wave 0 | `tools/ci`, GitHub Actions | 16–24 h |
| **S11-T2** | 14-Day Unattended Staging Soak & 11,905 Proof Frame Ingestion | Wave 1 | `tools/harvester`, `go-server` | 40–56 h |
| **S11-T3** | Directional Roadway Registry Scale-Out (57 Hot Set Cameras) | Wave 1 | `data/roadway_regions`, `tools` | 40–54 h |
| **S11-T4** | Automated Forecast Refit Pipeline & Live History Model Deployment | Wave 2 | `tools/forecast`, `go-server` | 40–56 h |
| **S11-T5** | Real GPU YOLOv8n-seg Training & 6.4 MB OpenVINO Deployment | Wave 2 | `tools/train`, `models` | 56–72 h |
| **S11-T6** | Commuter Last-Mile Geolocation (GPS 'Near-Me' View) | Wave 3 | `apps/web` (Leaflet, Geo API) | 32–44 h |
| **S11-T7** | Motorbike Flood Stall Hazard Level Classifier (cm Depth) | Wave 3 | `cv-engine`, `go-server` | 48–60 h |
| **S11-T8** | Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI | Wave 4 | `go-server/notify`, `apps/web` | 40–50 h |

Total Active Effort: **312–416 hours** (5–6 Calendar Weeks across 2 engineers).

---

## 2. Label Creation

```bash
gh label create sprint-11 --description "Sprint 11 backlog (ADR-013)" --color "0E8A16" || true
```

---

## 3. Wave 0: S11-T1

```bash
gh issue create \
  --title "[tooling + ci] S11-T1: Evidence Integrity CI Gate & Synthetic Data Prohibition" \
  --label "tooling,cross-cutting,sprint-11,wave-0" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 16–24 hours
- Expected Delivery Window: Day 1–3
- Dependencies: None

## Problem
Sprint 10 reality audit revealed that synthetic fixtures (`synthetic_congestion_v2_5m.csv`) and simulated soak timestamps were accepted to clear release gates. A gate that accepts synthetic input or future timestamps undermines engineering truth (AGENTS.md Rule 2 & 6).

## Solution
1. Add `tools/ci/check_evidence_integrity.py` asserting that no release gate accepts synthetic data (`fit_source == "live_history"`).
2. Validate all soak evidence timestamps against actual execution time: `generated_at >= window.end`.
3. Amend Definition of Done in CI to fail closed if any proof screenshot originates from static HTML mocks instead of live containers.

## Scope / Files
- `tools/ci/check_evidence_integrity.py`
- `.github/workflows/ci.yml`
- `AGENTS.md` (DoD amendment)

## Acceptance Criteria
[ ] CI rejects any PR whose evidence artifact has `source: synthetic`.
[ ] CI asserts `generated_at >= window.end` on windowed soak reports.
[ ] Unit tests verify that below-threshold inputs fail closed.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

---

## 4. Wave 1: S11-T2 & S11-T3

```bash
gh issue create \
  --title "[ops + harvester] S11-T2: 14-Day Unattended Staging Soak & 11,905 Proof Frame Ingestion" \
  --label "ops,backend,sprint-11,wave-1" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 40–56 hours
- Expected Delivery Window: Day 3–10 (continuous soak runs in background)
- Dependencies: S11-T1

## Problem
`hotset_verdict_history` currently holds 0 rows in staging. Sprint 10's 72h soak was simulated. Without continuous live history, forecast models cannot be fitted on real Saigon traffic, locking `/forecast` at 503 `model_unvalidated`.

## Solution
1. Deploy 24/7 autonomous 5-minute harvester service on staging host across all 57 Hot Set cameras.
2. Ingest 11,905 historical frames (1.4 GB) from `services/go-server/data/proofs/2026-11-03` into the replay dataset.
3. Expose live verification endpoint and emit telemetry on unbroken soak duration.

## Scope / Files
- `tools/harvester/service.py`
- `docker-compose.stg.yml`
- `tools/eval/ingest_proof_archive.py`

## Acceptance Criteria
[ ] Continuous 14-day history accumulated in `hotset_verdict_history` without write starvation.
[ ] Coverage API reports $\ge 90\%$ bin completeness over 14 contiguous days.
[ ] 11,905 proof frames ingested into labeling/replay pipeline.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

```bash
gh issue create \
  --title "[tooling + labeling] S11-T3: Directional Roadway Registry Scale-Out (57 Hot Set Cameras)" \
  --label "tooling,backend,sprint-11,wave-1" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 40–54 hours
- Expected Delivery Window: Day 5–14
- Dependencies: S11-T1

## Problem
Only 3 of 57 Hot Set cameras (5.26%) have directional polygons in `data/roadway_regions/`. The remaining 54 cameras dilute asymmetric jams by averaging across both directions.

## Solution
1. Annotate dual-direction polygons (`dir_a`, `dir_b`) and capacity shares (`share_a + share_b = 1.0`) for all remaining 54 Hot Set cameras using `/labeler`.
2. Validate polygon non-overlap and geometry invariants via `tools/eval/build_roadway_registry.py`.
3. Ingest into `services/go-server/internal/camera` and verify worst-direction severity pinning.

## Scope / Files
- `data/roadway_regions/*.json`
- `tools/eval/build_roadway_registry.py`
- `services/go-server/internal/camera/`

## Acceptance Criteria
[ ] 57/57 Hot Set cameras (100%) have valid directional roadway geometries.
[ ] All capacity shares sum to $1.0 \pm 0.01$.
[ ] Zero asymmetric jam dilution on arterial corridors (`cong_hoa`, `truong_chinh`).

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

---

## 5. Wave 2: S11-T4 & S11-T5

```bash
gh issue create \
  --title "[go-server + forecast] S11-T4: Automated Forecast Refit Pipeline & Live History Model Deployment" \
  --label "backend,sprint-11,wave-2" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 40–56 hours
- Expected Delivery Window: Day 12–20
- Dependencies: S11-T2, S11-T3

## Problem
`GET /api/v1/cameras/:id/forecast` is permanently fail-closed with 503 `model_unvalidated` because no automated pipeline fits weights to real traffic history.

## Solution
1. Build `tools/forecast/train_live_model.py` scheduled to run nightly on staging.
2. Extract chronological features from `hotset_verdict_history` joined as-of with `weather_observations`.
3. Fit Ridge regression models, evaluate on held-out test splits, and promote verified model artifacts atomically to `services/go-server/internal/forecast/embedded/`.
4. Unlock the forecast endpoint with `availability: ok`.

## Scope / Files
- `tools/forecast/train_live_model.py`
- `services/go-server/internal/forecast/artifact.go`
- `services/go-server/internal/forecast/handler.go`

## Acceptance Criteria
[ ] Live forecast models fitted strictly on real HCMC traffic history (`fit_source == "live_history"`).
[ ] Endpoint returns HTTP 200 with `availability: "ok"` and valid 95% CI bands.
[ ] Candidate model passes chronological backtest beating persistence baseline.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

```bash
gh issue create \
  --title "[cv-engine + tooling] S11-T5: Real GPU YOLOv8n-seg Training & 6.4 MB OpenVINO Deployment" \
  --label "tooling,cross-cutting,sprint-11,wave-2" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 56–72 hours
- Expected Delivery Window: Day 15–24
- Dependencies: S11-T1, S11-T2

## Problem
The committed model in `models/yolov8n_seg_openvino_416/` is a 64 KB stub of repeating null bytes. No real segmentation weights run on the worker.

## Solution
1. Train `yolov8n-seg` on authentic 550+ frame dataset v2 on dedicated GPU instance.
2. Enforce empirical gate: held-out `water_puddle` IoU $\ge 0.612$, Mask mAP50-95 $\ge 0.496$.
3. Export verified 6.4 MB OpenVINO FP16 model and replace the stub in `models/yolov8n_seg_openvino_416/`.

## Scope / Files
- `tools/train/train_yolov8_seg.py`
- `models/yolov8n_seg_openvino_416/`
- `docs/reports/seg_eval_v3.md`

## Acceptance Criteria
[ ] Exported `yolov8n_seg.bin` has real neural network weights ($> 6.0\text{ MB}$).
[ ] OpenVINO runtime compiles model graph successfully on AVX2 host.
[ ] Quantitative benchmark gate clears IoU $\ge 0.612$ on real validation images.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

---

## 6. Wave 3: S11-T6 & S11-T7

```bash
gh issue create \
  --title "[frontend] S11-T6: Commuter Last-Mile Geolocation (GPS 'Near-Me' Flood Proximity View)" \
  --label "frontend,cross-cutting,sprint-11,wave-3" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 32–44 hours
- Expected Delivery Window: Day 20–26
- Dependencies: S11-T1

## Problem
Soaked commuters navigating Saigon in torrential downpours have zero geolocation support in `apps/web`. The map requires manual pinch-and-zoom to locate nearby flooded junctions.

## Solution
1. Add a prominent 1-click "Quanh tôi / Near Me" button using `navigator.geolocation` (with permission handling and fallback).
2. Center map on user location and render a compact bottom sheet displaying cameras within a 1.5 km radius sorted by distance and flood severity.
3. Ensure touch targets meet the 48px ergonomic minimum for wet screens.

## Scope / Files
- `apps/web/src/hooks/useGeolocation.ts`
- `apps/web/src/components/NearMeModal.tsx`
- `apps/web/src/utils/geoDistance.ts`

## Acceptance Criteria
[ ] 1-Click GPS button centers map and identifies nearest cameras within 1.5 km.
[ ] Graceful fallback when location permission is denied.
[ ] Bilingual strings (`vi` default with diacritics, `en` secondary) and zero emojis.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

```bash
gh issue create \
  --title "[cv-engine + go-server] S11-T7: Motorbike Flood Stall Hazard Level Classifier (cm Depth)" \
  --label "backend,cross-cutting,sprint-11,wave-3" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 48–60 hours
- Expected Delivery Window: Day 22–30
- Dependencies: S11-T5

## Problem
Current flood telemetry reports abstract area ratios (`water_ratio: 0.25`). Motorcyclists in HCMC need physical safety guidance: "Will my exhaust pipe submerge and stall my engine?".

## Solution
1. Calibrate camera water depth estimation using physical references (kerb heights, wheel rims, barrier steps) in addition to vehicle submersion.
2. Classify flood severity into 3 physical categories:
   - Level 1: Bấp bênh / Bắn nước ($< 10\text{ cm}$, safe)
   - Level 2: Cảnh báo xe ga gầm thấp ($10 - 20\text{ cm}$, caution)
   - Level 3: Nguy cơ chết máy cao ($> 20\text{ cm}$, stall danger / do not enter)
3. Expose hazard category on `/verdicts` and render prominent warning badges on web map.

## Scope / Files
- `services/cv-engine/server.py`
- `services/go-server/internal/flood/`
- `apps/web/src/components/CameraModal.tsx`

## Acceptance Criteria
[ ] Flood severity classifies into 3 physical depth tiers based on physical calibration.
[ ] Eliminates false `FLOODED_SHALLOW` when traffic stops outside deep flood water.
[ ] Clear warning badges rendered on web UI with zero emojis.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```

---

## 7. Wave 4: S11-T8

```bash
gh issue create \
  --title "[ops + frontend] S11-T8: Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI" \
  --label "ops,frontend,backend,sprint-11,wave-4" \
  --body "$(cat <<'BODY'
## Estimation (AGENTS.md Rule 3)
- Active Effort: 40–50 hours
- Expected Delivery Window: Day 26–35
- Dependencies: S11-T6, S11-T7

## Problem
Telegram alerting remains in simulation mode because the bot token (#246) is unprovisioned. Furthermore, `apps/web` has zero subscription UI, preventing citizens from registering for alerts.

## Solution
1. Provision live Telegram Bot token in Stack 14 environment configuration.
2. Build interactive subscription modal in `apps/web` allowing commuters to select wards or arterial corridors.
3. Integrate Telegram deep-link activation (`t.me/WardedAlertBot?start=sub_<id>`) for 1-click subscription.
4. Verify end-to-end alert delivery on live smartphones during simulated flood events.

## Scope / Files
- `services/go-server/internal/notify/`
- `apps/web/src/components/SubscriptionModal.tsx`
- `.env.example`, `docker-compose.stg.yml`

## Acceptance Criteria
[ ] Live Telegram Bot dispatches flood alerts to subscribed users.
[ ] Citizen subscription modal delivered on web app with ward and corridor selectors.
[ ] End-to-end alert dispatch verified with empirical proof.

## Close Linkage (AGENTS.md Rule 9)
The implementation PR MUST begin with `Close #<issue>`.
BODY
)"
```
