# Multi-Lane Directional Traffic & Physical Inundation Labeling Taxonomy

**Document Version:** 1.0.0  
**Effective Date:** 2026-10-15  
**Governing Issue:** Issue #253 (Sprint 7 Task S7-T8)  
**Status:** Approved Standard Specification  

---

## 1. Executive Summary & Purpose

This specification establishes an unambiguous, first-principles Computer Vision and AI Engineering labeling standard for ProjectKV (Warded). 

Prior ground-truth datasets exhibited three critical flaws:
1. **Directional Flow Asymmetry**: Dual-carriageway corridors (e.g., Võ Văn Kiệt, Điện Biên Phủ, Mai Chí Thọ) frequently experienced heavy inbound congestion alongside completely clear outbound lanes. Collapsing both directions into a single scalar label produced contradictory ground truth, confusing downstream models and deceiving commuters.
2. **Red-Light Queuing vs. True Congestion**: Motorbikes and passenger vehicles queued during normal red-light phases were falsely classified as congestion because uncalibrated capacity floors could not distinguish transient signal queues from roadway breakdown.
3. **Synthetic Flood Heuristics**: Historical scripts assigned `SHALLOW` flood levels based on geographic corridor lists rather than physical image evidence, poisoning the evaluation benchmark with false-positive flood labels on dry or merely wet asphalt.

This document defines the physical visual anchors, directional partitioning rules, and annotation protocols required to build a 100% verified Gold Standard evaluation dataset.

---

## 2. Directional Traffic Flow Taxonomy

Traffic cameras in Ho Chi Minh City capture diverse vantage points: straight divided arterials, two-way undivided roads, and complex multi-leg intersections.

### 2.1. Directional Partitioning Principles

Every camera frame is evaluated with directional awareness:
- **Direction A (`traffic_dir_a`)**: Primary / Approaching / Near-side traffic flow (relative to camera lens).
- **Direction B (`traffic_dir_b`)**: Secondary / Receding / Far-side traffic flow.
  - If the roadway is a one-way street or the opposite direction is physically obscured/out of frame, `traffic_dir_b` MUST be recorded as `NOT_APPLICABLE`.
- **Global Rollup Rule**: For consumer-facing map rendering, the camera marker severity is computed as:
  $$\text{Global Severity} = \max(\text{Severity}(\text{Dir A}), \text{Severity}(\text{Dir B}))$$
  However, the camera inspection modal and corridor dock MUST present the directional breakdown (e.g., `Direction A: CONGESTED | Direction B: CLEAR`).

### 2.2. Traffic Severity Levels & Physical Indicators

| Severity Level | Flow Characteristic | Vehicle Headway | Signal Queue Behavior | Physical Visual Indicators |
|---|---|---|---|---|
| **`CLEAR`** | Free-flow (Level of Service A/B) | $>3$ vehicle lengths ($>15\text{ m}$) | Clears immediately on green; queue $<5$ vehicles | High travel speeds, wide spacing between motorbikes, empty lane patches visible. |
| **`MODERATE`** | Stable dense flow (Level of Service C/D) | $1 - 2$ vehicle lengths ($5 - 10\text{ m}$) | Clears within 1 green cycle | Constant movement, vehicles traveling close together, normal rush-hour density without stoppage. |
| **`HEAVY`** | Unstable flow / Stop-and-go (Level of Service E) | $<1$ vehicle length ($<3\text{ m}$) | Spills over $1 - 2$ green cycles | Vehicles crawling, motorbikes filtering closely between cars, frequent brake lights, shockwaves. |
| **`CONGESTED`** | Breakdown / Gridlock (Level of Service F) | Zero headway (bumper-to-bumper) | Multi-cycle deadlock ($>2$ cycles) | Vehicles stationary for $>30$ seconds, intersection gridlock, blocked cross-traffic. |

### 2.3. Resolving the Red-Light Queuing Dilemma

A cluster of 15 motorbikes and 3 cars stopped at an intersection is **NOT** necessarily a traffic jam. Annotators must apply the following heuristic:
1. **Queue Length vs. Green Clearance**: If the queue extends less than 30 meters from the stop line and road behind the queue is clear, label as `CLEAR` or `MODERATE`, setting `queue_reason = "traffic_light"`.
2. **Multi-Cycle Spillback**: If the queue extends beyond the camera field of view or vehicles cannot advance during a green signal due to downstream blockage, label as `HEAVY` or `CONGESTED`, setting `queue_reason = "bottleneck"`.

---

## 3. Physical Roadway Inundation Taxonomy

Flood severity is strictly determined by **physical immersion anchors** observed on vehicles, curbs, and road markings. Street names alone MUST NEVER dictate flood labels.

```
+-----------------------------------------------------------------------------------------+
|                                 PHYSICAL FLOOD ANCHORS                                  |
|                                                                                         |
|  [NONE]           Dry asphalt OR wet asphalt with sheen/glare. Zero tire immersion.    |
|                   Road surface fully functional.                                        |
|                                                                                         |
|  [SHALLOW]        Continuous water sheet. Depth < 10 cm.                                |
|                   Bottom 1/4 of motorcycle tire submerged (sole of shoe on road).       |
|                   Visible splashing / bow spray. Vehicles pass cautiously.             |
|                                                                                         |
|  [DEEP]           Severe inundation. Depth > 15-20 cm.                                  |
|                   Motorcycle exhaust pipe / wheel hub submerged; car door sills.        |
|                   Engine stalls, vehicles pushing on sidewalks, traffic diverting.      |
+-----------------------------------------------------------------------------------------+
```

### 3.1. Detailed Flood Levels

#### Level 0: `NONE` (Khô ráo / Ướt mặt đường)
- **Visual Definition**:
  - Roadway is bone dry, OR
  - Roadway is wet due to recent rain, showing specular reflections of headlights/streetlights, but **no standing water sheet exists**.
  - Tire treads make direct mechanical contact with the asphalt aggregate without wake or water displacement.
- **Common False Positive Trap**: Rain reflections at night create high-contrast shiny surfaces that naive CNNs classify as water. If tires are not partially submerged, the label is strictly `NONE`.

#### Level 1: `SHALLOW` (Ngập nhẹ / Đọng nước bề mặt)
- **Visual Definition**:
  - Continuous sheet of standing water covers part or all of the travel lane.
  - Water depth is estimated at $3 - 10\text{ cm}$.
  - **Motorcycle Anchor**: Water covers the tire contact patch up to the bottom $1/4$ of the tire rim. The rider's shoe soles are wet if resting on the pavement.
  - **Dynamic Anchor**: Moving vehicles produce distinct water spray and ripples.
  - Vehicles navigate the section with minor speed reduction.

#### Level 2: `DEEP` (Ngập sâu / Nguy hiểm)
- **Visual Definition**:
  - Extensive water accumulation with depth exceeding $15 - 20\text{ cm}$.
  - **Motorcycle Anchor**: Water reaches or submerges the motorcycle wheel hubs, crankcase, or exhaust pipe.
  - **Car Anchor**: Water reaches the wheel center caps or laps against car lower rocker panels/bumpers.
  - **Behavioral Anchor**: Motorcyclists turn around, ride onto elevated sidewalks, or push stalled motorbikes through the water. Vehicles produce large bow waves.

---

## 4. Image Quality & Environmental Artifact Flags

Traffic camera snapshots suffer from adverse weather and lens contamination. Annotators MUST flag the following quality tags when present:

1. **`rain_on_lens`**: Heavy raindrops adhere directly to the camera housing glass, distorting or blurring parts of the field of view.
2. **`night_glare`**: Headlights or neon signage reflect off wet pavement, causing blooming or blinding flare in the camera sensor.
3. **`occluded`**: Dense tree canopies, billboard structures, utility poles, or passing high-profile container trucks obscure $>30\%$ of the roadway.
4. **`signal_noise`**: Upstream video transmission artifacts, interlacing distortion, severe compression blockiness, or partial frame tearing.

---

## 5. Machine-Readable Schema Specification

Ground-truth label files are stored in JSON format matching the schema in `tools/eval/label_schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "WardedGroundTruthLabel",
  "type": "object",
  "required": [
    "camera_id",
    "snapshot_key",
    "traffic_dir_a",
    "flood_level",
    "weather",
    "time_of_day"
  ],
  "properties": {
    "camera_id": { "type": "string" },
    "snapshot_key": { "type": "string" },
    "annotator": { "type": "string" },
    "traffic_dir_a": { 
      "type": "string", 
      "enum": ["CLEAR", "MODERATE", "HEAVY", "CONGESTED"] 
    },
    "traffic_dir_b": { 
      "type": "string", 
      "enum": ["CLEAR", "MODERATE", "HEAVY", "CONGESTED", "NOT_APPLICABLE"] 
    },
    "queue_reason": { 
      "type": "string", 
      "enum": ["free_flow", "traffic_light", "bottleneck", "flood_barrier", "accident"] 
    },
    "flood_level": { 
      "type": "string", 
      "enum": ["NONE", "SHALLOW", "DEEP"] 
    },
    "flood_submersion_anchor": { 
      "type": "string", 
      "enum": ["dry", "wet_asphalt_no_standing", "tire_base_under_10cm", "exhaust_hub_over_15cm"] 
    },
    "weather": { 
      "type": "string", 
      "enum": ["dry", "rain", "heavy_rain"] 
    },
    "time_of_day": { 
      "type": "string", 
      "enum": ["day", "dusk", "night"] 
    },
    "quality_tags": {
      "type": "array",
      "items": { 
        "type": "string", 
        "enum": ["rain_on_lens", "night_glare", "occluded", "signal_noise"] 
      }
    },
    "notes": { "type": "string" },
    "timestamp": { "type": "integer" }
  }
}
```

---

## 6. Verification & Quality Assurance Protocol

1. **Dual-Review on Flood Positives**: Any frame labeled `SHALLOW` or `DEEP` must be independently verified by a second reviewer before inclusion in the Gold Standard evaluation benchmark.
2. **Automated Schema Validation**: All label JSON files must validate against `tools/eval/label_schema.json` via CI test `tools/eval/benchmark_test.py`.
3. **No Synthetic Extrapolation**: In-situ camera observations represent the sole source of ground truth. Under no circumstances may an annotator assign a flood level to a camera based on neighbor cameras or historical flood databases.
