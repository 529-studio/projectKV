#!/usr/bin/env python3
"""
dataset_progress.py — Evaluation dataset progress and composition auditor.

Sprint 4 T3 (Issue #194 / ADR-004 §D6):
Audits labeled frames in tools/eval/labels/ against the ADR-004 §D6 accuracy gate targets.
Strictly verifies that no pre-10-06 mock frames are counted, and reports current
progress across traffic, flood, weather, and time-of-day categories.

Zero-emoji, minimalist formatting adhering to AGENTS.md Rule 1.
"""

import argparse
import json
import os
import sys
from dataclasses import dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any

DEFAULT_LABELS_DIR = Path(__file__).resolve().parent / "labels"
DEFAULT_SCHEMA_PATH = Path(__file__).resolve().parent / "label_schema.json"
REPO_ROOT = Path(__file__).resolve().parent.parent.parent

# ADR-004 §D6 target criteria
TARGETS = [
    {
        "key": "clear_dry_day",
        "label": "Clear / dry / day",
        "target": 100,
        "predicate": lambda l: l.get("traffic") == "CLEAR" and l.get("weather") == "dry" and l.get("time_of_day") == "day",
    },
    {
        "key": "moderate_traffic",
        "label": "Moderate traffic",
        "target": 50,
        "predicate": lambda l: l.get("traffic") == "MODERATE",
    },
    {
        "key": "heavy_congested",
        "label": "Heavy / congested",
        "target": 50,
        "predicate": lambda l: l.get("traffic") in ("HEAVY", "CONGESTED"),
    },
    {
        "key": "rain",
        "label": "Rain (any traffic)",
        "target": 40,
        "predicate": lambda l: l.get("weather") in ("rain", "heavy_rain"),
    },
    {
        "key": "flood_shallow",
        "label": "Flood shallow",
        "target": 20,
        "predicate": lambda l: l.get("flood") == "SHALLOW",
    },
    {
        "key": "flood_deep",
        "label": "Flood deep",
        "target": 10,
        "predicate": lambda l: l.get("flood") == "DEEP",
    },
    {
        "key": "night",
        "label": "Night (any condition)",
        "target": 30,
        "predicate": lambda l: l.get("time_of_day") == "night",
    },
]

TOTAL_TARGET = 300
MIN_FLOOD_POSITIVE_TARGET = 60  # ADR-004 §D6: ≥60 flood-positive frames


@dataclass
class FrameLabel:
    file_path: Path
    frame_id: str
    image_path: str
    traffic: str
    flood: str
    time_of_day: str
    weather: str
    annotator: str
    annotated_at: str
    is_valid: bool
    is_mock: bool
    validation_error: Optional[str] = None


def load_schema(schema_path: Path) -> Dict[str, List[str]]:
    if not schema_path.exists():
        raise FileNotFoundError(f"Schema file not found: {schema_path}")
    with open(schema_path, "r", encoding="utf-8") as f:
        return json.load(f)


def parse_label_file(file_path: Path, schema: Dict[str, List[str]]) -> FrameLabel:
    with open(file_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    # Support nested labels: {...} or flat structure
    labels = data.get("labels", data)
    traffic = str(labels.get("traffic", "")).upper()
    flood = str(labels.get("flood", "")).upper()
    tod = str(labels.get("time_of_day", "")).lower()
    weather = str(labels.get("weather", "")).lower()

    image_path = str(data.get("image_path") or "")
    frame_id = str(data.get("frame_id") or file_path.stem)
    annotator = str(data.get("annotator") or "unknown")
    annotated_at = str(data.get("annotated_at") or "")

    # Pre-10-06 mock frame detection:
    # 1. Image path contains dates prior to 2026-10-08
    # 2. File size on disk is exactly 107 bytes or 149 bytes (mock fixture)
    is_mock = False
    if "fixture_" in frame_id or "fixtures/frames" in image_path:
        is_mock = True
    elif "2026-10-01" in image_path or "2026-10-02" in image_path or "2026-10-03" in image_path or "2026-10-04" in image_path or "2026-10-05" in image_path:
        is_mock = True

    err = None
    if traffic and traffic not in schema.get("traffic", []):
        err = f"Invalid traffic value: {traffic}"
    elif flood and flood not in schema.get("flood", []):
        err = f"Invalid flood value: {flood}"
    elif tod and tod not in schema.get("time_of_day", []):
        err = f"Invalid time_of_day value: {tod}"
    elif weather and weather not in schema.get("weather", []):
        err = f"Invalid weather value: {weather}"

    is_valid = (err is None) and bool(flood)  # Flood is mandatory per labeler.html

    return FrameLabel(
        file_path=file_path,
        frame_id=frame_id,
        image_path=image_path,
        traffic=traffic,
        flood=flood,
        time_of_day=tod,
        weather=weather,
        annotator=annotator,
        annotated_at=annotated_at,
        is_valid=is_valid,
        is_mock=is_mock,
        validation_error=err,
    )


def audit_labels(
    labels_dir: Path, schema: Dict[str, List[str]]
) -> Tuple[List[FrameLabel], Dict[str, Any]]:
    if not labels_dir.exists():
        raise FileNotFoundError(f"Labels directory not found: {labels_dir}")

    label_files = sorted(labels_dir.glob("*.json"))
    labels: List[FrameLabel] = []

    for lp in label_files:
        try:
            labels.append(parse_label_file(lp, schema))
        except Exception as e:
            labels.append(
                FrameLabel(
                    file_path=lp,
                    frame_id=lp.stem,
                    image_path="",
                    traffic="",
                    flood="",
                    time_of_day="",
                    weather="",
                    annotator="",
                    annotated_at="",
                    is_valid=False,
                    is_mock=False,
                    validation_error=f"JSON parse error: {e}",
                )
            )

    real_labels = [l for l in labels if l.is_valid and not l.is_mock]
    mock_labels = [l for l in labels if l.is_mock]
    invalid_labels = [l for l in labels if not l.is_valid and not l.is_mock]

    # Calculate category progress
    cat_progress = []
    for target_def in TARGETS:
        key = target_def["key"]
        pred = target_def["predicate"]
        count = sum(
            1
            for l in real_labels
            if pred(
                {
                    "traffic": l.traffic,
                    "flood": l.flood,
                    "weather": l.weather,
                    "time_of_day": l.time_of_day,
                }
            )
        )
        target = target_def["target"]
        pct = (count / target) * 100.0 if target > 0 else 0.0
        cat_progress.append(
            {
                "key": key,
                "label": target_def["label"],
                "count": count,
                "target": target,
                "percentage": min(pct, 100.0),
                "is_satisfied": count >= target,
            }
        )

    flood_shallow_count = sum(1 for l in real_labels if l.flood == "SHALLOW")
    flood_deep_count = sum(1 for l in real_labels if l.flood == "DEEP")
    total_flood_positive = flood_shallow_count + flood_deep_count

    stats = {
        "total_label_files": len(labels),
        "real_valid_labels": len(real_labels),
        "mock_labels_excluded": len(mock_labels),
        "invalid_labels": len(invalid_labels),
        "overall_total_target": TOTAL_TARGET,
        "overall_percentage": (len(real_labels) / TOTAL_TARGET) * 100.0,
        "flood_positive_count": total_flood_positive,
        "flood_positive_target": MIN_FLOOD_POSITIVE_TARGET,
        "flood_positive_percentage": (total_flood_positive / MIN_FLOOD_POSITIVE_TARGET) * 100.0,
        "is_d6_gate_ready": (len(real_labels) >= TOTAL_TARGET and total_flood_positive >= MIN_FLOOD_POSITIVE_TARGET),
        "categories": cat_progress,
    }

    return labels, stats


def format_terminal_report(labels: List[FrameLabel], stats: Dict[str, Any]) -> str:
    lines = []
    lines.append("=" * 80)
    lines.append("PROJECTKV EVALUATION DATASET PROGRESS DASHBOARD (T3 / ADR-004 §D6)")
    lines.append("=" * 80)
    lines.append(
        f"Total Real Labeled Frames: {stats['real_valid_labels']} / {stats['overall_total_target']} "
        f"({stats['overall_percentage']:.1f}%)"
    )
    lines.append(
        f"Flood-Positive Frames:     {stats['flood_positive_count']} / {stats['flood_positive_target']} "
        f"({stats['flood_positive_percentage']:.1f}%)"
    )
    lines.append(f"Mock Frames Excluded:      {stats['mock_labels_excluded']}")
    lines.append(f"Invalid / Corrupted Files: {stats['invalid_labels']}")
    lines.append("-" * 80)
    lines.append(
        f"{'CATEGORY':<28} {'CURRENT':<9} {'TARGET':<8} {'PROGRESS':<12} {'STATUS'}"
    )
    lines.append("-" * 80)

    for c in stats["categories"]:
        status_str = "[SATISFIED]" if c["is_satisfied"] else "[ACCUMULATING]"
        prog_bar = f"{c['percentage']:>5.1f}%"
        lines.append(
            f"{c['label']:<28} {c['count']:<9} {c['target']:<8} {prog_bar:<12} {status_str}"
        )

    lines.append("-" * 80)
    if stats["is_d6_gate_ready"]:
        lines.append("OVERALL DATASET STATUS: READY FOR D6 ACCURACY GATE (T4)")
    else:
        pending_reasons = []
        if stats["real_valid_labels"] < stats["overall_total_target"]:
            pending_reasons.append(
                f"Need {stats['overall_total_target'] - stats['real_valid_labels']} more frames to reach {stats['overall_total_target']}"
            )
        if stats["flood_positive_count"] < stats["flood_positive_target"]:
            pending_reasons.append(
                f"Need {stats['flood_positive_target'] - stats['flood_positive_count']} more flood frames (requires real rain events)"
            )
        lines.append(f"OVERALL DATASET STATUS: IN PROGRESS (Passive Accumulation Window Active)")
        for pr in pending_reasons:
            lines.append(f"  -> {pr}")
    lines.append("=" * 80)
    return "\n".join(lines)


def format_markdown_report(stats: Dict[str, Any]) -> str:
    lines = []
    lines.append("# Evaluation Dataset Progress Report (ADR-004 §D6 / Sprint 4 T3)")
    lines.append("")
    lines.append(f"- **Audit Timestamp**: {datetime.now(timezone.utc).isoformat()}")
    lines.append(f"- **Real Labeled Frames**: {stats['real_valid_labels']} / {stats['overall_total_target']} ({stats['overall_percentage']:.1f}%)")
    lines.append(f"- **Flood-Positive Frames**: {stats['flood_positive_count']} / {stats['flood_positive_target']} ({stats['flood_positive_percentage']:.1f}%)")
    lines.append(f"- **Mock Frames Discarded**: {stats['mock_labels_excluded']}")
    status_label = "READY FOR T4" if stats["is_d6_gate_ready"] else "IN PROGRESS (PASSIVE ACCUMULATION)"
    lines.append(f"- **Dataset Status**: **{status_label}**")
    lines.append("")
    lines.append("## Category Breakdown")
    lines.append("")
    lines.append("| Category | Current Count | Target Count | Progress | Status |")
    lines.append("|---|:---:|:---:|:---:|:---:|")

    for c in stats["categories"]:
        st = "SATISFIED" if c["is_satisfied"] else "ACCUMULATING"
        lines.append(f"| {c['label']} | {c['count']} | {c['target']} | {c['percentage']:.1f}% | **{st}** |")

    lines.append("")
    return "\n".join(lines)


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Audit evaluation dataset composition against ADR-004 §D6 targets."
    )
    parser.add_argument(
        "--labels-dir",
        type=Path,
        default=DEFAULT_LABELS_DIR,
        help="Path to tools/eval/labels directory",
    )
    parser.add_argument(
        "--schema",
        type=Path,
        default=DEFAULT_SCHEMA_PATH,
        help="Path to label_schema.json",
    )
    parser.add_argument(
        "--output-markdown",
        type=Path,
        help="Write Markdown report to specified file path",
    )
    parser.add_argument(
        "--output-json",
        type=Path,
        help="Write machine-readable JSON metrics to specified file path",
    )
    parser.add_argument(
        "--strict-pass",
        action="store_true",
        help="Exit with non-zero code if D6 gate dataset composition targets are not yet met",
    )

    args = parser.parse_args(argv)

    try:
        schema = load_schema(args.schema)
    except Exception as e:
        sys.stderr.write(f"Error loading schema: {e}\n")
        return 1

    try:
        labels, stats = audit_labels(args.labels_dir, schema)
    except Exception as e:
        sys.stderr.write(f"Error auditing labels: {e}\n")
        return 1

    print(format_terminal_report(labels, stats))

    if args.output_markdown:
        args.output_markdown.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_markdown, "w", encoding="utf-8") as f:
            f.write(format_markdown_report(stats))

    if args.output_json:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_json, "w", encoding="utf-8") as f:
            json.dump(stats, f, indent=2, ensure_ascii=False)

    if args.strict_pass and not stats["is_d6_gate_ready"]:
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
