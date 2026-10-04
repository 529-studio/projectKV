#!/usr/bin/env python3
"""
Hot Set Camera Curation Tool for Vehicle Detection Pipeline (projectKV).

Implements Sprint 3 Issue #136 (S2) and ADR-004 §10, §13, §14, §15.
Curates an explicit, reproducible Hot Set of intersection cameras for
self-hosted OpenVINO vehicle detection based on the capacity decision
confirmed in S1 (Option 1b: 57 cameras across 4 priority corridors on a 30s cycle).

Filter Rule:
  1. road_keys.length == 2 (intersection cameras only per A6; separates mid-block from junctions).
  2. At least one key in the confirmed priority corridors (vo_van_kiet, mai_chi_tho, dien_bien_phu, cong_hoa).
  3. Camera is strictly active and online (isOnline == true and status == 'UP').
  4. Excludes duplicate streams (duplicateOf is empty) and ghost cameras (isGhost is false).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Any, Dict, List, Optional, Set, Tuple

# Default priority corridors confirmed in Sprint 3 S1 (Option 1b)
# Reference: docs/spike-vehicle-capacity.md §5
DEFAULT_PRIORITY_CORRIDORS: List[str] = [
    "vo_van_kiet",
    "mai_chi_tho",
    "dien_bien_phu",
    "cong_hoa",
]

CORRIDOR_DISPLAY_NAMES: Dict[str, str] = {
    "vo_van_kiet": "Võ Văn Kiệt",
    "mai_chi_tho": "Mai Chí Thọ",
    "dien_bien_phu": "Điện Biên Phủ",
    "cong_hoa": "Cộng Hòa",
    "pham_van_dong": "Phạm Văn Đồng",
    "quoc_lo_1": "Quốc lộ 1",
    "nguyen_van_linh": "Nguyễn Văn Linh",
    "xa_lo_ha_noi": "Xa Lộ Hà Nội",
}


def is_eligible_camera(cam: Dict[str, Any]) -> bool:
    """Checks whether a camera stream is active, online, and non-duplicate."""
    if not cam.get("isOnline", False):
        return False
    if cam.get("status") != "UP":
        return False
    if cam.get("isGhost", False):
        return False
    if cam.get("duplicateOf"):
        return False
    return True


def filter_curated_cameras(
    cameras: List[Dict[str, Any]],
    corridors: List[str],
) -> Tuple[List[Dict[str, Any]], Dict[str, int]]:
    """
    Filters cameras matching the S2 intersection curation rule.

    Returns:
        Tuple of (curated_cameras_list, counts_by_corridor_dict).
    """
    target_corridors_set = set(corridors)
    counts_by_corridor: Dict[str, int] = {c: 0 for c in corridors}
    curated_cameras: List[Dict[str, Any]] = []
    seen_ids: Set[str] = set()

    for cam in cameras:
        cid = cam.get("id")
        if not cid or cid in seen_ids:
            continue

        if not is_eligible_camera(cam):
            continue

        # A6 intersection filter rule: road_keys length must be exactly 2
        rkeys = cam.get("road_keys") or []
        if len(rkeys) != 2:
            continue

        matched_corridors = [k for k in rkeys if k in target_corridors_set]
        if not matched_corridors:
            continue

        seen_ids.add(cid)

        # Primary corridor is the first matching target corridor
        primary_corridor = matched_corridors[0]
        counts_by_corridor[primary_corridor] = counts_by_corridor.get(primary_corridor, 0) + 1

        # Build clean curated record
        record = {
            "id": cid,
            "displayName": cam.get("displayName") or cam.get("title") or cid,
            "title": cam.get("title", ""),
            "latitude": cam.get("latitude"),
            "longitude": cam.get("longitude"),
            "snapshotUrl": cam.get("snapshotUrl", ""),
            "corridor": primary_corridor,
            "matched_corridors": matched_corridors,
            "road_keys": rkeys,
            "road_name": cam.get("road_name", ""),
            "phuong_name": cam.get("phuong_name", ""),
            "phuong_type": cam.get("phuong_type", ""),
            "zone": cam.get("zone", ""),
            "h3_res7": cam.get("h3_res7", ""),
        }
        curated_cameras.append(record)

    # Deterministic sorting: primary corridor order, then displayName
    corridor_order = {c: i for i, c in enumerate(corridors)}
    curated_cameras.sort(
        key=lambda c: (
            corridor_order.get(c["corridor"], 999),
            c["displayName"].lower(),
            c["id"],
        )
    )

    return curated_cameras, counts_by_corridor


def build_manifest(
    curated_cameras: List[Dict[str, Any]],
    counts_by_corridor: Dict[str, int],
    corridors: List[str],
) -> Dict[str, Any]:
    """Wraps curated cameras in a reproducible manifest envelope."""
    return {
        "metadata": {
            "description": "Curated Hot Set vehicle-detection camera manifest (Issue #136, S2)",
            "reference": "docs/spike-vehicle-capacity.md §5 (Option 1b)",
            "rule": "road_keys.length == 2 AND road_keys ∩ corridors != empty AND isOnline == true AND status == 'UP' AND not isGhost AND not duplicateOf",
            "corridors": corridors,
            "total_cameras": len(curated_cameras),
            "counts_by_corridor": counts_by_corridor,
        },
        "cameras": curated_cameras,
    }


def format_summary_table(
    counts_by_corridor: Dict[str, int],
    total_cameras: int,
) -> str:
    """Formats a concise terminal summary table."""
    lines = [
        "=================================================================",
        "        HOT SET VEHICLE-DETECTION CAMERA CURATION (S2)           ",
        "=================================================================",
        f"{'Corridor Name':<25} {'road_key':<20} {'Cameras':>8}",
        "-----------------------------------------------------------------",
    ]
    for rk, count in counts_by_corridor.items():
        name = CORRIDOR_DISPLAY_NAMES.get(rk, rk)
        lines.append(f"{name:<25} {rk:<20} {count:>8}")
    lines.append("-----------------------------------------------------------------")
    lines.append(f"{'Total Curated Cameras:':<46} {total_cameras:>8}")
    lines.append("=================================================================")
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Curate Hot Set vehicle-detection camera list via A6 road_keys filter."
    )
    parser.add_argument(
        "--input",
        "-i",
        default="cameras_enriched.json",
        help="Path to input enriched cameras JSON file (default: cameras_enriched.json)",
    )
    parser.add_argument(
        "--output",
        "-o",
        default=os.path.join(
            os.path.dirname(__file__), "data", "curated_hotset_cameras.json"
        ),
        help="Path to output curated cameras manifest JSON (default: tools/geo-enrich/data/curated_hotset_cameras.json)",
    )
    parser.add_argument(
        "--corridors",
        "-c",
        default=",".join(DEFAULT_PRIORITY_CORRIDORS),
        help=f"Comma-separated list of target corridor road_keys (default: {','.join(DEFAULT_PRIORITY_CORRIDORS)})",
    )
    parser.add_argument(
        "--summary",
        "-s",
        action="store_true",
        default=True,
        help="Print summary table to stdout (default: True)",
    )
    parser.add_argument(
        "--export-ids",
        action="store_true",
        help="Output raw JSON array of camera IDs only",
    )

    args = parser.parse_args()

    # Resolve input path
    input_path = args.input
    if not os.path.isabs(input_path) and not os.path.exists(input_path):
        # Fallback to repo root if run from subfolder
        candidate = os.path.join(os.path.dirname(__file__), "..", "..", input_path)
        if os.path.exists(candidate):
            input_path = os.path.abspath(candidate)

    if not os.path.exists(input_path):
        sys.stderr.write(f"[ERROR] Input cameras file not found: {input_path}\n")
        sys.exit(1)

    with open(input_path, "r", encoding="utf-8") as f:
        cameras_data = json.load(f)

    if not isinstance(cameras_data, list):
        sys.stderr.write("[ERROR] Expected input JSON to be an array of cameras.\n")
        sys.exit(1)

    corridors = [k.strip() for k in args.corridors.split(",") if k.strip()]
    curated, counts = filter_curated_cameras(cameras_data, corridors)

    # Ensure output directory exists
    os.makedirs(os.path.dirname(os.path.abspath(args.output)), exist_ok=True)

    if args.export_ids:
        ids_only = [c["id"] for c in curated]
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(ids_only, f, indent=2, ensure_ascii=False)
            f.write("\n")
    else:
        manifest = build_manifest(curated, counts, corridors)
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(manifest, f, indent=2, ensure_ascii=False)
            f.write("\n")

    if args.summary:
        print(format_summary_table(counts, len(curated)))
        print(f"[INFO] Curated manifest saved to: {args.output}")


if __name__ == "__main__":
    main()
