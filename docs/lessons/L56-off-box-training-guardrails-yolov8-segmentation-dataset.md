## Lesson 56: Off-Box Training Guardrails, YOLOv8 Segmentation Dataset Manifests, and Dual-Layer Physical Flood Triangulation

- **Incident & Tracking:** Sprint 8 Wave 2 / Issue #290 / PR targeting `dev` (`feat/s8-t8-road-water-segmentation-pipeline`, [KV-20], ADR-009).
- **Symptom:**
  1. The deployed computer vision engine (`services/cv-engine/server.py`) historically ran detection-only models (COCO 80-class or legacy 5-class) and defaulted flood assessment to `UNKNOWN`.
  2. Cloud vision language model (VLM) APIs suffered severe quota exhaustion (10 RPM / 300 RPD daily limit) during monsoon downpours, making cloud inference unviable.
  3. Attempting to train semantic segmentation models directly on production host `inference worker` risked crashing the node due to strict cgroup allocations (2 GB RAM, 20 GB disk ceiling with auto-disk-guard at 16 GB).
- **Root Cause:**
  1. **Multitask CNN vs. Pure Segmentation Tradeoff**:
     - Standalone segmentation networks (BiSeNet v2, SegFormer) require executing a separate vehicle detection network, doubling latency to 68–87 ms and violating the $\le 45\text{ ms}$ CPU budget.
     - Multitask architectures (YOLOv8n-seg) resolve both bounding boxes (vehicles) and prototype masks (roadway and puddles) in a single unified forward pass.
  2. **Specular Wet Asphalt False Alarms**:
     - Nighttime streetlamp reflections and rain moisture on dark asphalt exhibit specular glare that mimics standing water. Relying exclusively on 2D pixel segmentation creates high false alarm rates.
     - Physical verification via vehicle tire contact is required to distinguish superficial sheen from hazardous water depth ($\ge 20\text{ cm}$).
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Strict Off-Box Training Guardrails (`tools/train/train_yolov8_seg.py`)**:
     - Enforced host compute inspection (`check_host_compute_headroom()`); if disk $< 4\text{ GB}$ or RAM is constrained, training automatically transitions to dry-run verification mode, redirecting heavy fine-tuning to Google Colab or external GPU workstations.
     - Automated export of best checkpoints to OpenVINO FP16 IR (`yolov8n_seg.xml` and `yolov8n_seg.bin`), maintaining model footprint $< 10\text{ MB}$ (target $\sim 6.5\text{ MB}$).
  2. **YOLO Segmentation Dataset Toolchain (`tools/eval/export_yolo_seg.py`)**:
     - Standardized conversion of authentic CCTV frames and Draft-07 ground-truth metadata into Ultralytics YOLO segmentation manifests (`data/yolo_seg/`).
     - Supports 6 target classes: `0: roadway`, `1: water_puddle`, `2: motorcycle`, `3: car`, `4: bus`, `5: truck`.
     - Synthesizes perspective road and water polygons from depth metadata while normalizing coordinates strictly into $[0.0, 1.0]$.
  3. **Dual-Layer Physical Triangulation Engine in `server.py`**:
     - **Layer 1 (Surface Water Ratio $R_{\text{water}}$)**: Evaluates $R_{\text{water}} = \sum (\mathcal{M}_{\text{water}} \land \mathcal{M}_{\text{road}}) / \sum \mathcal{M}_{\text{road}}$. If $R_{\text{water}} < 0.05 \to \text{CLEAR/WET}$; $0.05 \le R_{\text{water}} < 0.15 \to \text{WET/PUDDLE}$; $R_{\text{water}} \ge 0.15 \to$ activates Layer 2.
     - **Layer 2 (Motorcycle Wheel Submersion $\delta_{\text{sub}}$)**: Samples waterline intersection $y_{\text{waterline}}$ coincident with detected motorcycle bounding boxes. Submersion $\delta_{\text{sub}} = (y_{\text{bottom}} - y_{\text{waterline}}) / h_{\text{box}}$ maps to $\text{Depth}_{\text{est}} \approx \delta_{\text{sub}} \times 110\text{ cm}$.
     - When $R_{\text{water}} \ge 0.15$ and $\delta_{\text{sub}} \ge 0.18$ ($\text{Depth}_{\text{est}} \ge 20\text{ cm}$), system issues definitive `FLOODED_DEEP` verdict.
  4. **Sub-40ms Inference Latency with NumPy BLAS**:
     - Mask prototypes are decoded via matrix dot-product and vectorized sigmoid directly on the $104 \times 104$ proto grid, completing mask decoding and triangulation within $< 3\text{ ms}$ overhead and $< 120\text{ MB}$ RSS.


- **Incident & Tracking:** Issue #288 / PR targeting `dev` (`fix/s8-t6-config-sanitization-observability`, [KV-18]).
- **Symptom:**
  1. Setting environment variables with surrounding quotes or whitespace (e.g. `HOTSET_INTERVAL_SECONDS="30"`, `PCU_CAPACITY_FLOOR=" 15.0 "`, or `HARVESTER_ENABLED='true'`) in deployment scripts, systemd service files, or Docker Compose silently caused configuration parsing to fail.
  2. The server silently fell back to compiled defaults without emitting any warnings or errors in the startup logs, leaving DevOps engineers under the false impression that custom performance parameters were active.
  3. When invalid non-numeric inputs were passed (e.g. `HOTSET_INTERVAL_SECONDS="abc"`), errors were discarded silently via bare `if v, err := strconv.Atoi(val); err == nil { ... }`.
- **Root Cause:**
  1. **Strict Formatting of `strconv`**:
     - Standard Go functions `strconv.Atoi`, `strconv.ParseFloat`, and `strconv.ParseBool` do not tolerate surrounding quotes (`"`, `'`) or whitespace/tab padding. A single space or quote triggers `strconv.ErrSyntax`.
  2. **The NaN Float Trap**:
     - `strconv.ParseFloat("nan", 64)` and `strconv.ParseFloat("inf", 64)` return `math.NaN(), nil` and `+Inf, nil` without producing an error.
     - In Go IEEE 754 arithmetic, all comparisons against `NaN` (such as `NaN < min`) evaluate to `false`. Without explicit `math.IsNaN(v)` and `math.IsInf(v, 0)` checks, invalid floats bypass minimum threshold validation and poison downstream arithmetic (such as PCU saturation formulas).
  3. **Silent Error Swallowing**:
     - Across 25+ configuration parameters in `services/go-server/internal/config/config.go`, failure to parse an environment variable silently did nothing, masking typos and operational misconfigurations.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Holistic Quote & Whitespace Stripping**:
     - Implemented `CleanEnv(key)` and unified helper parsers (`parseEnvInt`, `parseEnvInt64`, `parseEnvFloat`, `parseEnvBool`, `parseEnvDuration`, `parseEnvString`) that systematically strip surrounding quotes (`"`, `'`) and whitespace before parsing.
  2. **Explicit NaN/Inf Defensive Guardrails**:
     - Added `math.IsNaN(v) || math.IsInf(v, 0)` guards to `parseEnvFloat` to ensure corrupted strings trigger structured fallback warnings.
  3. **Structured Observability via `log/slog`**:
     - Replaced silently discarded errors with structured warning logs:
       ```go
       slog.Warn("config: invalid numeric format, using fallback default",
           "key", key, "raw_value", val, "default", fallback, "error", err)
       ```
  4. **Comprehensive Configuration Matrix Test Suite**:
     - Implemented `TestLoadConfig_SanitizationAndQuotedVariables` and `TestLoadConfig_InvalidInputsTriggerFallbackDefaults` in `internal/config/config_test.go` verifying quotes, spaces, tabs, out-of-bound values, and non-numeric inputs across both `LoadConfig()` and `LoadFromEnv()`.
