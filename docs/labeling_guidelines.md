# ProjectKV Ground Truth Labeling Guidelines

## 1. Overview & Context

This document defines the formal engineering standard for labeling ground-truth traffic and flood camera frames in ProjectKV. It accompanies Sprint 7 Issue #253 (S7-T8) and formalizes the transition from naive whole-frame heuristic classifications to multi-lane dual-carriageway directional evaluation and physical waterline depth anchors.

All ground truth labels emitted by human annotators or automated ingestion tools must adhere strictly to the JSON Schema Draft-07 specification in [`tools/eval/label_schema.json`](file:///Users/thanhdatvu/repos/projectKV/tools/eval/label_schema.json) and be validated using [`tools/eval/validate_labels.py`](file:///Users/thanhdatvu/repos/projectKV/tools/eval/validate_labels.py).

---

## 2. JSON Schema Draft-07 Conformance

All label files must be JSON objects with root structure:
- `frame_id` (string, required): Unique frame identifier (e.g. `cam_56de42f611f398ec0c48127d_1792045044`).
- `camera_id` (string, optional): Hexadecimal camera ID.
- `camera_name` (string, optional): Human-readable location or intersection name.
- `image_path` (string, required): Relative repository path or Cloudflare R2 URL to the frame image.
- `annotator` (string, required): Identifier of annotator or tool.
- `annotated_at` (string, required): ISO-8601 UTC timestamp.
- `notes` (string, optional): Contextual notes (e.g. road capacity baselines, landmark occlusions).
- `labels` (object, required):
  - `traffic` (string, required, enum: `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`): Scene-level maximum traffic density.
  - `carriageway_direction` (string, enum: `inbound`, `outbound`, `bidirectional`): Carriageway flow orientation relative to camera perspective.
  - `traffic_dir_a` (string, enum: `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`): Traffic density in Direction A (near/approaching carriageway).
  - `traffic_dir_b` (string, enum: `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`, `NOT_APPLICABLE`): Traffic density in Direction B (far/receding carriageway).
  - `is_waiting_red_light` (boolean): Flag indicating vehicles are transiently paused due to a traffic signal cycle.
  - `queue_reason` (string, enum: `free_flow`, `traffic_light`, `bottleneck`, `flood_barrier`, `accident`): Primary root cause of observed vehicle queue.
  - `flood` (string, required, enum: `NONE`, `SHALLOW`, `DEEP`): Standard 3-tier severity classification for UI markers.
  - `waterline_depth` (string, enum: `DRY`, `WET_SURFACE`, `TIRE_QUARTER_SUBMERGED`, `TIRE_HALF_SUBMERGED`, `IMPASSABLE`): Physical motorcycle wheel submersion depth anchor.
  - `flood_submersion_anchor` (string, enum: `dry`, `wet_asphalt_no_standing`, `tire_base_under_10cm`, `exhaust_hub_over_15cm`): Granular physical anchor mapping.
  - `weather` (string, enum: `dry`, `rain`, `heavy_rain`): Precipitation level observed on camera.
  - `time_of_day` (string, enum: `day`, `night`, `dawn_dusk`): Lighting condition.
  - `quality_tags` (array of string, enum items: `rain_on_lens`, `night_glare`, `occluded`, `signal_noise`): Optical distortion flags.

---

## 3. Dual-Carriageway Traffic Density Guidelines

Divided boulevards (e.g. Mai Chi Tho, Vo Van Kiet, Dien Bien Phu) frequently exhibit asymmetric traffic flow during morning and evening rush hours: one direction is heavily congested while the opposite carriageway remains free-flowing.

### Carriageway Direction
- `bidirectional`: Dual carriageway with both approaching (Dir A) and receding (Dir B) vehicle streams visible in frame.
- `inbound`: Single carriageway or intersection zoom focusing exclusively on approaching vehicles.
- `outbound`: Single carriageway focusing exclusively on receding vehicles.

### Lane-Level Density Categories
- `CLEAR`: Vehicles travel at free-flow corridor speed; headway between vehicles exceeds 5 vehicle lengths.
- `MODERATE`: Dense moving traffic with vehicle headway between 2 to 4 vehicle lengths.
- `HEAVY`: Vehicles moving slowly (< 15 km/h) bumper-to-bumper with stop-and-go intervals.
- `CONGESTED`: Complete standstill or gridlock lasting beyond ordinary signal cycles.
- `NOT_APPLICABLE`: Assigned to `traffic_dir_b` when the opposite carriageway is occluded or not present.

### Global Rollup Formula
The global scene-level `traffic` attribute reflects the worst-case condition observed on any active carriageway:
$$\text{traffic} = \max(\text{traffic\_dir\_a}, \text{traffic\_dir\_b})$$
where severity order is:
$$\text{CLEAR} < \text{MODERATE} < \text{HEAVY} < \text{CONGESTED}$$

---

## 4. Red-Light Waiting vs True Congestion

A frequent flaw in naive CV counting is classifying vehicles stopped at a 30-second red traffic light as chronic congestion. Annotators must decouple transient signal pauses from real gridlock:

- `is_waiting_red_light = true`:
  - Applied when vehicles are neatly queued behind a visible stop line or pedestrian crossing with no upstream queue spilling into previous junctions.
  - Set `queue_reason = "traffic_light"`.
  - Directional density should reflect transient queue length, but global traffic state must not trigger permanent congestion alerts unless the queue fails to clear across multiple green cycles.
- `is_waiting_red_light = false`:
  - Free-flow traffic (`queue_reason = "free_flow"`).
  - Downstream bottleneck or lane blockage (`queue_reason = "bottleneck"`).
  - Standing water preventing vehicle passage (`queue_reason = "flood_barrier"`).
  - Collision or breakdown obstruction (`queue_reason = "accident"`).

---

## 5. Motorcycle Wheel Waterline Depth Anchors

Because road geometry and camera angles vary widely across the 837 city cameras, water depth cannot be measured in absolute pixels without physical reference anchors. Motorcycle wheels serve as Ho Chi Minh City's primary optical depth gauge:

| Waterline Depth | Physical Landmark Anchor | UI Flood Tier | Practical Vehicle Impact |
|:---|:---|:---|:---|
| `DRY` | Road dry, zero moisture | `NONE` | Normal operation. |
| `WET_SURFACE` | Wet asphalt reflection only, 0cm standing water | `NONE` | Rain wet surface, reduced tire friction, zero pooling. |
| `TIRE_QUARTER_SUBMERGED` | Water reaches bottom 5-10cm of tire (below rim) | `SHALLOW` | Shallow puddle, splash spray, passable by all motorbikes. |
| `TIRE_HALF_SUBMERGED` | Water covers 15-20cm, submerging wheel rim and exhaust pipe | `DEEP` | Hazardous flood, exhaust stall risk, motorbikes forced to detour. |
| `IMPASSABLE` | Water exceeds 25cm, submerging engine crankcase or floorboards | `DEEP` | Impassable, stalled vehicles, emergency avoidance required. |

---

## 6. Quality Distortion Flags

Annotators must record optical degradations in `quality_tags`:
- `rain_on_lens`: Water droplets adhering to camera dome or lens housing, blurring foreground features.
- `night_glare`: Vehicle headlights or streetlamps causing blooming and sensor oversaturation on wet asphalt.
- `occluded`: Camera partially blocked by tree foliage, construction scaffolding, bridge girders, or signage.
- `signal_noise`: Compression artifacts, low-bandwidth color banding, or timestamp overlays obscuring road area.

---

## 7. Labeling Tools & Verification Workflow

### Web Labeler (`apps/web/src/app/labeler/page.tsx`)
1. Access the web interface at `/labeler`.
2. Authenticate using developer token if required by backend gatekeeper.
3. Use hotkeys for rapid annotation:
   - `1` through `5`: Waterline depth (`DRY` to `IMPASSABLE`).
   - `Q` / `W` / `E` / `R`: Direction A density (`CLEAR` to `CONGESTED`).
   - `Shift + Q` / `Shift + W` / `Shift + E` / `Shift + R` / `Shift + T`: Direction B density.
   - `L`: Toggle red-light waiting state.
   - `A` / `S` / `D`: Weather (`dry`, `rain`, `heavy_rain`).
   - `Z` / `X` / `C`: Time of day (`day`, `night`, `dawn_dusk`).
   - `Enter`: Save annotation to backend and advance to next frame.

### Standalone HTML Labeler (`tools/eval/labeler.html`)
For offline or local folder labeling, open `tools/eval/labeler.html` directly in any modern browser. Supports directory handle storage and JSON export.

### Strict Validation Tool (`tools/eval/validate_labels.py`)
Run validation on any target label directory:
```bash
python3 tools/eval/validate_labels.py tools/eval/labels_gold_standard --quiet
```
Validation verifies 100% compliance with Draft-07 schema:
- Root object type and required fields (`frame_id`, `image_path`, `labels`, `annotator`, `annotated_at`).
- Nested object types and enum validity.
- Array constraints and boolean type enforcement.

Unit tests for the validator can be run via:
```bash
python3 -m unittest tools/eval/validate_labels_test.py
```

---

## Segmentation Polygon Annotations (Issue #297 / ADR-011 D1)

The web labeler (`/labeler`, Segmentation mode) now supports polygon ground truth
for the 6-class segmentation taxonomy. The contract is enforced identically on
client and server:

| Rule | Value |
|---|---|
| Classes (canonical) | `roadway`, `water_puddle`, `motorcycle`, `car`, `bus`, `truck` |
| Aliases | accepted and canonicalized server-side (`flood`/`puddle`/`water` → `water_puddle`, `road`/`asphalt` → `roadway`, `motorbike`/`bike`/`mc` → `motorcycle`, `van`/`suv`/`automobile` → `car`, `coach` → `bus`, `container`/`lorry` → `truck`) |
| Coordinates | normalized `[0.0, 1.0]` against image natural size (pixel coordinates are rejected) |
| Vertices per polygon | 3 – 64 |
| Polygons per label | ≤ 32 |
| Provenance `source` | `human` (canvas), `sam_prelabel` (machine proposal, must be human-corrected before promotion) |

Labeling workflow: enable `Chế độ phân vùng / Segmentation mode`, pick a class,
click vertices on the snapshot, close by clicking the first vertex (enlarged) or
pressing Enter (Esc cancels the draft). Existing polygons reload as editable
annotations; drag vertices to correct them. `water_puddle` polygons must follow
ADR-009 §4.3: specular wet asphalt without standing water is `wet_asphalt_no_standing`
and must NOT be masked as water.

Dataset export: `python3 tools/eval/export_yolo_seg.py --labels-dir <dir> --verify`
consumes `annotations` directly. Labels without polygons are skipped by default;
`--allow-synthetic` (fabricated geometry, smoke tests only) is opt-in per Lesson 58.
