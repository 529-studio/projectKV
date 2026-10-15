#!/usr/bin/env python3
"""
benchmark.py — Evaluation harness for ProjectKV traffic camera analysis.

ADR-004 §D6 Accuracy Gate:
"Build an evaluation set: ≥300 labeled frames, including ≥60 rain/flood-positive
and night frames. Report precision/recall per class. Flood false negatives are
the metric that matters."

Usage:
    python3 tools/eval/benchmark.py --analyzer mock
    python3 tools/eval/benchmark.py --analyzer vlm --api-key <KEY>
    python3 tools/eval/benchmark.py --analyzer predictions --predictions-file preds.json
"""

import argparse
import base64
import json
import math
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Set, Tuple, Any


# ---------------------------------------------------------------------------
# Data Models & Schemas
# ---------------------------------------------------------------------------

DEFAULT_SCHEMA = {
    "traffic": ["CLEAR", "MODERATE", "HEAVY", "CONGESTED"],
    "flood": ["NONE", "SHALLOW", "DEEP"],
    "time_of_day": ["day", "night", "dawn_dusk"],
    "weather": ["dry", "rain", "heavy_rain"],
}

# Target frame counts from ADR-004 §D6 / Issue #71
TARGET_COMPOSITION = [
    {"category": "Clear/dry/day", "target": 100, "filter": lambda f: f.traffic == "CLEAR" and f.weather == "dry" and f.time_of_day == "day"},
    {"category": "Moderate traffic", "target": 50, "filter": lambda f: f.traffic == "MODERATE"},
    {"category": "Heavy/congested", "target": 50, "filter": lambda f: f.traffic in ("HEAVY", "CONGESTED")},
    {"category": "Rain (any traffic)", "target": 40, "filter": lambda f: f.weather in ("rain", "heavy_rain")},
    {"category": "Flood shallow", "target": 20, "filter": lambda f: f.flood == "SHALLOW"},
    {"category": "Flood deep", "target": 10, "filter": lambda f: f.flood == "DEEP"},
    {"category": "Night", "target": 30, "filter": lambda f: f.time_of_day == "night"},
]


@dataclass
class LabeledFrame:
    frame_id: str
    camera_id: str
    image_path: Path
    traffic: str
    flood: str
    time_of_day: str
    weather: str
    annotator: Optional[str] = None
    annotated_at: Optional[str] = None
    notes: Optional[str] = None
    raw_data: Dict[str, Any] = field(default_factory=dict)


@dataclass
class Prediction:
    frame_id: str
    traffic: str
    flood: str
    confidence: float
    description: str = ""
    raw_response: Optional[Dict[str, Any]] = None
    latency_ms: float = 0.0


@dataclass
class ClassMetric:
    cls_name: str
    tp: int = 0
    fp: int = 0
    fn: int = 0
    tn: int = 0
    support: int = 0

    @property
    def precision(self) -> float:
        denom = self.tp + self.fp
        return (self.tp / denom) if denom > 0 else 0.0

    @property
    def recall(self) -> float:
        denom = self.tp + self.fn
        return (self.tp / denom) if denom > 0 else 0.0

    @property
    def f1(self) -> float:
        p, r = self.precision, self.recall
        return (2 * p * r / (p + r)) if (p + r) > 0 else 0.0


@dataclass
class ConfusionMatrix:
    classes: List[str]
    matrix: Dict[str, Dict[str, int]]  # actual -> predicted -> count

    def get(self, actual: str, predicted: str) -> int:
        return self.matrix.get(actual, {}).get(predicted, 0)


@dataclass
class FloodGateResult:
    actual_positives: int
    detected_positives: int
    false_negatives: int
    false_positives: int
    true_negatives: int
    false_negative_rate: float
    max_fnr_threshold: float
    passed: bool
    status_label: str
    missed_frames: List[Dict[str, Any]] = field(default_factory=list)


@dataclass
class BenchmarkResult:
    timestamp: str
    analyzer_name: str
    model_name: str
    total_frames: int
    labels_dir: str
    traffic_classes: List[str]
    traffic_metrics: Dict[str, ClassMetric]
    traffic_macro_f1: float
    traffic_weighted_f1: float
    traffic_cm: ConfusionMatrix
    flood_classes: List[str]
    flood_metrics: Dict[str, ClassMetric]
    flood_macro_f1: float
    flood_weighted_f1: float
    flood_cm: ConfusionMatrix
    flood_gate: FloodGateResult
    weather_slices: Dict[str, Dict[str, Any]]
    time_of_day_slices: Dict[str, Dict[str, Any]]
    composition_progress: List[Dict[str, Any]]
    predictions: List[Dict[str, Any]] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Schema Validation & Dataset Loading
# ---------------------------------------------------------------------------

def load_schema(schema_path: Optional[Path]) -> Dict[str, List[str]]:
    """Loads and returns label schema definition."""
    if schema_path and schema_path.is_file():
        try:
            with open(schema_path, "r", encoding="utf-8") as f:
                data = json.load(f)
                if isinstance(data, dict):
                    # Direct category map
                    if "traffic" in data and "flood" in data:
                        return data
                    # Nested in "categories" property
                    if "categories" in data and isinstance(data["categories"], dict):
                        return data["categories"]
                    # Draft-07 JSON Schema with properties.labels.properties
                    if "properties" in data and "labels" in data["properties"]:
                        props = data["properties"]["labels"].get("properties", {})
                        enums: Dict[str, List[str]] = {}
                        for k, v in props.items():
                            if isinstance(v, dict) and "enum" in v:
                                enums[k] = v["enum"]
                        if enums:
                            return enums
        except Exception as e:
            print(f"[WARN] Failed to parse schema file {schema_path}: {e}. Falling back to default schema.", file=sys.stderr)
    return DEFAULT_SCHEMA


def parse_label_dict(data: Dict[str, Any], schema: Dict[str, List[str]], repo_root: Optional[Path] = None, fallback_id: str = "") -> LabeledFrame:
    """Parses a dictionary representing a labeled frame."""
    if not isinstance(data, dict):
        raise ValueError("Label JSON must be an object")

    # Extract labels (check nested labels object or top-level keys)
    labels = data.get("labels", {})
    traffic = (labels.get("traffic") or data.get("traffic") or "").strip().upper()
    flood = (labels.get("flood") or data.get("flood") or "").strip().upper()
    time_of_day = (labels.get("time_of_day") or data.get("time_of_day") or "").strip().lower()
    weather = (labels.get("weather") or data.get("weather") or "").strip().lower()

    # Validate against schema
    if traffic not in schema["traffic"]:
        raise ValueError(f"Invalid traffic value '{traffic}'. Allowed: {schema['traffic']}")
    if flood not in schema["flood"]:
        raise ValueError(f"Invalid flood value '{flood}'. Allowed: {schema['flood']}")
    if time_of_day not in schema["time_of_day"]:
        raise ValueError(f"Invalid time_of_day value '{time_of_day}'. Allowed: {schema['time_of_day']}")
    if weather not in schema["weather"]:
        raise ValueError(f"Invalid weather value '{weather}'. Allowed: {schema['weather']}")

    frame_id = str(data.get("frame_id") or fallback_id)
    camera_id = str(data.get("camera_id") or "")
    image_path_raw = str(data.get("image_path") or "")

    # Resolve image path relative to repo_root
    image_path = Path(image_path_raw)
    if repo_root and not image_path.is_absolute():
        candidate_repo = repo_root / image_path
        if candidate_repo.exists():
            image_path = candidate_repo

    return LabeledFrame(
        frame_id=frame_id,
        camera_id=camera_id,
        image_path=image_path,
        traffic=traffic,
        flood=flood,
        time_of_day=time_of_day,
        weather=weather,
        annotator=data.get("annotator"),
        annotated_at=data.get("annotated_at"),
        notes=data.get("notes"),
        raw_data=data,
    )


def parse_label_file(file_path: Path, schema: Dict[str, List[str]], repo_root: Optional[Path] = None) -> LabeledFrame:
    """Parses a single label JSON file supporting both flat and nested schemas."""
    with open(file_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    frame = parse_label_dict(data, schema, repo_root, fallback_id=file_path.stem)
    if not frame.image_path.is_file() and not frame.image_path.is_absolute():
        candidate_local = file_path.parent / Path(data.get("image_path") or "")
        if candidate_local.exists():
            frame.image_path = candidate_local
    return frame


def load_dataset(target_path: Path, schema: Dict[str, List[str]], repo_root: Optional[Path] = None) -> List[LabeledFrame]:
    """Loads all label JSON files from directory recursively or from a single JSON file."""
    frames: List[LabeledFrame] = []
    errors: List[str] = []

    if target_path.is_file():
        with open(target_path, "r", encoding="utf-8") as f:
            raw = json.load(f)
        items = raw if isinstance(raw, list) else raw.get("frames", [raw])
        for idx, item in enumerate(items):
            try:
                frame = parse_label_dict(item, schema, repo_root, fallback_id=f"frame_{idx}")
                frames.append(frame)
            except Exception as e:
                errors.append(f"item {idx}: {e}")
    elif target_path.is_dir():
        for path in sorted(target_path.glob("**/*.json")):
            # Skip schema or metadata files
            if path.name in ("label_schema.json", "schema.json", "package.json"):
                continue
            try:
                frame = parse_label_file(path, schema, repo_root)
                frames.append(frame)
            except Exception as e:
                errors.append(f"{path.name}: {e}")
    else:
        raise FileNotFoundError(f"Labels path not found: {target_path}")

    if errors:
        print(f"[WARN] Encountered {len(errors)} invalid label files:", file=sys.stderr)
        for err in errors[:5]:
            print(f"  - {err}", file=sys.stderr)
        if len(errors) > 5:
            print(f"  ... and {len(errors) - 5} more", file=sys.stderr)

    return frames


# ---------------------------------------------------------------------------
# Analyzers
# ---------------------------------------------------------------------------

class BaseAnalyzer:
    """Abstract interface for camera frame analyzers."""
    name: str = "base"
    model: str = "none"

    def analyze(self, frame: LabeledFrame) -> Prediction:
        raise NotImplementedError


class MockAnalyzer(BaseAnalyzer):
    """
    Deterministic mock analyzer for local verification and unit testing.
    Can operate in exact ground-truth simulation or controlled accuracy mode.
    """
    name: str = "mock"
    model: str = "mock-dev-engine"

    def __init__(self, accuracy: float = 1.0, simulate_flood_miss: bool = False):
        self.accuracy = accuracy
        self.simulate_flood_miss = simulate_flood_miss

    def analyze(self, frame: LabeledFrame) -> Prediction:
        t0 = time.time()
        # Default: matches ground truth for deterministic tests
        pred_traffic = frame.traffic
        pred_flood = frame.flood

        if self.simulate_flood_miss and frame.flood in ("SHALLOW", "DEEP"):
            pred_flood = "NONE"

        latency = (time.time() - t0) * 1000.0
        return Prediction(
            frame_id=frame.frame_id,
            traffic=pred_traffic,
            flood=pred_flood,
            confidence=0.95,
            description="Mock analyzer deterministic prediction",
            latency_ms=latency,
        )


class PrecomputedPredictionsAnalyzer(BaseAnalyzer):
    """Loads predictions from a pre-recorded JSON file."""
    name: str = "predictions"

    def __init__(self, filepath: Path):
        self.model = filepath.name
        with open(filepath, "r", encoding="utf-8") as f:
            raw = json.load(f)
        self.lookup: Dict[str, Dict[str, Any]] = {}
        if isinstance(raw, list):
            for item in raw:
                fid = str(item.get("frame_id") or "")
                if fid:
                    self.lookup[fid] = item
        elif isinstance(raw, dict):
            # Check if map of frame_id -> dict
            for k, v in raw.items():
                if isinstance(v, dict):
                    self.lookup[k] = v

    def analyze(self, frame: LabeledFrame) -> Prediction:
        item = self.lookup.get(frame.frame_id)
        if not item:
            # Fallback to UNKNOWN
            return Prediction(
                frame_id=frame.frame_id,
                traffic="UNKNOWN",
                flood="UNKNOWN",
                confidence=0.0,
                description="Frame not found in precomputed predictions file",
            )

        traffic = str(item.get("traffic") or item.get("traffic_severity") or "UNKNOWN").upper()
        flood_raw = str(item.get("flood") or item.get("flood_severity") or "UNKNOWN").upper()
        flood = normalize_vlm_flood_severity(flood_raw)
        confidence = float(item.get("confidence", 0.9))

        return Prediction(
            frame_id=frame.frame_id,
            traffic=traffic,
            flood=flood,
            confidence=confidence,
            description=str(item.get("description", "")),
            raw_response=item,
        )


def normalize_vlm_flood_severity(raw: str) -> str:
    """Maps VLM flood_severity (DRY, WET, FLOODED_SHALLOW, FLOODED_DEEP) to evaluation schema (NONE, SHALLOW, DEEP)."""
    s = raw.strip().upper()
    if s in ("DRY", "WET", "NONE"):
        return "NONE"
    if s in ("FLOODED_SHALLOW", "SHALLOW"):
        return "SHALLOW"
    if s in ("FLOODED_DEEP", "DEEP"):
        return "DEEP"
    return "UNKNOWN"


def normalize_vlm_traffic_severity(raw: str) -> str:
    """Normalizes traffic severity from VLM output."""
    s = raw.strip().upper()
    if s in ("CLEAR", "MODERATE", "HEAVY", "CONGESTED"):
        return s
    return "UNKNOWN"


class VLMAnalyzer(BaseAnalyzer):
    """
    Calls Google Gemini Multimodal Vision API (Issue #71 / ADR-004 §D6 & §B3).
    Encodes JPEG image frame as base64 and parses structured JSON output.
    """
    name: str = "vlm"

    TRAFFIC_PROMPT = """You are a traffic analyst AI. Analyze the traffic camera image and return a JSON object ONLY — no markdown, no explanation.

JSON schema (all fields required):
{
  "traffic_severity": "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN",
  "flood_severity": "DRY" | "WET" | "FLOODED_SHALLOW" | "FLOODED_DEEP" | "UNKNOWN",
  "confidence": <float 0.0–1.0>,
  "description": "<one sentence in English>"
}

Rules:
- traffic_severity = CLEAR: free-flowing traffic, no congestion.
- traffic_severity = MODERATE: some slowdowns, vehicles moving.
- traffic_severity = HEAVY: significant slowdown, queues forming.
- traffic_severity = CONGESTED: gridlock or near-standstill.
- flood_severity = DRY: no visible water on road surface.
- flood_severity = WET: wet road, rain, shallow puddles only.
- flood_severity = FLOODED_SHALLOW: ankle-deep to wheel-height water on road.
- flood_severity = FLOODED_DEEP: waist-high or deeper water, impassable.
- If the image is too dark, blurry, obscured, or otherwise unanalyzable, set confidence < 0.5 and use UNKNOWN for both severity fields.
- confidence must reflect how certain you are about BOTH severity fields combined.
- Return ONLY the JSON object. No other text."""

    def __init__(self, api_key: str, model: str = "gemini-3.5-flash-lite", request_delay: float = 0.5):
        self.api_key = api_key
        self.model = model
        self.request_delay = request_delay

    def analyze(self, frame: LabeledFrame) -> Prediction:
        if not frame.image_path.is_file():
            raise FileNotFoundError(f"Image not found for frame {frame.frame_id}: {frame.image_path}")

        image_bytes = frame.image_path.read_bytes()
        b64_image = base64.b64encode(image_bytes).decode("ascii")

        endpoint = f"https://generativelanguage.googleapis.com/v1beta/models/{self.model}:generateContent?key={self.api_key}"
        payload = {
            "contents": [
                {
                    "parts": [
                        {"text": self.TRAFFIC_PROMPT},
                        {
                            "inline_data": {
                                "mime_type": "image/jpeg",
                                "data": b64_image,
                            }
                        },
                    ]
                }
            ],
            "generationConfig": {
                "temperature": 0.1,
                "responseMimeType": "application/json",
            },
        }

        body = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            endpoint,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )

        t0 = time.time()
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                resp_bytes = resp.read()
            latency = (time.time() - t0) * 1000.0
        except urllib.error.HTTPError as e:
            err_msg = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Gemini API HTTP Error {e.code}: {err_msg}") from e
        except Exception as e:
            raise RuntimeError(f"Gemini API request failed: {e}") from e

        if self.request_delay > 0:
            time.sleep(self.request_delay)

        resp_data = json.loads(resp_bytes.decode("utf-8"))
        try:
            text_part = resp_data["candidates"][0]["content"]["parts"][0]["text"]
            clean_text = text_part.strip()
            if clean_text.startswith("```"):
                lines = clean_text.splitlines()
                clean_text = "\n".join(lines[1:-1] if lines[-1].startswith("```") else lines[1:])
            verdict = json.loads(clean_text)
        except Exception as e:
            return Prediction(
                frame_id=frame.frame_id,
                traffic="UNKNOWN",
                flood="UNKNOWN",
                confidence=0.0,
                description=f"Failed to parse Gemini output: {e}",
                raw_response=resp_data,
                latency_ms=latency,
            )

        traffic_raw = str(verdict.get("traffic_severity") or "UNKNOWN")
        flood_raw = str(verdict.get("flood_severity") or "UNKNOWN")
        confidence = float(verdict.get("confidence", 0.0))
        description = str(verdict.get("description", ""))

        return Prediction(
            frame_id=frame.frame_id,
            traffic=normalize_vlm_traffic_severity(traffic_raw),
            flood=normalize_vlm_flood_severity(flood_raw),
            confidence=confidence,
            description=description,
            raw_response=verdict,
            latency_ms=latency,
        )


class CVEngineAnalyzer(BaseAnalyzer):
    """
    Calls self-hosted OpenVINO cv-engine microservice (POST /predict with raw JPEG bytes).
    Computes PCU Saturation Index (SI) and maps to traffic severity conforming to
    ADR-004 §9 D15, ADR-005 T6 (Container folded into Truck = 2.0), and cvclient/pcu.go.
    """
    name: str = "cv-engine"
    model: str = "yolov8n_openvino_320"

    def __init__(
        self,
        base_url: str = "http://localhost:8000",
        use_pcu: bool = True,
        capacity_floor: float = 15.0,
        request_timeout: float = 10.0,
    ):
        self.base_url = base_url.rstrip("/")
        self.use_pcu = use_pcu
        self.capacity_floor = capacity_floor
        self.request_timeout = request_timeout
        self.capacities: Dict[str, float] = {}
        self.base_capacities: Dict[str, float] = {}

        # Load corridor design capacities from cameras_enriched.json (S6-T2 Issue #222)
        enriched_path = Path("cameras_enriched.json")
        if not enriched_path.is_file():
            enriched_path = Path(__file__).resolve().parent.parent.parent / "cameras_enriched.json"
        if enriched_path.is_file():
            try:
                with open(enriched_path, "r", encoding="utf-8") as f:
                    cams = json.load(f)
                    for c in cams:
                        cid = c.get("_id") or c.get("id")
                        bcap = c.get("base_capacity")
                        if cid and bcap is not None:
                            self.base_capacities[str(cid)] = float(bcap)
            except Exception:
                pass

        # PCU weights conforming to cvclient/pcu.go (calibrated per Issue #196) and ADR-005 T6
        self.pcu_weights = {
            "motorcycle": 0.3,
            "car": 1.0,
            "bus": 2.5,
            "truck": 2.0,
            "container": 2.0,
        }
        # Thresholds conforming to cvclient/pcu.go (calibrated per Issue #196)
        self.pcu_thresholds = {
            "moderate": 0.30,
            "heavy": 0.60,
            "congested": 0.80,
        }

    def _calculate_pcu(self, camera_id: str, counts: Dict[str, int], fallback_total: int) -> Tuple[str, float, float]:
        total_vehicles = sum(counts.values()) if counts else fallback_total
        if counts and total_vehicles > 0:
            pcu_total = (
                counts.get("motorcycle", 0) * self.pcu_weights["motorcycle"]
                + counts.get("car", 0) * self.pcu_weights["car"]
                + counts.get("bus", 0) * self.pcu_weights["bus"]
                + counts.get("truck", 0) * self.pcu_weights["truck"]
                + counts.get("container", 0) * self.pcu_weights["container"]
            )
        elif fallback_total > 0:
            pcu_total = float(fallback_total) * self.pcu_weights["car"]
        else:
            return "CLEAR", 0.0, 0.0

        hist_max = self.capacities.get(camera_id, 0.0)
        if pcu_total > hist_max:
            hist_max = pcu_total
            self.capacities[camera_id] = hist_max

        base_floor = self.base_capacities.get(camera_id, self.capacity_floor)
        eff_capacity = max(hist_max, base_floor)
        if eff_capacity < 1.0:
            eff_capacity = 1.0
        saturation_index = pcu_total / eff_capacity

        if saturation_index >= self.pcu_thresholds["congested"]:
            severity = "CONGESTED"
        elif saturation_index >= self.pcu_thresholds["heavy"]:
            severity = "HEAVY"
        elif saturation_index >= self.pcu_thresholds["moderate"]:
            severity = "MODERATE"
        else:
            severity = "CLEAR"

        return severity, pcu_total, saturation_index

    def analyze(self, frame: LabeledFrame) -> Prediction:
        if not frame.image_path.is_file():
            raise FileNotFoundError(f"Image not found for frame {frame.frame_id}: {frame.image_path}")

        image_bytes = frame.image_path.read_bytes()
        url = f"{self.base_url}/predict"
        req = urllib.request.Request(
            url,
            data=image_bytes,
            headers={"Content-Type": "application/octet-stream"},
            method="POST",
        )

        t0 = time.time()
        try:
            with urllib.request.urlopen(req, timeout=self.request_timeout) as resp:
                resp_bytes = resp.read()
            latency = (time.time() - t0) * 1000.0
            data = json.loads(resp_bytes.decode("utf-8"))
        except Exception as e:
            return Prediction(
                frame_id=frame.frame_id,
                traffic="UNKNOWN",
                flood="UNKNOWN",
                confidence=0.0,
                description=f"cv-engine request error: {e}",
                latency_ms=(time.time() - t0) * 1000.0,
            )

        model_name = data.get("model", self.model)
        self.model = model_name
        counts = data.get("vehicle_counts", {})
        vehicles = int(data.get("vehicles_detected", 0))
        confidence = float(data.get("confidence", 0.88))

        if self.use_pcu:
            traffic_sev, pcu_total, si = self._calculate_pcu(frame.camera_id, counts, vehicles)
            desc = f"PCU={pcu_total:.1f}, SI={si:.2f}, vehicles={vehicles}"
        else:
            traffic_sev = str(data.get("traffic_severity", "UNKNOWN")).upper()
            si = 0.0
            desc = f"Raw cv-engine severity, vehicles={vehicles}"

        # Per Finding B: cv-engine v1 is an object detector and does not detect flood.
        # It reports NONE for flood severity.
        flood_sev = "NONE"

        return Prediction(
            frame_id=frame.frame_id,
            traffic=traffic_sev,
            flood=flood_sev,
            confidence=confidence,
            description=desc,
            raw_response={**data, "saturation_index": si, "camera_id": frame.camera_id},
            latency_ms=latency,
        )


# ---------------------------------------------------------------------------
# Metrics Computation
# ---------------------------------------------------------------------------

def compute_confusion_matrix(
    y_true: List[str], y_pred: List[str], class_labels: List[str]
) -> ConfusionMatrix:
    """Builds an actual -> predicted -> count confusion matrix."""
    all_classes = list(class_labels)
    # Include UNKNOWN in columns/rows if present in predictions or ground truth
    extra_classes = set(y_true).union(set(y_pred)) - set(all_classes)
    for extra in sorted(extra_classes):
        all_classes.append(extra)

    matrix: Dict[str, Dict[str, int]] = {
        act: {pred: 0 for pred in all_classes} for act in all_classes
    }

    for act, pred in zip(y_true, y_pred):
        if act not in matrix:
            matrix[act] = {p: 0 for p in all_classes}
        if pred not in matrix[act]:
            for r in matrix.values():
                r[pred] = 0
        matrix[act][pred] += 1

    return ConfusionMatrix(classes=all_classes, matrix=matrix)


def compute_class_metrics(
    cm: ConfusionMatrix, eval_classes: List[str]
) -> Dict[str, ClassMetric]:
    """Computes TP, FP, FN, TN, precision, recall, f1 for each class."""
    metrics: Dict[str, ClassMetric] = {}
    total_samples = sum(
        sum(preds.values()) for preds in cm.matrix.values()
    )

    for c in eval_classes:
        tp = cm.get(c, c)
        actual_total = sum(cm.matrix.get(c, {}).values())
        fn = actual_total - tp

        predicted_total = sum(
            row.get(c, 0) for row in cm.matrix.values()
        )
        fp = predicted_total - tp
        tn = total_samples - (tp + fp + fn)

        metrics[c] = ClassMetric(
            cls_name=c,
            tp=tp,
            fp=fp,
            fn=fn,
            tn=tn,
            support=actual_total,
        )

    return metrics


def compute_summary_f1(metrics: Dict[str, ClassMetric]) -> Tuple[float, float]:
    """Calculates macro-average F1 and weighted-average F1."""
    classes_with_support = [m for m in metrics.values() if m.support > 0]
    if not classes_with_support:
        return 0.0, 0.0

    macro_f1 = sum(m.f1 for m in classes_with_support) / len(classes_with_support)
    total_support = sum(m.support for m in classes_with_support)
    weighted_f1 = (
        sum(m.f1 * m.support for m in classes_with_support) / total_support
        if total_support > 0
        else 0.0
    )
    return macro_f1, weighted_f1


def evaluate_flood_safety_gate(
    frames: List[LabeledFrame],
    predictions: List[Prediction],
    max_fnr: float = 0.05,
) -> FloodGateResult:
    """
    Computes flood safety accuracy gate metric: False-Negative Rate.
    Safety rule: Actual flood (SHALLOW or DEEP) must NEVER be missed (classified as NONE/UNKNOWN).
    """
    actual_positives = 0
    detected_positives = 0
    false_negatives = 0
    false_positives = 0
    true_negatives = 0
    missed_frames = []

    pred_map = {p.frame_id: p for p in predictions}

    for f in frames:
        p = pred_map.get(f.frame_id)
        is_actual_flood = f.flood in ("SHALLOW", "DEEP")
        pred_flood = p.flood if p else "UNKNOWN"
        is_pred_flood = pred_flood in ("SHALLOW", "DEEP")

        if is_actual_flood:
            actual_positives += 1
            if is_pred_flood:
                detected_positives += 1
            else:
                false_negatives += 1
                missed_frames.append({
                    "frame_id": f.frame_id,
                    "camera_id": f.camera_id,
                    "actual_flood": f.flood,
                    "predicted_flood": pred_flood,
                    "confidence": p.confidence if p else 0.0,
                    "weather": f.weather,
                    "time_of_day": f.time_of_day,
                    "notes": f.notes or "",
                })
        else:
            if is_pred_flood:
                false_positives += 1
            else:
                true_negatives += 1

    fnr = (false_negatives / actual_positives) if actual_positives > 0 else 0.0

    if actual_positives == 0:
        passed = False
        status_label = "INCOMPLETE (0 flood positive frames evaluated - accumulation window active)"
    elif fnr <= max_fnr:
        passed = True
        status_label = f"PASS ({fnr * 100:.1f}% FNR <= {max_fnr * 100:.1f}%)"
    else:
        passed = False
        status_label = f"FAIL ({fnr * 100:.1f}% FNR > {max_fnr * 100:.1f}%)"

    return FloodGateResult(
        actual_positives=actual_positives,
        detected_positives=detected_positives,
        false_negatives=false_negatives,
        false_positives=false_positives,
        true_negatives=true_negatives,
        false_negative_rate=fnr,
        max_fnr_threshold=max_fnr,
        passed=passed,
        status_label=status_label,
        missed_frames=missed_frames,
    )


def compute_slices(
    frames: List[LabeledFrame],
    predictions: List[Prediction],
    key: str,
) -> Dict[str, Dict[str, Any]]:
    """Evaluates accuracy broken down by environmental slice (weather or time_of_day)."""
    pred_map = {p.frame_id: p for p in predictions}
    groups: Dict[str, List[Tuple[LabeledFrame, Optional[Prediction]]]] = {}

    for f in frames:
        val = getattr(f, key, "unknown")
        groups.setdefault(val, []).append((f, pred_map.get(f.frame_id)))

    results = {}
    for slice_val, pairs in sorted(groups.items()):
        total = len(pairs)
        traffic_correct = sum(1 for f, p in pairs if p and p.traffic == f.traffic)
        flood_correct = sum(1 for f, p in pairs if p and p.flood == f.flood)
        results[slice_val] = {
            "total_frames": total,
            "traffic_accuracy": traffic_correct / total if total > 0 else 0.0,
            "flood_accuracy": flood_correct / total if total > 0 else 0.0,
        }
    return results


def evaluate_composition_progress(frames: List[LabeledFrame]) -> List[Dict[str, Any]]:
    """Evaluates dataset composition against ADR-004 §D6 target counts."""
    results = []
    for item in TARGET_COMPOSITION:
        cat = item["category"]
        tgt = item["target"]
        fn = item["filter"]
        count = sum(1 for f in frames if fn(f))
        pct = (count / tgt) * 100.0 if tgt > 0 else 0.0
        status = "[COMPLETE]" if count >= tgt else f"[{count}/{tgt} ({pct:.0f}%)]"
        results.append({
            "category": cat,
            "current": count,
            "target": tgt,
            "percentage": pct,
            "status": status,
        })
    return results


# ---------------------------------------------------------------------------
# Runner & Report Generation
# ---------------------------------------------------------------------------

def run_benchmark(
    frames: List[LabeledFrame],
    analyzer: BaseAnalyzer,
    schema: Dict[str, List[str]],
    labels_dir_str: str,
    max_flood_fnr: float = 0.05,
) -> BenchmarkResult:
    """Runs analyzer on frames and produces a full BenchmarkResult."""
    predictions: List[Prediction] = []

    for idx, frame in enumerate(frames, start=1):
        pred = analyzer.analyze(frame)
        predictions.append(pred)

    y_traffic_true = [f.traffic for f in frames]
    y_traffic_pred = [p.traffic for p in predictions]
    traffic_classes = schema["traffic"]
    traffic_cm = compute_confusion_matrix(y_traffic_true, y_traffic_pred, traffic_classes)
    traffic_metrics = compute_class_metrics(traffic_cm, traffic_classes)
    traffic_macro_f1, traffic_weighted_f1 = compute_summary_f1(traffic_metrics)

    y_flood_true = [f.flood for f in frames]
    y_flood_pred = [p.flood for p in predictions]
    flood_classes = schema["flood"]
    flood_cm = compute_confusion_matrix(y_flood_true, y_flood_pred, flood_classes)
    flood_metrics = compute_class_metrics(flood_cm, flood_classes)
    flood_macro_f1, flood_weighted_f1 = compute_summary_f1(flood_metrics)

    flood_gate = evaluate_flood_safety_gate(frames, predictions, max_fnr=max_flood_fnr)
    weather_slices = compute_slices(frames, predictions, "weather")
    tod_slices = compute_slices(frames, predictions, "time_of_day")
    comp_progress = evaluate_composition_progress(frames)

    pred_records = [
        {
            "frame_id": p.frame_id,
            "camera_id": getattr(f, "camera_id", ""),
            "traffic": p.traffic,
            "flood": p.flood,
            "confidence": p.confidence,
            "description": p.description,
            "latency_ms": p.latency_ms,
            "saturation_index": (
                p.raw_response.get("saturation_index")
                if isinstance(p.raw_response, dict)
                else None
            ),
        }
        for f, p in zip(frames, predictions)
    ]

    return BenchmarkResult(
        timestamp=datetime.now(timezone.utc).isoformat(),
        analyzer_name=analyzer.name,
        model_name=analyzer.model,
        total_frames=len(frames),
        labels_dir=labels_dir_str,
        traffic_classes=traffic_classes,
        traffic_metrics=traffic_metrics,
        traffic_macro_f1=traffic_macro_f1,
        traffic_weighted_f1=traffic_weighted_f1,
        traffic_cm=traffic_cm,
        flood_classes=flood_classes,
        flood_metrics=flood_metrics,
        flood_macro_f1=flood_macro_f1,
        flood_weighted_f1=flood_weighted_f1,
        flood_cm=flood_cm,
        flood_gate=flood_gate,
        weather_slices=weather_slices,
        time_of_day_slices=tod_slices,
        composition_progress=comp_progress,
        predictions=pred_records,
    )


def generate_markdown_report(res: BenchmarkResult) -> str:
    """Generates a complete, publication-ready Markdown report."""
    lines = []
    lines.append(f"# AI Analyzer Evaluation Report (ADR-004 §D6 Accuracy Gate)")
    lines.append("")
    lines.append(f"- **Generated At**: `{res.timestamp}`")
    lines.append(f"- **Analyzer**: `{res.analyzer_name}` (`{res.model_name}`)")
    lines.append(f"- **Evaluation Dataset**: `{res.total_frames}` labeled frames from `{res.labels_dir}`")
    lines.append("")

    # Executive Summary & Gate Status
    lines.append("## 1. Executive Summary & Accuracy Gate")
    lines.append("")
    if res.flood_gate.actual_positives == 0:
        gate_badge = "**INCOMPLETE** (0 flood frames evaluated; passive accumulation window active)"
    elif res.flood_gate.passed:
        gate_badge = "**PASS**"
    else:
        gate_badge = "**FAIL (SAFETY GATE BREACH)**"

    traffic_badge = "**[PASS]**" if res.traffic_macro_f1 >= 0.7 else "**[FAIL]** (Below 0.70 Target)"
    flood_badge = "**[PASS]**" if res.flood_macro_f1 >= 0.8 else "**[FAIL]** (Below 0.80 Target)"

    lines.append(f"| Gate Metric | Target | Actual Value | Verdict |")
    lines.append(f"|---|---|---|---|")
    lines.append(f"| **Flood False-Negative Rate (FNR)** | ≤ {res.flood_gate.max_fnr_threshold * 100:.1f}% | **{res.flood_gate.false_negative_rate * 100:.1f}%** ({res.flood_gate.false_negatives}/{res.flood_gate.actual_positives} missed) | {gate_badge} |")
    lines.append(f"| Traffic Macro F1 | ≥ 0.70 | **{res.traffic_macro_f1:.3f}** | {traffic_badge} |")
    lines.append(f"| Flood Macro F1 | ≥ 0.80 | **{res.flood_macro_f1:.3f}** | {flood_badge} |")
    lines.append("")
    lines.append(f"> **Safety Gate Rule (ADR-004 §D6)**: A flood false-negative (failing to detect standing water or deep floods) directs drivers into hazardous corridors. Until the flood FNR is rigorously ≤ {res.flood_gate.max_fnr_threshold * 100:.1f}% across ≥60 rain/flood frames, route verdicts cannot show an unrestricted 'clear' state.")
    lines.append("")

    # Target Dataset Progress
    lines.append("## 2. Dataset Composition Progress (Target: ≥300 Frames)")
    lines.append("")
    lines.append("| Category | Target | Current Count | Progress | Status |")
    lines.append("|---|---|---|---|---|")
    for cp in res.composition_progress:
        bar_len = min(10, int(cp["percentage"] / 10))
        bar = "=" * bar_len + "." * (10 - bar_len)
        lines.append(f"| {cp['category']} | {cp['target']} | {cp['current']} | `[{bar}]` {cp['percentage']:.0f}% | {cp['status']} |")
    lines.append("")

    # Traffic Classification
    lines.append("## 3. Traffic Classification Performance")
    lines.append("")
    lines.append("| Class | Precision | Recall | F1-Score | Support |")
    lines.append("|---|---|---|---|---|")
    for c in res.traffic_classes:
        m = res.traffic_metrics.get(c)
        if m:
            lines.append(f"| **{c}** | {m.precision:.3f} | {m.recall:.3f} | {m.f1:.3f} | {m.support} |")
    lines.append(f"| *Macro Avg* | - | - | **{res.traffic_macro_f1:.3f}** | {res.total_frames} |")
    lines.append(f"| *Weighted Avg* | - | - | **{res.traffic_weighted_f1:.3f}** | {res.total_frames} |")
    lines.append("")

    # Traffic Confusion Matrix
    lines.append("### Traffic Confusion Matrix")
    lines.append("")
    header = "| Actual \\ Predicted | " + " | ".join(res.traffic_cm.classes) + " | Total |"
    sep = "|---|" + "|".join(["---"] * len(res.traffic_cm.classes)) + "|---|"
    lines.append(header)
    lines.append(sep)
    for act in res.traffic_cm.classes:
        counts = [str(res.traffic_cm.get(act, pred)) for pred in res.traffic_cm.classes]
        row_tot = sum(res.traffic_cm.get(act, pred) for pred in res.traffic_cm.classes)
        lines.append(f"| **{act}** | " + " | ".join(counts) + f" | **{row_tot}** |")
    lines.append("")

    # Flood Classification
    lines.append("## 4. Flood Classification Performance")
    lines.append("")
    lines.append("| Class | Precision | Recall | F1-Score | Support |")
    lines.append("|---|---|---|---|---|")
    for c in res.flood_classes:
        m = res.flood_metrics.get(c)
        if m:
            lines.append(f"| **{c}** | {m.precision:.3f} | {m.recall:.3f} | {m.f1:.3f} | {m.support} |")
    lines.append(f"| *Macro Avg* | - | - | **{res.flood_macro_f1:.3f}** | {res.total_frames} |")
    lines.append(f"| *Weighted Avg* | - | - | **{res.flood_weighted_f1:.3f}** | {res.total_frames} |")
    lines.append("")

    # Flood Confusion Matrix
    lines.append("### Flood Confusion Matrix")
    lines.append("")
    header = "| Actual \\ Predicted | " + " | ".join(res.flood_cm.classes) + " | Total |"
    sep = "|---|" + "|".join(["---"] * len(res.flood_cm.classes)) + "|---|"
    lines.append(header)
    lines.append(sep)
    for act in res.flood_cm.classes:
        counts = [str(res.flood_cm.get(act, pred)) for pred in res.flood_cm.classes]
        row_tot = sum(res.flood_cm.get(act, pred) for pred in res.flood_cm.classes)
        lines.append(f"| **{act}** | " + " | ".join(counts) + f" | **{row_tot}** |")
    lines.append("")

    # Flood False-Negative Diagnostic
    lines.append("## 5. Flood False-Negative Diagnostic (Missed Floods)")
    lines.append("")
    if res.flood_gate.missed_frames:
        lines.append(f"[WARN] **{len(res.flood_gate.missed_frames)} flood instances were missed by the analyzer.**")
        lines.append("")
        lines.append("| Frame ID | Camera ID | Actual Flood | Predicted | Confidence | Weather | Time of Day | Notes |")
        lines.append("|---|---|---|---|---|---|---|---|")
        for mf in res.flood_gate.missed_frames:
            lines.append(f"| `{mf['frame_id']}` | `{mf['camera_id']}` | **{mf['actual_flood']}** | `{mf['predicted_flood']}` | {mf['confidence']:.2f} | {mf['weather']} | {mf['time_of_day']} | {mf['notes']} |")
    else:
        lines.append("**Zero false-negative flood events detected in evaluated frames.**")
    lines.append("")

    # Environmental Slices
    lines.append("## 6. Sliced Analysis (Weather & Lighting)")
    lines.append("")
    lines.append("### Weather Slice")
    lines.append("")
    lines.append("| Weather Condition | Frames | Traffic Accuracy | Flood Accuracy |")
    lines.append("|---|---|---|---|")
    for w, data in res.weather_slices.items():
        lines.append(f"| **{w}** | {data['total_frames']} | {data['traffic_accuracy'] * 100:.1f}% | {data['flood_accuracy'] * 100:.1f}% |")
    lines.append("")

    lines.append("### Time of Day Slice")
    lines.append("")
    lines.append("| Lighting / Time | Frames | Traffic Accuracy | Flood Accuracy |")
    lines.append("|---|---|---|---|")
    for tod, data in res.time_of_day_slices.items():
        lines.append(f"| **{tod}** | {data['total_frames']} | {data['traffic_accuracy'] * 100:.1f}% | {data['flood_accuracy'] * 100:.1f}% |")
    lines.append("")

    return "\n".join(lines)


def print_cli_summary(res: BenchmarkResult):
    """Prints a clean ASCII summary to console."""
    sep = "=" * 64
    print(sep)
    print(" PROJECTKV AI ANALYZER BENCHMARK SUMMARY")
    print(sep)
    print(f" Timestamp:      {res.timestamp}")
    print(f" Analyzer:       {res.analyzer_name} ({res.model_name})")
    print(f" Dataset:        {res.total_frames} frames from {res.labels_dir}")
    print(f" Safety Gate:    {res.flood_gate.status_label}")
    print(f" Flood FNR:      {res.flood_gate.false_negative_rate * 100:.2f}% (Threshold: ≤ {res.flood_gate.max_fnr_threshold * 100:.1f}%)")
    print(f" Traffic F1:     Macro={res.traffic_macro_f1:.3f} | Weighted={res.traffic_weighted_f1:.3f}")
    print(f" Flood F1:       Macro={res.flood_macro_f1:.3f} | Weighted={res.flood_weighted_f1:.3f}")
    print(sep)


# ---------------------------------------------------------------------------
# CLI Entry Point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Evaluate traffic & flood AI analyzer on labeled camera frames.")
    parser.add_argument("--labels-dir", default="tools/eval/labels", help="Directory with label JSON files")
    parser.add_argument("--labels-file", default=None, help="Path to aggregated label JSON file")
    parser.add_argument("--fixtures-dir", default=None, help="Optional fixture directory with frames and labels")
    parser.add_argument("--schema", default="tools/eval/label_schema.json", help="Path to label schema JSON")
    parser.add_argument("--analyzer", choices=["mock", "vlm", "predictions", "cv-engine", "openvino"], default="mock", help="Analyzer backend to benchmark")
    parser.add_argument("--model", default="gemini-3.5-flash-lite", help="Gemini VLM model name (for --analyzer vlm)")
    parser.add_argument("--api-key", default=None, help="Gemini API key (or set CLOUD_VISION_API_KEY env var)")
    parser.add_argument("--predictions-file", default=None, help="JSON file with predictions (for --analyzer predictions)")
    parser.add_argument("--cv-engine-url", default="http://localhost:8000", help="Base URL of cv-engine service (for --analyzer cv-engine / openvino)")
    parser.add_argument("--no-pcu", dest="use_pcu", action="store_false", help="Disable PCU saturation calculation and use raw cv-engine severity")
    parser.set_defaults(use_pcu=True)
    parser.add_argument("--output-report", default=None, help="File path to write markdown report")
    parser.add_argument("--output-json", default=None, help="File path to write JSON metrics")
    parser.add_argument("--gate-max-fnr", type=float, default=0.05, help="Maximum allowed flood false-negative rate (default 0.05 = 5%%)")
    parser.add_argument("--strict", action="store_true", help="Exit with code 1 if accuracy gate fails")
    parser.add_argument("--simulate-flood-miss", action="store_true", help="Simulate flood detection miss in mock mode (for gate testing)")
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parent.parent.parent
    schema_path = Path(args.schema) if Path(args.schema).is_absolute() else repo_root / args.schema
    schema = load_schema(schema_path if schema_path.exists() else None)

    # Determine labels path
    if args.labels_file:
        labels_path = Path(args.labels_file) if Path(args.labels_file).is_absolute() else repo_root / args.labels_file
    elif args.fixtures_dir:
        labels_path = Path(args.fixtures_dir)
        if (labels_path / "labels").is_dir():
            labels_path = labels_path / "labels"
    else:
        labels_path = Path(args.labels_dir) if Path(args.labels_dir).is_absolute() else repo_root / args.labels_dir

    print(f"[INFO] Loading dataset from: {labels_path}")
    frames = load_dataset(labels_path, schema, repo_root)
    if not frames:
        print(f"[ERROR] No valid labeled frames found in {labels_path}", file=sys.stderr)
        sys.exit(1)

    print(f"[INFO] Loaded {len(frames)} labeled frames")

    # Instantiate analyzer
    if args.analyzer == "mock":
        analyzer = MockAnalyzer(simulate_flood_miss=args.simulate_flood_miss)
    elif args.analyzer in ("cv-engine", "openvino"):
        analyzer = CVEngineAnalyzer(
            base_url=args.cv_engine_url,
            use_pcu=args.use_pcu,
        )
    elif args.analyzer == "predictions":
        if not args.predictions_file:
            print("[ERROR] --predictions-file is required when --analyzer=predictions", file=sys.stderr)
            sys.exit(1)
        preds_path = Path(args.predictions_file) if Path(args.predictions_file).is_absolute() else repo_root / args.predictions_file
        analyzer = PrecomputedPredictionsAnalyzer(preds_path)
    elif args.analyzer == "vlm":
        api_key = args.api_key or os.environ.get("CLOUD_VISION_API_KEY") or os.environ.get("GEMINI_API_KEY")
        if not api_key:
            print("[ERROR] Gemini API key required for VLM analyzer (pass --api-key or set CLOUD_VISION_API_KEY)", file=sys.stderr)
            sys.exit(1)
        analyzer = VLMAnalyzer(api_key=api_key, model=args.model)
    else:
        print(f"[ERROR] Unsupported analyzer: {args.analyzer}", file=sys.stderr)
        sys.exit(1)

    print(f"[INFO] Running benchmark using analyzer: {analyzer.name} ({analyzer.model})...")
    result = run_benchmark(
        frames=frames,
        analyzer=analyzer,
        schema=schema,
        labels_dir_str=str(labels_path),
        max_flood_fnr=args.gate_max_fnr,
    )

    print_cli_summary(result)

    # Write report if requested
    report_md = generate_markdown_report(result)
    if args.output_report:
        report_path = Path(args.output_report) if Path(args.output_report).is_absolute() else repo_root / args.output_report
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(report_md, encoding="utf-8")
        print(f"[INFO] Wrote report to: {report_path}")

    # Write JSON metrics if requested or when output_report is requested for real evaluation
    json_path_target = args.output_json or (
        (repo_root / "predictions_real.json") if (args.output_report and not args.fixtures_dir) else None
    )
    if json_path_target:
        json_path = Path(json_path_target) if Path(json_path_target).is_absolute() else repo_root / json_path_target
        json_path.parent.mkdir(parents=True, exist_ok=True)
        
        # Serialize result cleanly
        export_data = {
            "timestamp": result.timestamp,
            "analyzer": result.analyzer_name,
            "model": result.model_name,
            "total_frames": result.total_frames,
            "flood_gate": asdict(result.flood_gate),
            "traffic_macro_f1": result.traffic_macro_f1,
            "traffic_weighted_f1": result.traffic_weighted_f1,
            "flood_macro_f1": result.flood_macro_f1,
            "flood_weighted_f1": result.flood_weighted_f1,
            "traffic_metrics": {k: asdict(v) for k, v in result.traffic_metrics.items()},
            "flood_metrics": {k: asdict(v) for k, v in result.flood_metrics.items()},
            "weather_slices": result.weather_slices,
            "time_of_day_slices": result.time_of_day_slices,
            "predictions": result.predictions,
        }
        json_path.write_text(json.dumps(export_data, indent=2) + "\n", encoding="utf-8")
        print(f"[INFO] Wrote JSON metrics to: {json_path}")
        if json_path.name != "predictions_real.json" and not args.fixtures_dir:
            (repo_root / "predictions_real.json").write_text(json.dumps(export_data, indent=2) + "\n", encoding="utf-8")

    if args.strict and not result.flood_gate.passed:
        print("[FAIL] Accuracy gate breached. Exiting with code 1.", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
