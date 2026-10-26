# AI Analyzer Evaluation Report (ADR-004 §D6 Accuracy Gate)

- **Generated At**: `2026-10-01T16:22:03.810127+00:00`
- **Analyzer**: `mock` (`mock-dev-engine`)
- **Evaluation Dataset**: `5` labeled frames from `../../../tools/eval/labels`

## 1. Executive Summary & Accuracy Gate

| Gate Metric | Target | Actual Value | Verdict |
|---|---|---|---|
| **Flood False-Negative Rate (FNR)** | ≤ 5.0% | **0.0%** (0/2 missed) | ✓ **PASS** |
| Traffic Macro F1 | ≥ 0.70 | **1.000** | ✓ |
| Flood Macro F1 | ≥ 0.80 | **1.000** | ✓ |

> **Safety Gate Rule (ADR-004 §D6)**: A flood false-negative (failing to detect standing water or deep floods) directs drivers into hazardous corridors. Until the flood FNR is rigorously ≤ 5.0% across ≥60 rain/flood frames, route verdicts cannot show an unrestricted 'clear' state.

## 2. Dataset Composition Progress (Target: ≥300 Frames)

| Category | Target | Current Count | Progress | Status |
|---|---|---|---|---|
| Clear/dry/day | 100 | 1 | `░░░░░░░░░░` 1% | ⏳ 1/100 (1%) |
| Moderate traffic | 50 | 1 | `░░░░░░░░░░` 2% | ⏳ 1/50 (2%) |
| Heavy/congested | 50 | 2 | `░░░░░░░░░░` 4% | ⏳ 2/50 (4%) |
| Rain (any traffic) | 40 | 2 | `░░░░░░░░░░` 5% | ⏳ 2/40 (5%) |
| Flood shallow | 20 | 1 | `░░░░░░░░░░` 5% | ⏳ 1/20 (5%) |
| Flood deep | 10 | 1 | `█░░░░░░░░░` 10% | ⏳ 1/10 (10%) |
| Night | 30 | 1 | `░░░░░░░░░░` 3% | ⏳ 1/30 (3%) |

## 3. Traffic Classification Performance

| Class | Precision | Recall | F1-Score | Support |
|---|---|---|---|---|
| **CLEAR** | 1.000 | 1.000 | 1.000 | 2 |
| **MODERATE** | 1.000 | 1.000 | 1.000 | 1 |
| **HEAVY** | 1.000 | 1.000 | 1.000 | 1 |
| **CONGESTED** | 1.000 | 1.000 | 1.000 | 1 |
| *Macro Avg* | - | - | **1.000** | 5 |
| *Weighted Avg* | - | - | **1.000** | 5 |

### Traffic Confusion Matrix

| Actual \ Predicted | CLEAR | MODERATE | HEAVY | CONGESTED | Total |
|---|---|---|---|---|---|
| **CLEAR** | 2 | 0 | 0 | 0 | **2** |
| **MODERATE** | 0 | 1 | 0 | 0 | **1** |
| **HEAVY** | 0 | 0 | 1 | 0 | **1** |
| **CONGESTED** | 0 | 0 | 0 | 1 | **1** |

## 4. Flood Classification Performance

| Class | Precision | Recall | F1-Score | Support |
|---|---|---|---|---|
| **NONE** | 1.000 | 1.000 | 1.000 | 3 |
| **SHALLOW** | 1.000 | 1.000 | 1.000 | 1 |
| **DEEP** | 1.000 | 1.000 | 1.000 | 1 |
| *Macro Avg* | - | - | **1.000** | 5 |
| *Weighted Avg* | - | - | **1.000** | 5 |

### Flood Confusion Matrix

| Actual \ Predicted | NONE | SHALLOW | DEEP | Total |
|---|---|---|---|---|
| **NONE** | 3 | 0 | 0 | **3** |
| **SHALLOW** | 0 | 1 | 0 | **1** |
| **DEEP** | 0 | 0 | 1 | **1** |

## 5. Flood False-Negative Diagnostic (Missed Floods)

 **Zero false-negative flood events detected!** All flooded conditions were recognized as hazardous.

## 6. Sliced Analysis (Weather & Lighting)

### Weather Slice

| Weather Condition | Frames | Traffic Accuracy | Flood Accuracy |
|---|---|---|---|
| **dry** | 3 | 100.0% | 100.0% |
| **heavy_rain** | 1 | 100.0% | 100.0% |
| **rain** | 1 | 100.0% | 100.0% |

### Time of Day Slice

| Lighting / Time | Frames | Traffic Accuracy | Flood Accuracy |
|---|---|---|---|
| **day** | 4 | 100.0% | 100.0% |
| **night** | 1 | 100.0% | 100.0% |
