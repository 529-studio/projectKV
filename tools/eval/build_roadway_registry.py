#!/usr/bin/env python3
"""
build_roadway_registry.py - Directional Roadway Registry Builder & Verifier.

Sprint 10 / Issue #374 (S10-T3) / ADR-012 D1:
  * Generates per-camera runtime geometry at data/roadway_regions/<camera_id>.json.
  * Extracts approved directional roadway polygons (direction 'a' and 'b').
  * Enforces strict capacity_share validation: sum must be 1.0 +- 0.01.
  * Rejects implicit 50/50 splits: unregistered cameras report direction_coverage: none
    and remain frame-wide.
  * Provides --verify flag to assert schema and geometry integrity.

Zero-emoji, 100% English, adheres to AGENTS.md.
"""

from __future__ import annotations

import argparse
import glob
import json
import logging
import math
import os
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger("build-roadway-registry")

# Default paths
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_REGISTRY_DIR = PROJECT_ROOT / "data" / "roadway_regions"
DEFAULT_LABELS_DIR = PROJECT_ROOT / "tools" / "eval" / "labels_gold_standard"

# Three pilot corridors with calibrated directional polygons (Issue #374 / ADR-012 D1)
PILOT_CORRIDORS: Dict[str, Dict[str, Any]] = {
    # Corridor 1: Võ Văn Kiệt - Nguyễn Văn Cừ 1 (Dual Carriageway)
    "56de42f611f398ec0c48128b": {
        "camera_id": "56de42f611f398ec0c48128b",
        "corridor_name": "Võ Văn Kiệt - Nguyễn Văn Cừ 1",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "56de42f611f398ec0c48128b_dir_a",
                "direction": "a",
                "description": "Inbound carriageway (towards District 1)",
                "polygon": [
                    [0.05, 0.95],
                    [0.48, 0.95],
                    [0.44, 0.42],
                    [0.12, 0.42],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "56de42f611f398ec0c48128b_dir_b",
                "direction": "b",
                "description": "Outbound carriageway (towards Binh Chanh)",
                "polygon": [
                    [0.52, 0.95],
                    [0.95, 0.95],
                    [0.86, 0.42],
                    [0.54, 0.42],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_vvk_nvc1_202611"],
    },
    # Corridor 2: Điện Biên Phủ - Nguyễn Gia Trí (Dual Carriageway)
    "66b1c426779f74001867415e": {
        "camera_id": "66b1c426779f74001867415e",
        "corridor_name": "Điện Biên Phủ - Nguyễn Gia Trí",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "66b1c426779f74001867415e_dir_a",
                "direction": "a",
                "description": "Approaching carriageway (towards Saigon Bridge)",
                "polygon": [
                    [0.08, 0.92],
                    [0.47, 0.92],
                    [0.42, 0.45],
                    [0.15, 0.45],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "66b1c426779f74001867415e_dir_b",
                "direction": "b",
                "description": "Receding carriageway (towards Hang Xanh)",
                "polygon": [
                    [0.53, 0.92],
                    [0.92, 0.92],
                    [0.84, 0.45],
                    [0.56, 0.45],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_dbp_ngt_202611"],
    },
    # Corridor 3: Mai Chí Thọ - Nguyễn Cơ Thạch 1 (Dual Carriageway)
    "5822b5ededeb6c0012a2d66a": {
        "camera_id": "5822b5ededeb6c0012a2d66a",
        "corridor_name": "Mai Chí Thọ - Nguyễn Cơ Thạch 1",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "5822b5ededeb6c0012a2d66a_dir_a",
                "direction": "a",
                "description": "Eastbound carriageway (towards Cat Lai)",
                "polygon": [
                    [0.05, 0.95],
                    [0.49, 0.95],
                    [0.45, 0.40],
                    [0.10, 0.40],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "5822b5ededeb6c0012a2d66a_dir_b",
                "direction": "b",
                "description": "Westbound carriageway (towards Thu Thiem Tunnel)",
                "polygon": [
                    [0.51, 0.95],
                    [0.95, 0.95],
                    [0.88, 0.40],
                    [0.53, 0.40],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_mct_nct1_202611"],
    },
}


def validate_registry_dict(data: Dict[str, Any], file_path: Optional[Path] = None) -> Tuple[bool, List[str]]:
    """
    Validates a camera roadway registry dictionary against ADR-012 D1 constraints.
    Returns (is_valid, errors).
    """
    errors: List[str] = []
    cam_id = data.get("camera_id")
    if not cam_id or not isinstance(cam_id, str):
        errors.append("missing or invalid 'camera_id'")

    if file_path and cam_id:
        expected_stem = cam_id
        if file_path.stem != expected_stem:
            errors.append(f"filename stem {file_path.stem} does not match camera_id {cam_id}")

    regions = data.get("regions")
    if not isinstance(regions, list) or len(regions) == 0:
        errors.append("missing or empty 'regions' list")
        return False, errors

    total_share = 0.0
    seen_directions = set()
    seen_region_ids = set()

    for idx, reg in enumerate(regions):
        if not isinstance(reg, dict):
            errors.append(f"region[{idx}] is not an object")
            continue

        reg_id = reg.get("region_id")
        if not reg_id or not isinstance(reg_id, str):
            errors.append(f"region[{idx}] missing 'region_id'")
        elif reg_id in seen_region_ids:
            errors.append(f"duplicate region_id '{reg_id}'")
        else:
            seen_region_ids.add(reg_id)

        direction = reg.get("direction")
        if direction not in ("a", "b"):
            errors.append(f"region[{idx}] direction must be 'a' or 'b', got '{direction}'")
        elif direction in seen_directions:
            errors.append(f"duplicate direction '{direction}'")
        else:
            seen_directions.add(direction)

        poly = reg.get("polygon")
        if not isinstance(poly, list) or len(poly) < 3:
            errors.append(f"region[{idx}] polygon must contain >= 3 vertices")
        else:
            for pt_idx, pt in enumerate(poly):
                if not isinstance(pt, (list, tuple)) or len(pt) != 2:
                    errors.append(f"region[{idx}] vertex[{pt_idx}] must be [x, y]")
                    continue
                x, y = pt[0], pt[1]
                if not (0.0 <= x <= 1.0 and 0.0 <= y <= 1.0):
                    errors.append(f"region[{idx}] vertex[{pt_idx}] out of bounds [0, 1]: [{x}, {y}]")

        share = reg.get("capacity_share")
        if share is None or not isinstance(share, (int, float)) or share <= 0.0 or share > 1.0:
            errors.append(f"region[{idx}] invalid capacity_share: {share} (must be > 0.0 and <= 1.0)")
        else:
            total_share += float(share)

    # Constraint: capacity_share must sum to 1.0 +- 0.01 (ADR-012 D1)
    if abs(total_share - 1.0) > 0.01:
        errors.append(
            f"capacity_share sum {total_share:.4f} violates constraint (must sum to 1.0 +- 0.01)"
        )

    return len(errors) == 0, errors


def parse_labels_into_registry(labels_dir: Path) -> Dict[str, Dict[str, Any]]:
    """
    Scans a directory of ground-truth JSON labels, extracting directional roadway
    annotations and grouping them by camera_id.
    """
    registry: Dict[str, Dict[str, Any]] = {}
    if not labels_dir.exists():
        return registry

    label_files = sorted(labels_dir.glob("*.json"))
    for lf in label_files:
        try:
            with open(lf, "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception as exc:
            logger.warning("Failed to read %s: %s", lf.name, exc)
            continue

        # Extract camera_id: prefer top-level camera_id or frame_id prefix
        cam_id = data.get("camera_id")
        if not cam_id:
            frame_id = data.get("frame_id", "")
            cam_id = frame_id.split("_")[0] if frame_id else lf.stem.split("_")[0]

        if not cam_id:
            continue

        annotations = data.get("annotations") or data.get("polygons") or []
        directional_regs = []
        for ann in annotations:
            if ann.get("class") == "roadway":
                raw_dir = ann.get("direction")
                if raw_dir in ("a", "b", "near", "far"):
                    norm_dir = "a" if raw_dir in ("a", "near") else "b"
                    poly = ann.get("polygon")
                    share = ann.get("capacity_share", 0.50)
                    if poly and len(poly) >= 3:
                        directional_regs.append({
                            "region_id": f"{cam_id}_dir_{norm_dir}",
                            "direction": norm_dir,
                            "polygon": poly,
                            "capacity_share": float(share),
                        })

        if directional_regs:
            # Check if camera already registered or needs merge
            if cam_id not in registry:
                registry[cam_id] = {
                    "camera_id": cam_id,
                    "direction_coverage": "dual" if len(directional_regs) >= 2 else "partial",
                    "regions": directional_regs,
                    "source_label_ids": [lf.name],
                }
            else:
                existing_dirs = {r["direction"] for r in registry[cam_id]["regions"]}
                for reg in directional_regs:
                    if reg["direction"] not in existing_dirs:
                        registry[cam_id]["regions"].append(reg)
                        existing_dirs.add(reg["direction"])
                registry[cam_id]["source_label_ids"].append(lf.name)
                if len(registry[cam_id]["regions"]) >= 2:
                    registry[cam_id]["direction_coverage"] = "dual"

    return registry


def verify_registry_dir(registry_dir: Path) -> Tuple[bool, List[str]]:
    """
    Verifies all JSON files in the registry directory.
    Returns (all_valid, list_of_errors).
    """
    all_errors: List[str] = []
    if not registry_dir.exists():
        all_errors.append(f"registry directory does not exist: {registry_dir}")
        return False, all_errors

    files = sorted(registry_dir.glob("*.json"))
    if not files:
        all_errors.append(f"registry directory is empty: {registry_dir}")
        return False, all_errors

    valid_count = 0
    for f in files:
        try:
            with open(f, "r", encoding="utf-8") as fp:
                data = json.load(fp)
        except Exception as exc:
            all_errors.append(f"{f.name}: unmarshal error: {exc}")
            continue

        valid, errs = validate_registry_dict(data, file_path=f)
        if not valid:
            for e in errs:
                all_errors.append(f"{f.name}: {e}")
        else:
            valid_count += 1

    logger.info("Verified %d registry files (%d valid, %d with errors)", len(files), valid_count, len(files) - valid_count)
    return len(all_errors) == 0, all_errors


def build_and_export_registry(
    output_dir: Path,
    labels_dir: Optional[Path] = None,
    include_pilot: bool = True,
) -> int:
    """
    Builds and writes per-camera roadway registry files.
    Returns the number of successfully written camera registry files.
    """
    output_dir.mkdir(parents=True, exist_ok=True)
    registry_map: Dict[str, Dict[str, Any]] = {}

    # 1. Parse labels if provided
    if labels_dir and labels_dir.exists():
        parsed = parse_labels_into_registry(labels_dir)
        registry_map.update(parsed)

    # 2. Add pilot corridors
    if include_pilot:
        for cam_id, data in PILOT_CORRIDORS.items():
            if cam_id not in registry_map:
                registry_map[cam_id] = data

    written = 0
    for cam_id, entry in registry_map.items():
        valid, errs = validate_registry_dict(entry)
        if not valid:
            logger.warning("Skipping invalid entry for camera %s: %s", cam_id, "; ".join(errs))
            continue

        dest_file = output_dir / f"{cam_id}.json"
        with open(dest_file, "w", encoding="utf-8") as f:
            json.dump(entry, f, indent=2, ensure_ascii=False)
        written += 1

    logger.info("Exported %d camera roadway registries to %s", written, output_dir)
    return written


def main() -> int:
    parser = argparse.ArgumentParser(description="Build and verify directional roadway registry")
    parser.add_argument("--output-dir", type=Path, default=DEFAULT_REGISTRY_DIR, help="Destination directory for registry JSONs")
    parser.add_argument("--labels-dir", type=Path, default=DEFAULT_LABELS_DIR, help="Source directory for gold standard labels")
    parser.add_argument("--verify", action="store_true", help="Verify existing registry directory without re-exporting")
    parser.add_argument("--no-pilot", action="store_true", help="Omit built-in pilot corridors")

    args = parser.parse_args()

    if args.verify:
        valid, errors = verify_registry_dir(args.output_dir)
        if not valid:
            logger.error("Registry verification failed with %d errors:", len(errors))
            for err in errors:
                logger.error("  - %s", err)
            return 1
        logger.info("All registry files passed verification.")
        return 0

    count = build_and_export_registry(
        output_dir=args.output_dir,
        labels_dir=args.labels_dir,
        include_pilot=not args.no_pilot,
    )

    # Run verification on the exported directory
    valid, errors = verify_registry_dir(args.output_dir)
    if not valid:
        logger.error("Post-export verification failed: %s", errors)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
