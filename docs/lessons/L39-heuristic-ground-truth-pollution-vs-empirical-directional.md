## Lesson 39: Heuristic Ground-Truth Pollution vs. Empirical Directional Multi-Lane Taxonomy

- **Incident & Tracking:** Issue #253 (S7-T8, PR #258) & Gold-Standard Curation 2026-10-16 (PR #260).
- **Symptom:**
  Despite major perception-layer improvements in Sprint 6 (NMS, letterbox preprocessing, 416px scaling), the real-world 764-frame evaluation benchmark persistently showed low F1-scores (~0.245 Macro F1) and 0% flood recall. Furthermore, on major dual-carriageway boulevards (Võ Văn Kiệt, Mai Chí Thọ), commuters and evaluation agents observed contradictory traffic states: inbound lanes toward the city center were completely gridlocked while outbound lanes were completely clear, yet whole-frame tags collapsed them into a single global score.
- **Root Cause:**
  1. **Answer Key Pollution via Crude String Heuristics**:
     - An audit of `tools/eval/label_real_batch.py` revealed that the 764 "ground-truth" frames were never manually inspected or labeled by humans. Instead, a script injected hardcoded tags based on camera name substring matches: any camera on "Huỳnh Tấn Phát" during evening rain was tagged `traffic: HEAVY, flood: SHALLOW`.
     - When OpenVINO inspected an empty road at 20:47 and correctly reported 0 vehicles, the benchmark marked it as a False Negative because the answer key was wrong.
  2. **Directional Congestion Collapse**:
     - Whole-scene tags (`traffic: CLEAR/CONGESTED`) cannot represent asymmetric flows on divided boulevards, producing misleading alerts and confusing model benchmarks.
  3. **Absence of Physical Ground-Truth Anchors**:
     - Object detectors like YOLOv8n only detect bounding boxes for COCO vehicle classes. They have no concept of water or flood depth without physical depth anchors or segmentation masks.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Strict Prohibition on Heuristic Benchmark Generation**: Never generate ground-truth answer keys using name or timestamp heuristics. Benchmarks must be certified by human inspection or strictly verified multi-attribute schemas.
  2. **Directional Multi-Lane Draft-07 Schema (`label_schema.json`)**:
     - Formalized JSON Schema Draft-07 supporting directional segments (`inbound`, `outbound`), vehicle breakdown counts (`motorcycle`, `car`, `bus`, `truck`), traffic light pause states (`WAITING_RED_LIGHT`), and physical waterline depth anchors (`dry`, `wet_asphalt_no_standing`, `tire_base_under_10cm`, `tire_half_submerged`, `impassable`).
  3. **Pre-Ingestion Schema Validation (`validate_labels.py`)**: All candidate labels must pass programmatic schema validation before entry into `tools/eval/labels_gold_standard/`.
