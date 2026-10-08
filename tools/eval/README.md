# Evaluation Harness & Precision/Recall Benchmark (ADR-004 §D6)

This directory provides the evaluation dataset infrastructure, human labeling schema, and precision/recall benchmarking harness for AI camera analysis in **ProjectKV**.

---

## 1. Context & Safety Gate (ADR-004 §D6)

> **ADR-004 §D6 Accuracy Gate**:
> *"Build an evaluation set: ≥300 labeled frames, including ≥60 rain/flood-positive and night frames. Report precision/recall per class. Flood false negatives are the metric that matters. Until the gate passes, any route/flood verdict UI carries a 'beta' label and cannot show a green state."*

### Why Flood False Negatives Matter
When a street is flooded (ankle-deep or wheel-height water):
- **False Positive**: The model says there is water when there isn't. The user takes an alternate route (mild inconvenience).
- **False Negative**: The model says the road is clear/dry when it is actually flooded. A motorcyclist or driver enters a flooded street, stalls their vehicle, or risks serious danger.

Therefore, **Flood False-Negative Rate (FNR = Missed Floods / Actual Floods)** is the safety-critical gate metric.

---

## 2. Label Schema (`label_schema.json`)

The label categories and valid values are defined in [`label_schema.json`](./label_schema.json):

```json
{
  "traffic": ["CLEAR", "MODERATE", "HEAVY", "CONGESTED"],
  "flood": ["NONE", "SHALLOW", "DEEP"],
  "time_of_day": ["day", "night", "dawn_dusk"],
  "weather": ["dry", "rain", "heavy_rain"]
}
```

### Classification Definitions

#### Traffic Severity
- `CLEAR`: Free-flowing traffic, vehicles travelling at standard corridor speeds with no noticeable queues.
- `MODERATE`: Heightened vehicle density, minor slowdowns at intersections, but continuous flow without gridlock.
- `HEAVY`: Significant vehicle queues, stop-and-go movement, substantial delay.
- `CONGESTED`: Standstill traffic, intersection blockage, multi-cycle clearance delays.

#### Flood Severity
- `NONE`: Road surface is dry or has minor tire spray/surface dampness without standing water.
- `SHALLOW`: Visible pooling water on the road surface or curbside (ankle-deep, up to lower wheel rim). Impassable or hazardous for small motorbikes.
- `DEEP`: Severe inundation (wheel-height or higher, above curb height). Dangerous and impassable for standard passenger vehicles.

#### Time of Day
- `day`: Normal daylight conditions.
- `night`: Darkness requiring headlights and streetlights.
- `dawn_dusk`: Twilight, low sun angle, strong shadows or dusk transitions.

#### Weather
- `dry`: No active precipitation.
- `rain`: Light to moderate active rainfall, visible windshield wiper motion or street ripples.
- `heavy_rain`: Downpour, reduced camera visibility, heavy water runoff.

---

## 3. Labeled Frame File Format

Label files are stored as individual JSON files in [`tools/eval/labels/`](./labels/) (one JSON file per frame):

```json
{
  "frame_id": "cam_5a825b7d5058170011f6ead4_1790858415",
  "camera_id": "5a825b7d5058170011f6ead4",
  "image_path": "data/proofs/2026-10-01/5a825b7d5058170011f6ead4/1790858415_sample.jpg",
  "labels": {
    "traffic": "CLEAR",
    "flood": "NONE",
    "time_of_day": "day",
    "weather": "dry"
  },
  "annotator": "human_annotator_name",
  "annotated_at": "2026-10-01T12:00:00Z",
  "notes": "Optional notes on camera glare, occlusion, or street landmarks."
}
```

> Note: Both nested `labels: {...}` and flat top-level `traffic`, `flood`, `weather`, `time_of_day` keys are supported by the parser.

---

## 4. Target Dataset Composition (≥300 Frames)

To ensure statistical significance across rare events and tough lighting conditions, the evaluation dataset targets the following composition:

| Category | Min Frames | Source | Purpose |
|---|---|---|---|
| **Clear / dry / day** | 100 | Sampled archive (`data/proofs/`) | Baseline false-positive control |
| **Moderate traffic** | 50 | State-change archive | Transition boundary precision |
| **Heavy / congested** | 50 | State-change archive | Bottleneck detection recall |
| **Rain (any traffic)** | 40 | Rainy season collection | Lens droplet & reflection resilience |
| **Flood shallow** | 20 | Rainy season + manual collection | **Safety gate: ankle-deep detection** |
| **Flood deep** | 10 | Rare event collection | **Safety gate: impassable road detection** |
| **Night** | 30 | Nighttime sample archive | Low-light headlight/flare handling |
| **Total** | **≥ 300** | Ongoing B1 ingestion | **Accuracy Gate Dataset** |

---

## 5. Benchmark Script Usage (`benchmark.py`)

The evaluation script loads labeled frames, runs them through the specified analyzer, computes multi-class precision/recall and confusion matrices, and checks the flood safety gate.

### 5.1 Quick Start (Mock Analyzer)
Run offline on the sample fixture dataset:
```bash
python3 tools/eval/benchmark.py --analyzer mock
```

Output includes:
- Summary table with accuracy gate verdict
- Precision, recall, and F1 per class (traffic and flood)
- Confusion matrices
- Flood False-Negative diagnostic

### 5.2 Evaluating with Google Gemini Multimodal Vision (VLM)
To benchmark the real VLM analyzer against labeled frames:
```bash
export CLOUD_VISION_API_KEY="your-gemini-api-key"
python3 tools/eval/benchmark.py --analyzer vlm --model gemini-3.5-flash-lite --output-report tools/eval/reports/vlm_eval_report.md
```

### 5.3 Evaluating Precomputed Predictions (CI / Zero-API-Cost)
You can evaluate a saved predictions JSON file without network or API costs:
```bash
python3 tools/eval/benchmark.py --analyzer predictions --predictions-file my_predictions.json
```

### 5.4 Command-Line Options
| Flag | Default | Description |
|---|---|---|
| `--labels-dir` | `tools/eval/labels` | Directory containing frame label JSON files |
| `--fixtures-dir` | `None` | Path to fixture directory with frames and labels |
| `--schema` | `tools/eval/label_schema.json` | Path to category schema file |
| `--analyzer` | `mock` | Analyzer to test (`mock`, `vlm`, `predictions`) |
| `--model` | `gemini-3.5-flash-lite` | Gemini model name when using `--analyzer vlm` |
| `--api-key` | `CLOUD_VISION_API_KEY` | Gemini API key |
| `--output-report` | `None` | File path to write full Markdown evaluation report |
| `--output-json` | `None` | File path to write machine-readable JSON metrics |
| `--gate-max-fnr` | `0.05` (5%) | Maximum allowed flood false-negative rate |
| `--strict` | `False` | Exit with code 1 if accuracy gate fails (for CI) |

---

## 6. Running Tests

Run the complete evaluation harness test suite:
```bash
python3 tools/eval/benchmark_test.py
```
Or via standard unittest discovery:
```bash
python3 -m unittest discover -s tools/eval -p "*_test.py"
```

---

## 7. Hotkey Labeler (`labeler.html`)

A zero-dependency browser-based labeling tool for rapidly annotating JPEG frames pulled from `data/proofs/`.

### 7.1 Quick Start

```bash
# Open directly in any modern browser — no server required
open tools/eval/labeler.html          # macOS
xdg-open tools/eval/labeler.html     # Linux
start tools/eval/labeler.html        # Windows
```

Then click **"Open Folder"** and select a directory containing JPEG frames (e.g. `data/proofs/2026-10-01/` or `tools/eval/fixtures/frames/`).

### 7.2 Hotkey Reference

| Key | Action |
|---|---|
| **1** | Flood → `NONE` (dry / surface spray) |
| **2** | Flood → `SHALLOW` (pooling / ankle-deep) |
| **3** | Flood → `DEEP` (wheel-height or higher) |
| **Q** | Traffic → `CLEAR` |
| **W** | Traffic → `MODERATE` |
| **E** | Traffic → `HEAVY` |
| **R** | Traffic → `CONGESTED` |
| **A** | Weather → `dry` |
| **S** | Weather → `rain` |
| **D** | Weather → `heavy_rain` |
| **Z** | Time of day → `day` |
| **X** | Time of day → `night` |
| **C** | Time of day → `dawn_dusk` |
| **Enter** | **Save label and advance to next frame** |
| **Tab** | Skip frame (no label saved) |
| **← / →** | Navigate without saving |

> **Flood is the only required field before saving** (1/2/3). Traffic, weather, and time-of-day are optional but strongly recommended for the accuracy gate (ADR-004 §D6).

### 7.3 Output

Each saved frame writes a JSON file matching the existing label schema:

```json
{
  "frame_id": "cam_5a825b7d_1790858415",
  "image_path": "cam_5a825b7d_1790858415.jpg",
  "labels": {
    "traffic": "CLEAR",
    "flood": "NONE",
    "time_of_day": "day",
    "weather": "dry"
  },
  "annotator": "your_name",
  "annotated_at": "2026-10-03T10:00:00.000Z"
}
```

The tool writes output in a `labels/` sub-folder inside the opened directory (created automatically). When write permission is denied by the browser, it falls back to downloading individual `.json` files.

Use **"Export All JSON"** to download a single array file (`labels_export.json`) containing all in-session annotations.

### 7.4 Workflow

1. Click **Open Folder** → select `data/proofs/<date>/` or any frame directory.
2. Press **1** / **2** / **3** to classify flood level.
3. Press **Q–R**, **A–D**, **Z–C** to set traffic / weather / time.
4. Press **Enter** to save and advance automatically.
5. At session end, copy the `labels/` output into `tools/eval/labels/`.
6. Run `python3 tools/eval/benchmark.py` to regenerate the benchmark report.

> The annotator name is persisted in `localStorage` so you only type it once per browser profile.

---

## 8. Adding New Labeled Frames

1. Save or copy snapshot JPEG frames from `data/proofs/` into your archive directory.
2. Use the [labeler](./labeler.html) (§7) or create a JSON label file manually in `tools/eval/labels/<frame_id>.json`.
3. Verify labels against the schema using `python3 tools/eval/benchmark_test.py`.
4. Run `python3 tools/eval/benchmark.py` to regenerate the baseline report in `tools/eval/reports/`.

---

## 9. Known-State Smoke-Test Camera Set (Sprint 4 T7 / ADR-005 Finding B)

To avoid regressions and subjective verification on staging UI, a curated set of **10 cameras with deterministic baseline states** is defined in [`fixtures/smoke_test_cameras.json`](./fixtures/smoke_test_cameras.json) and detailed in [`smoke_test_cameras.md`](./smoke_test_cameras.md):

- **5 Free-Flow Baselines (`CLEAR`)**: Wide boulevards and highway interchanges (`mai_chi_tho`, `vo_van_kiet`, `dien_bien_phu`) with consistently low off-peak volume ($SI < 0.35$).
- **5 Chronic Bottlenecks (`CONGESTED`)**: Critical gateway intersections and freight junctions (`cong_hoa`, `dien_bien_phu`, `mai_chi_tho`) with chronic peak rush-hour gridlock ($SI \ge 0.90$).

### 9.1 Running Smoke-Test Verification (`verify_smoke_set.py`)

Run automated baseline verification against mock outputs, recorded verdicts, or live backend APIs:

```bash
# 1. Dry-run mock verification in CI / offline test
python3 tools/eval/verify_smoke_set.py --mock --strict

# 2. Verify precomputed predictions file
python3 tools/eval/verify_smoke_set.py --verdicts my_predictions.json --strict

# 3. Verify against live Go backend on staging/local
python3 tools/eval/verify_smoke_set.py --backend-url http://localhost:3000

# 4. Generate Markdown and JSON summary reports
python3 tools/eval/verify_smoke_set.py --mock --output-markdown reports/smoke_report.md --output-json reports/smoke_report.json
```

### 9.2 Running Smoke-Test Unit Tests

```bash
# Run unit tests directly
python3 -m unittest discover -s tools/eval -p "*_test.py"

# Or via Makefile
make test-eval
```

---

## 10. Real Dataset Collection & Progress Tracking (Sprint 4 T3 / Issue #194)

Per ADR-004 §D6 and Issue #194, all pre-10-06 mock placeholder frames (107 bytes) are strictly discarded. Only real frames captured from 2026-10-08 onwards are ingested into `tools/eval/labels/`.

### 10.1 Collecting Real Frames (`collect_real_frames.py`)

Fetches real camera snapshots directly from DOT upstream or staging volume into `data/proofs/<YYYY-MM-DD>/<camera_id>/<timestamp>_sample.jpg` with valid JFIF magic byte (`ffd8ffe0`) and file size verification (>20KB):

```bash
# Fetch snapshots across all 57 priority corridor hotset cameras
make collect-frames

# Or fetch for a specific corridor
python3 tools/eval/collect_real_frames.py --corridor mai_chi_tho
```

### 10.2 Auditing Dataset Composition (`dataset_progress.py`)

Audits all active JSON label files in `tools/eval/labels/` against ADR-004 §D6 gate targets (≥300 frames, ≥60 flood-positive, ≥30 night):

```bash
# Display terminal progress dashboard
make dataset-progress

# Export Markdown progress report
python3 tools/eval/dataset_progress.py --output-markdown docs/reports/dataset_progress.md
```

