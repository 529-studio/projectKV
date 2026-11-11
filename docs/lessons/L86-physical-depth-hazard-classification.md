# Lesson 86: Physical Depth Hazard Classification — Evidence of Impasse, Not of Safety

## Navigation Aid & Quick Reference

- **Domain**: CV inference contract, hazard modelling, API persistence, resilient bilingual UI.
- **Relevant Issues**: Issue #401 (S12-T3), ADR-009 §5.2, ADR-013 D6, Lesson L19 (the unit-test verification trap), L60 (context budget), L66 (one row per lesson).
- **Key Files**:
  - `services/cv-engine/hazard_classifier.py` (reference ladder, calibration loader, avoidance rule, de-escalation latch)
  - `services/cv-engine/server.py` (`triangulate_flood_verdict`, `build_flood_assessment`, `/predict` wiring)
  - `services/go-server/internal/cvclient/cvengine.go` (verbatim decode, re-keyed calibration ref, rolling-deploy fallback)
  - `services/go-server/internal/store/verdict_repo.go` + `migrations/0011_add_flood_hazard.sql`, `0012_add_flood_calibration_ref.sql`
  - `apps/web/src/utils/hazardUtils.ts`, `apps/web/src/components/CameraModal.tsx`
- **Core Invariants**:
  - **Absence of traffic in a large pool is evidence of impassability, not of shallow water.** A lane-blocking pool with no through-traffic is a Level 3 hazard whether the lane is empty or riders stopped upstream of it.
  - **A warning system must publish a level without inventing the number behind it.** Unmeasured depth is `null`, never `0.0`; missing data never becomes Level 1.
  - **One mapping from persistence to the wire.** Every path that rebuilds a verdict from SQLite must carry the new fields, or the data reaches the database and never reaches the API.

---

## 1. Context & Problem Statement

The API exposed an abstract area ratio (`water_ratio: 0.25`) and a severity word. A
rider on a Honda Vision could act on neither: 0.25 across a 12 m arterial and 0.25 in
a 3 m alley are different dangers, and neither says whether the water reaches the
exhaust outlet. The only physical number — `depth_estimated_cm = delta_sub × 110` —
never reached the API, the database or the UI.

Worse, `server.py` inferred clearance from emptiness:

```python
if motorcycles_observed == 0 and water_ratio >= 0.35:
    return "FLOODED_SHALLOW", 0.82
```

A rider who stops *upstream* of a large pool behaves exactly as a rational agent does
when the pool is impassable. The branch therefore converted the strongest evidence of
hazard into a safety claim, with 0.82 confidence. Detection counts are a proxy for
traffic, and traffic absence is ambiguous; treating an ambiguous signal as good news is
the root cause, not a threshold to retune.

Three secondary traps appeared while fixing it:

1. **A depth of zero is not always a measurement.** With no vehicle in the water, every
   bike in the frame reports `delta_sub = 0`. Copying the legacy depth through would
   publish "0 cm, measured" next to a flooded lane — a fabricated measurement.
2. **The database is not the wire.** New columns can round-trip through SQLite and still
   never appear on `GET /api/traffic-status` if the boot-time hydration rebuilds the
   struct by hand instead of using the shared mapping.
3. **A single passing truck must not erase a pool for a frame.** Escalation can be
   immediate because over-warning is visible; de-escalation needs corroboration.

---

## 2. Core Architectural Decisions

### A. Put the ruler in the frame (spec §2.1)

The camera is not a ruler, so the ruler must be in the frame. Priority, first available
wins, exactly one source per observation:

1. `MOTORCYCLE_110CM` — a motorcycle box inside the ROI: `px_per_cm = box_h_px / 110`.
2. `CURB_15CM` — a curb segment crossing the pool's waterline: `px_per_cm = curb_px / 15`.
3. `CAMERA_OVERRIDE` — an operator multiplier applied **on top of** a base source.
4. `NONE` — no reference: depth is not estimated.

Only sources 1, 3 and 4 are reachable at runtime in this sprint: no curb segmenter
exists yet, so `/predict` passes `has_curb_reference=False` and `CURB_15CM` is deferred
to the sprint that lands the curb segmentation model. A motorcycle box is also a
reference only when the same frame produced a water mask to measure against; a
detection-only model reports `NONE` and a null depth rather than a measured `0.0`.

`CAMERA_OVERRIDE` is a modifier, not a third base source; an override with no base
reference yields `NONE`. The loader rejects any multiplier outside `1.0 ± 0.25`
(exclusive, so 1.25 and 0.75 fail), requires `measured_by` and `frame_ref`, warns when
`measured_at` is older than 180 days, and never fails to boot.

### B. Classify unrounded, inclusive at the physical threshold (spec §2.3)

`0 ≤ d < 10` Level 1; `10 ≤ d < 20` Level 2; `d ≥ 20` Level 3. 20.0 is inclusive
because 20 cm is the exhaust outlet: at exactly 20 cm the pipe sits at the waterline and
any ripple reaches it. Rounding to one decimal *before* classifying would turn 19.96 cm
into a stall warning; display rounds, classification does not. Depths above 150 cm keep
Level 3 but drop the figure and log a calibration warning — the level is the warning,
the number is a claim.

### C. Replace emptiness with an explicit avoidance inference (spec §2.4)

```
through_traffic  = any motorcycle with delta_sub >= 0.09
upstream_stop    = any motorcycle with box_bottom_y < pool_top_y and delta_sub <= 0.05
pool_blocks_lane = water_ratio >= 0.30 and pool_area_px >= 400
escalate         = pool_blocks_lane and not through_traffic
                   and (motorcycles_observed == 0 or upstream_stop)
```

On escalation: `FLOODED_DEEP`, level 3, basis `AVOIDANCE_INFERRED`, confidence capped
at 0.78, **depth stays `null`**. The 400 px area floor rejects wet-asphalt reflections
the segmentation model reads as water. Through-traffic at rim depth suppresses
escalation: over-warning costs the same trust as under-warning.

A zero submersion measured inside a flooded ROI is not a measurement of the pool — it
is a reference vehicle standing outside the water. The classifier therefore reports the
depth as unmeasured and lets avoidance carry the level, while a genuinely dry frame
(`water_ratio < 0.05`) keeps its measured `0.0`.

Composition is `max(level_from_depth, level_from_avoidance)`; both absent yields
`level: null`, `basis: "NONE"`. No default to Level 1, because absence of measurement
is not safety.

### D. Latch only the drop (spec §2.5)

`level_from_depth` and `level_from_avoidance` stay pure. The single stateful component
is a per-camera `(assessment, observed_at, pending)` map: escalation is immediate, a
drop requires two consecutive agreeing observations, entries older than the 180 s
verdict TTL are ignored, and an unmeasured frame neither resets nor releases a live
escalation.

The map holds the whole `HazardAssessment`, not just the level. A held Level 3 must
republish *its own* `basis`, `depth_cm`, `depth_confidence` and `calibration_ref`;
grafting the held level onto the drop frame's fields publishes a self-contradictory
block ("Level 3, `DEPTH_MEASURED`, 5.0 cm, `MEASURED`") and turns a synthetic frame
into a fabricated measurement, which is the defect this classifier exists to remove.

### E. Pointers end to end, one mapping to the wire (spec §5)

`flood_depth_cm` is `*float64` in Go, not `float64` with `omitempty`: the latter erases
a measured `0.0` and makes "dry, verified" indistinguishable from "never measured". The
same values are scanned with `sql.NullFloat64` / `sql.NullInt64` and projected to
pointers, so a pre-migration row reads `NULL` with no backfill.

The Go client is a carrier: it decodes the five fields verbatim, re-keys cv-engine's
`calibration_ref` to `flood_calibration_ref`, and applies a single rolling-deploy
fallback — level absent, the whole hazard block absent, and `depth_estimated_cm` present
→ derive with the same bands and label the result `INFERRED`. An engine that publishes
the block and explicitly declines to measure keeps its nil level and nil depth. The
client never derives a level from `water_ratio`.

One helper (`hotsetVerdictFromStore`) converts persisted rows into `hotset.Verdict` for
both the GET handlers and the boot-time hydration, and all five hazard fields (the
calibration reference included) are written by `Save` and read by `GetLatest`. A boot
path that rebuilt the struct by hand would return the new columns from SQLite and omit
them from the API; a repository round-trip test cannot observe that divergence, an
end-to-end endpoint assertion can.

### F. Honest UI (spec §6)

`resolveHazard` prefers the published level, derives from a measured depth during
rollout, and returns `null` for a pre-contract verdict so the legacy badge keeps
rendering. The badge replaces the legacy flood chip when it resolves — two chips, one
saying `FLOODED_DEEP` in colour and one saying Level 3 in monochrome, would contradict
each other. Level is encoded as text, an inline SVG glyph and a 3-segment meter; the
depth meter renders only when a number exists; avoidance wording contains no digits.

---

## 3. Verification That Would Have Caught Each Trap

| Trap | Assertion that pins it |
|---|---|
| Emptiness read as clearance | Sweep `water_ratio` 0.30–0.90 × area × empty/upstream: never `FLOODED_SHALLOW`, confidence ≤ 0.78 |
| Unmeasured serialised as zero | `/predict` with a model that finds nothing: `flood_depth_cm is None`, level `None` |
| Zero submersion claimed as dry | Upstream bike in a flooded ROI: depth `None`, basis `AVOIDANCE_INFERRED` |
| Calibration silently bad | 1.25 and 1.4 rejected with a logged error; 1.08 hand-computed to 59.4 cm |
| Column persisted but not served | Seed SQLite, boot the server, assert all four endpoint envelopes |
| Legacy verdict upgraded | Migration on a 0001 database: old row reads `nil` level, legacy badge |
| Over-warning | Through-traffic at `delta_sub 0.09` and a 399 px patch never escalate |
| One-frame de-escalation | Latch 3 → 1 holds; 3 → 1 → 1 releases; TTL expiry ignores history |

Unit tests were necessary and insufficient: the segment→verdict→HTTP run and the
SQLite→server→JSON run each surfaced a defect the unit suites could not see.

---

## 4. Mandatory Guardrails

1. **Never derive a hazard level from an area ratio.** Without a calibrated scale it is
   not a depth. If nothing can be measured, publish a level from explicit avoidance
   evidence or publish `null`.
2. **Never publish a number you did not measure.** `null` is honest; `0.0` means "dry,
   verified". A level may stand without a figure; a figure may not stand without a
   measurement.
3. **Treat traffic absence as ambiguous evidence.** Encode *why* the lane is empty
   (through-traffic, upstream stop, empty road, area floor) rather than inferring
   safety from a count.
4. **Map persisted rows to the wire in exactly one place.** When adding a column, find
   every path that rebuilds the struct — adapters, boot hydration, pre-seed loops — and
   assert the field at the endpoint, not just in the repository.
5. **Escalate immediately, de-escalate on corroboration.** Asymmetric hysteresis
   matches asymmetric cost: a missed hazard is worse than a cautious frame.
