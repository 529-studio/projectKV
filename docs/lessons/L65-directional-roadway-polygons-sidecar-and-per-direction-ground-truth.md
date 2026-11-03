# L65: Directional Roadway Polygons, Sidecar Architecture & Per-Direction Ground Truth

## Context & Problem
In Sprint 7, projectKV introduced a directional traffic taxonomy (`traffic_dir_a` for near/approaching lanes, `traffic_dir_b` for far/receding lanes) to resolve the "Directional Congestion Paradox" on divided carriageways—where one direction is heavily bottlenecked while the opposite carriageway flows freely.

However, that semantics previously existed only at the scene classification level:
1. Object detection and segmentation models counted vehicles across the whole frame without spatial partitioning.
2. The polygon annotation contract (ADR-011 D1 / Issue #297) defined `roadway` masks as a single generic class with no directional attributes.
3. Downstream time-series and congestion forecasting models (S9-T6) suffered signal dilution whenever opposing carriageways exhibited divergent traffic states.

## Key Decisions & Architecture
To bridge the gap between classification semantics and computer vision spatial geometry without breaking existing YOLO segmentation training pipelines:

1. **Class-Conditional Contract & Server Canonicalization**:
   - `PolygonAnnotation` in Go backend (`services/go-server/internal/archiver/handler.go`) and harvester schema (`schema.go`) gained an optional `Direction` field (`a` or `b`).
   - Server strictly enforces that `Direction` is only valid when `Class == "roadway"`; annotations on any other class or unrecognized values fail fast with `400 Bad Request`.
   - Aliases (`dir_a`, `traffic_dir_a`, `near` → `a`; `dir_b`, `traffic_dir_b`, `far` → `b`) are canonicalized server-side before persistence. Absent/empty values pass unchanged for full backward compatibility.

2. **Web Labeler Ergonomics & Bilingual UX**:
   - In `/labeler`, selecting the `roadway` segmentation class dynamically renders a direction selector (`None`, `Direction A (near lane)`, `Direction B (far lane)`) in both Vietnamese and English.
   - Drawn directional polygons render distinct suffix badges (`Chiều A` / `Chiều B` in `vi`, `Dir A` / `Dir B` in `en`) in the polygon chip tray.

3. **Sidecar Export Pattern (`roadway_regions.json`)**:
   - Standard Ultralytics YOLO segmentation text files require uniform class integer IDs (`0: roadway`). Altering YOLO classes would fragment weights and break base segmentation models.
   - Instead, `tools/eval/export_yolo_seg.py` retains standard single-class YOLO `.txt` output while emitting a sidecar `roadway_regions.json` per split containing normalized polygon coordinates, canonical direction, and label IDs. If no directional polygons exist in the split, the sidecar is cleanly omitted (skip-when-absent).

## Verification & Impact
- Full automated test suites pass across Go backend (`make test-go` with `-race`), frontend (`npm test` and Next.js static build), and Python tooling (`python3 -m unittest discover -s tools/eval`).
- End-to-end browser simulation verified drawing two roadway polygons (Direction A and Direction B) on a live camera snapshot, saving to backend API, reloading with persistent direction badges, and exporting to `roadway_regions.json`.
- Downstream feature builders and cv-engine vehicle counting can now map vehicle bounding box centroids into directional roadway polygons, deriving authentic per-direction PCU series and congestion ground truth.
