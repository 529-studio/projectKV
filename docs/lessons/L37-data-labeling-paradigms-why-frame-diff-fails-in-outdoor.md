## Lesson 37: Data Labeling Paradigms, Why Frame-Diff Fails in Outdoor Surveillance, and Zero-VLM Flood Detection

- **Incident & Tracking:** Data Preparation & Perception Roadmap Reality Check (Sprint 6 Strategic Review).
- **Symptom:**
  1. Confusion regarding whether current image-level tags (`labels_d6_real.json`, R2 Web Labeler) constitute "clean training data" for retraining neural networks.
  2. Proposal to replace cloud VLM (Gemini) by comparing current live camera frames against historical tagged reference frames.
  3. Strategic imperative to eliminate cloud VLM dependencies (cost, 10 RPM quota exhaustion, 1.5–4.0s network latency, vendor lock-in) while maintaining high alert reliability across 837 cameras.
- **Root Cause:**
  1. **Conflating Evaluation Benchmarks with Model Training Sets**:
     - Current annotations are whole-image categorical tags (`traffic: CLEAR/MODERATE/HEAVY/CONGESTED`, `flood: NONE/PUDDLE/DEEP`).
     - Object detectors (YOLO) require bounding boxes $[x, y, w, h]$ for each vehicle; segmentation models require pixel masks.
     - The current 965 labeled frames are an **Evaluation Ground-Truth Benchmark** for downstream heuristic verification, NOT a model training set.
  2. **The Outdoor Baseline Comparison Fallacy (DevOps Critique)**:
     - Naive template matching or image subtraction against a "tagged reference sample" fails catastrophically on outdoor municipal CCTV cameras:
       - *Diurnal illumination & shadows*: Sun position shifts continuously, and cloud cover or building shadows alter pixel histograms by $>60\%$.
       - *Camera wind vibration & PTZ repositioning*: 2–10px wind jitter or DOT operator pan/tilt invalidates static pixel alignment immediately.
       - *Specular reflection on wet asphalt*: Rain-slicked pavement mirrors sky and headlights, triggering false flood alarms under direct pixel comparison.
       - *Scale heterogeneity*: 837 cameras across varied heights, focal lengths, and resolutions (512x288 to 1080p) make maintaining static reference baselines unmaintainable.
- **Mandatory Takeaway & Engineering Resolution (Zero-VLM Architecture):**
  1. **Data Taxonomy Clarity**: Formally document existing labeled datasets as benchmark evaluation suites. For future model fine-tuning, adopt bounding box annotation (vehicles) and polygon mask segmentation (standing water).
  2. **Corridor-First Self-Hosted Perception (Roadmap for Zero Gemini Dependency)**:
     - Eliminate VLM calls by transitioning to a 3-tier local pipeline:
       - *Tier 1 (Physical Triangulation)*: Trigger flood inference only when external physical conditions match (`rainfall >= 5mm/h` from Open-Meteo or river tide $>1.50\text{m}$ at Phú An / Nhà Bè stations).
       - *Tier 2 (On-Premise Water Segmentation)*: Deploy a lightweight edge semantic segmentation network (BiSeNet / SegFormer-B0 / YOLOv8-seg via OpenVINO on `inference worker`) trained to segment standing water vs. dry/wet asphalt along major arterial corridors.
       - *Tier 3 (Motorcycle Wheel Waterline Occlusion)*: Exploit Vietnamese traffic realities—measure physical tire submergence and spray patterns on detected motorcycles to verify deep, hazardous flooding ($\ge 20\text{cm}$) with zero cloud dependencies.
