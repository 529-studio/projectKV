# S12-T3 Functional Specification — Motorbike Flood Stall Hazard Level Classifier (cm Depth)

**Issue**: [#401](https://github.com/529-studio/projectKV/issues/401) · **Sprint**: 12 · **Branch**: `feat/401-flood-hazard-levels`
**Owner**: Product Owner · **Consumers**: Lead Developer, Tester
**Depends on**: `triangulate_flood_verdict`, `calculate_motorcycle_submersion` (ADR-009 §5.2); `hotset.Verdict`, `store.VerdictRepo`; `CameraModal.tsx`
**Charter**: ADR-013 D6, `docs/HANDOFF-s12.md` §6.2, taxonomy §3

---

## 0. Estimation (AGENTS.md Rule 3)

**Total**: **48–60 h active**, Day 22–30 (2026-11-13 → 2026-11-21, 6–7 focused days plus review).

P0 ladder + `calibration.json` loader + ±25 % clamp + fail-closed validation, 8–10 h · P1 depth→level + avoidance escalation + latch, 10–12 h · P2 `/predict` contract + legacy fields + tests, 7–9 h · P3 Go `cvclient`/`hotset.Verdict`/migrations/endpoints/swagger, 8–10 h · P4 web badge + depth meter + VI/EN + tests, 8–10 h · P5 live calibration + Rule-10 evidence + docs + PR, 7–9 h.
**Blocks**: S12-T7 (corridor aggregate), S12-T4 (alert wording). **Does not block**: S12-T5/T6.

---

## 1. Problem

The API exposes an abstract area ratio (`water_ratio: 0.25`) and a severity word. Neither is actionable: 0.25 across a 12 m arterial and 0.25 in a 3 m alley are different dangers, and neither says whether 20 cm of water reaches the exhaust. The only physical number today — `depth_estimated_cm` from `delta_sub × 110 cm` — never reaches the API, the database or the UI.

`server.py:553` then infers from **emptiness**: `motorcycles_observed == 0 and water_ratio >= 0.35 → FLOODED_SHALLOW`. A rider stopping *upstream* of a large pool behaves exactly as a rational agent does when the pool is impassable, so this branch converts evidence of hazard into a clearance.

---

## 2. Decisions (normative)

### 2.1 Reference ladder

The camera is not a ruler, so the ruler must be in the frame. Heights target the HCMC scooter fleet and published curb standards.

| Reference | Height above road | Role |
|:---|:---:|:---|
| Road surface / lane paint | 0 cm | datum |
| Curb top edge (`lề đường`) | **15 cm** (13–17) | primary anchor when no vehicle is in water |
| **Exhaust outlet** (`miệng ống xả`) | **20 cm** (19–22) | stall threshold: L2 → L3 |
| Wheel rim (`vành xe`) | **35–40 cm** | deep-water confirmation, confidence only |
| Motorcycle height (grip → ground) | **110 cm** | scale source when a bike box exists |

Constants: `MOTORCYCLE_HEIGHT_CM = 110.0`, `EXHAUST_HEIGHT_CM = 20.0`, `CURB_HEIGHT_CM = 15.0`.

**Pixels-per-cm resolution** (first available wins, exactly one source per observation):

1. `MOTORCYCLE_110CM` — a motorcycle box inside the ROI: `px_per_cm = box_h_px / 110.0`. The existing `calculate_motorcycle_submersion` path, unchanged.
2. `CURB_15CM` — a curb segment crossing the pool's waterline: `px_per_cm = curb_px / 15.0`.
3. `CAMERA_OVERRIDE` — the §2.2 multiplier applied on top of (1) or (2).
4. `NONE` — no reference. Depth is **not** estimated: `flood_depth_cm: null`, never `0.0`.

`depth_cm = delta_sub × 110.0 × scale_multiplier`, `delta_sub = (box_bottom_y − waterline_y) / box_h` clamped to `[0,1]`.

**Scope in this sprint**: only `MOTORCYCLE_110CM`, `CAMERA_OVERRIDE` and `NONE` are reachable. `CURB_15CM` and `estimate_depth_from_curb_cm` are unit-tested but `/predict` passes `has_curb_reference=False` and no curb segmenter exists yet, so the curb anchor is **deferred to the sprint that lands curb segmentation**. Frames without a bike reference publish `calibration_ref: "NONE"` and a null depth.

### 2.2 Per-camera calibration (physical, auditable)

`services/cv-engine/calibration.json` — operator-authored, committed, credential-free; the repository ships the empty template `{"version": 1, "cameras": {}}`:

```json
{"version":1,"cameras":{"cam_dbp_01":{"scale_multiplier":1.08,
 "measured_by":"operator-id","measured_at":"2026-11-14",
 "frame_ref":"docs/proofs/s12_t3/calib_cam_dbp_01.jpg",
 "landmarks":{"curb_px":94.0,"curb_cm":15.0}}}}
```

Loaded at startup and re-validated on `mtime` change. A malformed file, an entry without `measured_by`/`frame_ref`, or `scale_multiplier` outside `1.0 ± 0.25` is **rejected with a logged error** and the camera falls back to the raw scale — never a failed boot, never a silently bad multiplier (L66). `measured_at` older than 180 days warns.

### 2.3 Classification rule (inclusive at 20.0)

```
LEVEL_1_SPLASH     : 0 <= d < 10.0
LEVEL_2_WARNING    : 10.0 <= d < 20.0
LEVEL_3_STALL_RISK : d >= 20.0
```

20.0 is inclusive because 20 cm is the exhaust outlet: the pipe sits at the waterline and any ripple reaches it. Classify the **unrounded** value; display one decimal, VI comma. `d > 150 cm` is not credible: keep Level 3, set `depth_confidence: "UNMEASURED"`, log a calibration warning, show no centimetre figure.

### 2.4 Avoidance escalation (the false-`SHALLOW` fix)

Replace the static fallback with an explicit inference about *why* the lane is empty:

```
through_traffic  = any motorcycle with delta_sub >= 0.09        // wheels visibly in water
upstream_stop    = any motorcycle with box_bottom_y < pool_top_y and delta_sub <= 0.05
pool_blocks_lane = water_ratio >= 0.30 and pool_area_px >= 400  // ~4 m² at 416 px / ~20 m FOV
escalate         = pool_blocks_lane and not through_traffic
                   and (motorcycles_observed == 0 or upstream_stop)
```

On escalation: `flood_severity = FLOODED_DEEP`, `flood_hazard_level = 3`, `flood_hazard_basis = "AVOIDANCE_INFERRED"`, confidence capped at `0.78`.

- `flood_depth_cm` **stays null when unmeasured**; the classifier never invents a number to justify a level (AC10).
- `through_traffic` suppresses escalation: a rider crossing a 30 % pool at rim depth is proof of passability at Level 2. Over-warning costs the same trust as under-warning.
- The area floor rejects wet-asphalt reflections read as water.

Composition: `final = max(level_from_depth, level_from_avoidance)`; both null → `flood_hazard_level: null`, `basis: "NONE"`. **No default to Level 1**: absence of measurement is not safety.

### 2.5 De-escalation latch

Escalation is immediate; a drop (`level_new < level_prev`) requires **two consecutive agreeing observations**, so one passing truck cannot erase the pool for a frame. State is an in-process `camera_id → (assessment, observed_at)` map, cleared on restart and ignored once older than 180 s (the verdict TTL). A held entry republishes its own `depth_cm`, `basis`, `depth_confidence` and `calibration_ref` — never the drop frame's. `level_from_depth` and `level_from_avoidance` stay pure; the latch is the only stateful part.

---

## 3. Scope / Non-goals

**In scope**: reference ladder; calibration loader; depth confidence; Level 1–3 classifier with the avoidance rule; latch; cv-engine fields; Go decode + fallback; `hotset.Verdict`; migration + round-trip; four endpoints; web badge + depth meter + VI/EN; tests; live evidence.

**Out of scope**: car/truck clearance profiles; per-vehicle-type levels; hydrological modelling; new segmentation classes or retraining (S12-T5); hex aggregation (S12-T7); push alerts (S12-T4); routing; new dependencies.

---

## 4. User stories & acceptance criteria

### US-401.1 — "Will my exhaust go under?"
- **AC1** The camera view shows a hazard block: level label, one-line rider instruction, and the depth in cm when measured (`14,5 cm` / `14.5 cm`).
- **AC2** Depth names its reference (`MOTORCYCLE_110CM`, `CURB_15CM`, `CAMERA_OVERRIDE`); without one the UI says the depth is unmeasured and shows no number.
- **AC3** Boundaries are exactly `<10`, `10–20`, `>=20`: `19.99 → Level 2`, `20.0 → Level 3`.

### US-401.2 — Three physical categories, one glance
- **AC4** Level 1 `Bấp bênh / Bắn nước`, Level 2 `Cảnh báo xe ga gầm thấp`, Level 3 `Nguy cơ chết máy cao`; EN copy in §6.3.
- **AC5** Level is encoded by text, an inline SVG glyph and a 3-segment meter, never colour alone; monochrome `zinc`, Level 3 inverted for contrast.
- **AC6** The badge is visible without scrolling at 390 × 844 when the modal opens.

### US-401.3 — Traffic stopping upstream is a warning
- **AC7** `water_ratio >= 0.30` with no through-traffic and zero motorcycles or an upstream stop returns `flood_hazard_level: 3`, `flood_hazard_basis: "AVOIDANCE_INFERRED"`, `flood_severity: "FLOODED_DEEP"`.
- **AC8** That frame must not return `FLOODED_SHALLOW`; the static fallback branch is deleted and its unit test rewritten to assert the new outcome.
- **AC9** A rider crossing at rim depth (`delta_sub >= 0.09`) stays Level 2; patches under 400 px are never escalated.
- **AC10** The UI renders the avoidance sentence, not a fabricated centimetre value.

### US-401.4 — Backwards-compatible contract
- **AC11** `flood_depth_cm` and `flood_hazard_level` appear on `/api/traffic-status`, `/api/flood-status`, `/api/v1/cameras/hotset/status` and the `/api/cameras/hotset/status` alias.
- **AC12** Every pre-existing key keeps its name, type and meaning; clients ignoring the new keys see zero change.
- **AC13** Absent measurement serialises as omitted/null, never `0`; a measured dry `0.0` is preserved.
- **AC14** A verdict persisted before migration 0011 reads back with null hazard fields and renders the legacy flood badge, not Level 1.

### US-401.5 — Bilingual, monochrome, honest
- **AC15** Every string exists in `HAZARD_I18N` in VI (full diacritics, default) and EN, zero emoji; icons are inline SVG.
- **AC16** Switching language re-renders labels, advice and the decimal separator with no refetch.
- **AC17** The block states observation age; a verdict older than 180 s dims it and shows `Dữ liệu cũ / Stale data`.

---

## 5. Wire contracts

### 5.1 cv-engine `POST /predict` (additive)

```json
{"flood_hazard_level":3,"flood_hazard_basis":"AVOIDANCE_INFERRED",
 "flood_depth_confidence":"UNMEASURED","calibration_ref":"CURB_15CM","flood_depth_cm":null}
```

`flood_assessment` mirrors them as `hazard_level`, `hazard_basis`, `depth_confidence` and `calibration_ref`; an unmeasured depth is `null` at both levels (AC13). Every legacy key is unchanged, `max_submersion_depth_cm` included; the block comes from the extracted `build_flood_assessment` helper so assertions hit production code (L66).

- `flood_hazard_basis` ∈ `DEPTH_MEASURED | AVOIDANCE_INFERRED | DEPTH_AND_AVOIDANCE | NONE`
- `flood_depth_confidence` ∈ `MEASURED | INFERRED | UNMEASURED`
- `calibration_ref` ∈ `MOTORCYCLE_110CM | CURB_15CM | CAMERA_OVERRIDE | NONE`

### 5.2 `internal/hotset/scheduler.go` — `Verdict`

```go
FloodDepthCm         *float64 `json:"flood_depth_cm,omitempty"`         // nil unmeasured; 0.0 measured dry
FloodHazardLevel     *int     `json:"flood_hazard_level,omitempty"`     // 1|2|3; nil unknown
FloodHazardBasis     string   `json:"flood_hazard_basis,omitempty"`
FloodDepthConfidence string   `json:"flood_depth_confidence,omitempty"`
FloodCalibrationRef  string   `json:"flood_calibration_ref,omitempty"`
```

Pointers are mandatory: `omitempty` on a bare `float64` would erase a measured `0.0`, making "dry, verified" indistinguishable from "never measured". Because all three response structs marshal `Verdict`, one tag change satisfies AC11 with no per-handler logic.

### 5.3 `internal/cvclient/cvengine.go`

Decode the five fields and pass them through verbatim — no recomputation, no clamping. Rolling-deploy fallback: level absent **and the whole hazard block absent** (`flood_hazard_basis`, `flood_depth_confidence`, `calibration_ref` all empty) but `depth_estimated_cm` present → derive with §2.3, `basis: "DEPTH_MEASURED"`, `confidence: "INFERRED"`. A frame that explicitly declines to measure (level null with `basis: "NONE"`) is carried verbatim: nil stays nil. Both absent (VLM, mock, `unknownVerdict`) → nil pointers. The Go layer **must not** derive a level from `water_ratio`: it is an area ratio, not a depth, and manufacturing a level from it is the defect being fixed.

### 5.4 Migrations `0011_add_flood_hazard.sql` and `0012_add_flood_calibration_ref.sql`

```sql
-- 0011
ALTER TABLE camera_verdicts ADD COLUMN flood_depth_cm REAL;
ALTER TABLE camera_verdicts ADD COLUMN flood_hazard_level INTEGER;
ALTER TABLE camera_verdicts ADD COLUMN flood_hazard_basis TEXT NOT NULL DEFAULT '';
ALTER TABLE camera_verdicts ADD COLUMN flood_depth_confidence TEXT NOT NULL DEFAULT '';

-- 0012
ALTER TABLE camera_verdicts ADD COLUMN flood_calibration_ref TEXT NOT NULL DEFAULT '';
```

`Save` writes them; `GetLatest` scans with `sql.NullFloat64`/`sql.NullInt64` and `COALESCE` for text, projecting to pointers. `flood_calibration_ref` is persisted verbatim so a restart rehydrates the ladder string the engine published with the level. It gets its own version because `runMigrations` skips a recorded version: extending the applied 0011 would leave those databases without the column the boot-time schema assertion requires. `hotset_verdict_history` is **not** extended: the level is derivable from depth and avoidance is too volatile a label. Non-idempotent or backfilling migrations are rejected by review.

### 5.5 Endpoint payload

Each verdict object in `verdicts[]` gains the five keys, `flood_calibration_ref` being cv-engine's `calibration_ref` re-keyed by the Go client; every legacy key (`severity`, `confidence`, `source`, `observed_at`, `stale`) is unchanged (AC12).

`make swagger` must regenerate `docs/docs.go` or the API gate fails.

---

## 6. Web UI

### 6.1 `src/utils/hazardUtils.ts` (new, pure)

```ts
export type HazardLevel = 1 | 2 | 3;
export type HazardBasis = 'DEPTH_MEASURED'|'AVOIDANCE_INFERRED'|'DEPTH_AND_AVOIDANCE'|'NONE';
export type DepthConfidence = 'MEASURED'|'INFERRED'|'UNMEASURED';
export function levelFromDepthCm(depthCm: number): HazardLevel;   // <10→1, <20→2, else 3
export function resolveHazard(v?: VerdictLike|null): {
  level: HazardLevel|null; basis: HazardBasis; depthCm: number|null;
  depthConfidence: DepthConfidence; isUnmeasured: boolean } | null;  // null = pre-contract verdict
export function formatDepthCm(depthCm: number|null, lang: 'vi'|'en'): string | null;
```

`resolveHazard` prefers `flood_hazard_level`; when absent but `flood_depth_cm` is finite it derives via `levelFromDepthCm` (older Go build during rollout); otherwise it returns `null` so AC14 holds. It never infers from `water_ratio` and never returns Level 1 for missing data. `formatDepthCm(0,…)` → `0 cm`; `(14.5,'vi')` → `14,5 cm`; `(null,…)` → `null`.

Add the five optional snake_case fields (plus camelCase aliases handled in `normalizeVerdict`) to `TrafficVerdict` in `src/utils/trafficUtils.ts`.

### 6.2 `CameraModal.tsx` — hazard block

Placed below the header badge row, before the metrics grid, on the existing `zinc-950` surface, ordered 3-segment meter → `<h3>` label → one-line advice → `14,5 cm · đo theo lề đường 15 cm` (Level 3 inverts to zinc-100 fill, zinc-950 text). It **replaces** the legacy flood badge when `resolveHazard()` is non-null, or a coloured `FLOODED_DEEP` chip and a monochrome hazard chip would contradict each other. Null keeps legacy rendering (AC14).

- Container `rounded-lg border px-3 py-2.5`. Tokens: L1 `border-zinc-700 bg-zinc-900/80 text-zinc-300`; L2 `border-zinc-400 bg-zinc-800 text-zinc-100`; L3 `border-zinc-100 bg-zinc-100 text-zinc-950`. No hue, no emoji, `aria-hidden` inline SVG at `3.5` w/h.
- 3-segment meter (`h-1.5 w-5` bars filled to the level) is part of the text alternative; the depth meter (`0–30+ cm`, ticks `0 / 10 / 20`, `role="img"` + value `aria-label`) is hidden when unmeasured.
- Unmeasured + avoidance: level label, avoidance sentence, `Chưa đo được độ sâu / Depth not measurable`; **no number and no meter**.
- Header carries a compact `EN`/`VI` toggle beside close (`aria-label` names the target language); it re-renders labels, advice and the decimal separator in place and reports `onLangChange` to the page — no refetch (AC16/M10).
- `role="status" aria-live="polite"`, real `<h3>`, contrast ≥ 4.5:1, nothing encoded by colour alone, 320 px safe.

### 6.3 `HAZARD_I18N` (`src/config/constants.ts`)

Zero emoji, VI default with full diacritics; function values take the same argument in both languages.

```ts
export const HAZARD_I18N = Object.freeze({
  LEVEL_1: { vi:'Bấp bênh / Bắn nước', en:'Splash zone — low risk' },
  LEVEL_2: { vi:'Cảnh báo xe ga gầm thấp', en:'Caution — low-clearance scooters' },
  LEVEL_3: { vi:'Nguy cơ chết máy cao', en:'High stall risk — do not enter' },
  LEVEL_1_ADVICE: { vi:'Nước dưới 10 cm. Vẫn có thể đi qua nhưng nên giảm tốc độ và tránh bắn nước.',
    en:'Water is under 10 cm. You can pass, but slow down and avoid splashing others.' },
  LEVEL_2_ADVICE: { vi:'Nước ngập 10–20 cm, sát miệng ống xả. Xe ga gầm thấp nên tránh hoặc đi thật chậm.',
    en:'Water is 10–20 cm, level with the exhaust outlet. Low-clearance scooters should avoid or go very slowly.' },
  LEVEL_3_ADVICE: { vi:'Nước trên 20 cm, nguy cơ chết máy và hỏng động cơ. Không nên đi vào.',
    en:'Water is over 20 cm. Engine stall and damage risk. Do not enter.' },
  AVOIDANCE_ADVICE: { vi:'Nước chiếm phần lớn lòng đường và dòng xe đang dừng lại phía trước. Không nên đi vào.',
    en:'Water covers most of the roadway and traffic is stopping upstream. Do not enter.' },
  DEPTH_UNMEASURED: { vi:'Chưa đo được độ sâu', en:'Depth not measurable' },
  REF_MOTORCYCLE: { vi:'đo theo chiều cao xe máy 110 cm', en:'measured against the 110 cm motorcycle height' },
  REF_CURB: { vi:'đo theo lề đường 15 cm', en:'measured against the 15 cm curb' },
  REF_OVERRIDE: { vi:'đo theo hiệu chuẩn riêng của camera', en:'measured with this camera’s own calibration' },
  METER_ARIA: { vi:(cm:string)=>`Độ sâu nước ${cm}, thang từ 0 đến 30 cm`,
    en:(cm:string)=>`Water depth ${cm}, scale 0 to 30 cm` },
  LEVEL_ARIA: { vi:(n:number)=>`Mức nguy hiểm ${n} trên 3`, en:(n:number)=>`Hazard level ${n} of 3` },
  STALE: { vi:'Dữ liệu cũ', en:'Stale data' },
});
```

`LEVEL_1_ADVICE` may say "can pass" only because a depth exists; `AVOIDANCE_ADVICE` deliberately contains no number.

---

## 7. Edge cases (normative)

| # | Condition | Behaviour |
|:--:|:---|:---|
| E1 | No reference object | `flood_depth_cm: null`, `UNMEASURED`; level only via avoidance, else null |
| E2 | Depth exactly 10.0 / 20.0 | 10.0 → L2; 20.0 → L3 (inclusive) |
| E3 | Measured dry road | `flood_depth_cm: 0`, Level 1, `DRY`; UI shows `0 cm` |
| E4 | ≥30 % pool, traffic stopped upstream | L3, `AVOIDANCE_INFERRED`, no cm figure |
| E5 | Riders crossing at rim depth | `through_traffic` suppresses escalation |
| E6 | Patch under 400 px | never escalated; WET/DRY as before |
| E7 | Empty road, 30 % pooled | escalate: a blocked lane is a hazard regardless of why it is empty |
| E8 | Depth > 150 cm | L3 retained, `UNMEASURED`, no cm displayed, warning logged |
| E9 | Multiplier outside 1.0 ± 0.25 | entry rejected, raw scale used, error logged, serving continues |
| E10 | Missing/malformed `calibration.json` | defaults only; boot never fails |
| E11 | Malformed multipart metadata | legacy single-frame path, no regression |
| E12 | Old cv-engine image | derive from `depth_estimated_cm`, `INFERRED` |
| E13 | VLM / cloud / mock verdict | nil pointers; legacy badge (AC14) |
| E14 | Verdict older than 180 s | block dimmed + `STALE`; level retained |
| E15 | De-escalation across one frame | previous level held until a second agreeing observation |
| E16 | `9.99 / 19.99` vs `10.01 / 20.01` | no rounding before classification |
| E17 | Language switch, modal open | labels, advice and decimal separator switch, no refetch |
| E18 | 320 px viewport, long EN text | block wraps, meter shrinks, nothing clipped |
| E19 | Reduced motion / screen reader | no animated fill; level and meter fully announced |
| E20 | Migration on legacy DB with rows | old rows read null; no backfill, no fake levels |

---

## 8. Tester plan

### 8.1 Automated

**cv-engine** `test_hazard_classifier.py` — `python3 -m unittest discover`:
- `level_from_depth` on `0, 9.99, 10.0, 19.99, 20.0, 20.01, 150.0` → `1,1,2,2,3,3,3`.
- Ladder: motorcycle 110 cm; curb 15 cm; `1.08` verified against a hand-computed depth; `1.25` rejected.
- Avoidance: `0.30` + no bikes → L3/DEEP; `0.29` and `pool_area_px 399` → none; upstream stop → escalate; through-traffic (`delta_sub 0.12`) → no escalate.
- **No path returns `FLOODED_SHALLOW`** for `water_ratio >= 0.30` without through-traffic; the old static-fallback test is rewritten, not deleted (AC8).
- Unmeasured → `flood_depth_cm is None`, never `0.0`; `build_flood_assessment` parity with the live payload.
- Latch: 3→1 held with the held basis/depth/confidence/ref; 1→3 immediate; a stale entry ignored.

**Go** — `make test-go`:
- `cvclient`: full decode; no hazard keys → nil; derive-from-depth fallback; VLM path nil.
- `store`: 0011 + 0012 on a `0001` database; round-trip of nil, `0.0`, `14.5`, levels 1–3 and every `calibration_ref`; history table untouched.
- `hotset`: marshal omits nil, emits `0`/`0.0` when set; asserted across the four handlers.
- `make swagger` diff limited to the new properties; `make docs-lint`, `make evidence-lint` pass.

**Web** — `npm test`: `hazardUtils.test.ts` boundaries, `resolveHazard` null for pre-contract and derive-when-level-missing, never Level 1 from missing data, `formatDepthCm` VI comma / EN point / zero / null; catalog parity, no `\p{Extended_Pictographic}`, VI diacritics. Gates: `npm test`, `npx tsc --noEmit`, `npm run lint`, `npm run build`; `package.json` unchanged.

### 8.2 Manual — Rule 10 scenarios (live `make dev-all`, real cameras)

| ID | Scenario / evidence |
|:--|:---|
| M1 | Real deep frame → Level 3 + cm; badge visible at 390 × 844 unscrolled |
| M2 | Real shallow frame → correct boundary, advice, meter marker matches the number |
| M3 | Dry frame → `0 cm`, Level 1, `DRY`, no fabricated warning |
| M4 | #401 regression frame (traffic upstream of a ≥30 % pool) → L3 `AVOIDANCE_INFERRED`, no cm |
| M5 | Riders crossing a 30 % pool → no escalation (over-warning guard) |
| M6 | All four endpoints return identical new fields (`jq` capture) |
| M7 | Pre-migration DB copy → legacy verdicts keep the legacy badge |
| M8 | `scale_multiplier: 1.4` → rejected, error logged, service serving |
| M9 | No reference object → unmeasured copy, no number, aria correct |
| M10 | VI ↔ EN with modal open → strings and decimal separator switch |
| M11 | Verdict older than 180 s → dimmed + `Dữ liệu cũ`, level retained |
| M12 | Keyboard + screen reader → announced once, contrast ≥ 4.5:1 |
| M13 | Rapid camera switching → latch correct, no badge bleed |
| M14 | `make swagger` + `/swagger/index.html` show the new fields |

Evidence: `docs/reports/s12_t3_manual_evidence.json` + `.md`, captures in `docs/proofs/s12_t3/`, all from live inference (`fit_source == "live_history"`, non-future timestamps, genuine weights) and validated by `python3 tools/ci/check_evidence_integrity.py`. Synthetic fixtures are forbidden as release evidence (ADR-013 D1, L77).

### 8.3 Definition of Done

- [ ] Classifier, loader and latch match §2; constants exported, not inlined.
- [ ] No Level 1 for unmeasured data; no fabricated cm under avoidance.
- [ ] Five fields on all four endpoints, legacy keys unchanged, swagger clean; migrations idempotent; nil vs `0.0` distinct end-to-end.
- [ ] Badge monochrome, emoji-free, bilingual with diacritics, legible at 320 px, ≥ 4.5:1.
- [ ] All suites, typecheck, lint and build clean, no new dependency; M1–M14 evidence attached; docs synced (§9); PR starts `Close #401`, targets `dev`.

---

## 9. Documentation impact (Rule 6)

- `docs/taxonomy_traffic_and_flood_labeling.md` §3: cm hazard ladder as the physical layer above `NONE/SHALLOW/DEEP`; severity enum unchanged, new fields additive.
- `docs/GO_BACKEND_ARCHITECTURE.md`: `hotset.Verdict` field table, `FloodHazardBasis` vocabulary, migrations `0011`/`0012`.
- `docs/SYSTEM_DESIGN.md`: a paragraph on the ladder and why upstream avoidance evidences hazard.
- `docs/reports/sprint12_report.md` at close: S12-T3 evidence summary.
- `docs/lessons/L86-physical-depth-hazard-classification.md` + one `docs/lessons/INDEX.md` row (≤ 20,480 B): *"Absence of traffic in a large pool is evidence of impassability, not of shallow water; a warning system must publish a level without inventing its number."*
- `README.md`: feature line for cm depth and hazard levels; no port/command change, so `README.vi.md` stays untouched (`tools/docs/check_readme_vi_parity.py` passes).

## 10. Risks

| Risk | Mitigation |
|:---|:---|
| Waterline noise inflates depth | Two-observation latch, area floor, confidence shown to the rider |
| Calibration goes stale after re-aiming | `measured_at` + `frame_ref` mandatory; 180-day warning; re-run M1–M5 |
| Avoidance over-warns on a reflective puddle | `through_traffic` suppression, area floor, wording claims blockage never depth |
