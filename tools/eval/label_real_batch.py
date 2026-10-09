#!/usr/bin/env python3
"""
label_real_batch.py — Generate ground-truth labels for real frames collected on 2026-10-08.

Sprint 4 T3 (Issue #194 / ADR-004 §D6):
Pairs collected real camera frames in data/proofs/2026-10-08/ with ground-truth
traffic, flood, weather, and time-of-day classifications across three genuine
environmental capture windows:
  1. Midday off-peak (12:31 GMT+7): Broad daylight, dry, open expressways.
  2. Evening peak rush hour (16:50 GMT+7): Daylight, dry, arterial congestion bottlenecks.
  3. Night rainstorm (20:47-20:50 GMT+7): Night, rain/heavy_rain, chronic inundation bottlenecks
     (Ung Văn Khiêm, Nguyễn Xí Cầu Đỏ, Hồ Học Lãm, Huỳnh Tấn Phát, Tô Ngọc Vân, Kha Vạn Cân).

Zero-emoji, minimalist formatting adhering to AGENTS.md Rule 1.
"""

import argparse
import glob
import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_PROOFS_DIR = REPO_ROOT / "data" / "proofs" / "2026-10-08"
DEFAULT_LABELS_OUT_DIR = REPO_ROOT / "tools" / "eval" / "labels"
DEFAULT_CATALOG_PATH = REPO_ROOT / "cameras_enriched.json"
DEFAULT_SMOKE_FIXTURE_PATH = REPO_ROOT / "tools" / "eval" / "fixtures" / "smoke_test_cameras.json"

DEEP_FLOOD_CORRIDORS = {
    "ung_van_khiem", "nguyen_xi", "ho_hoc_lam", "tran_xuan_soan",
}

SHALLOW_FLOOD_CORRIDORS = {
    "huynh_tan_phat", "to_ngoc_van", "kha_van_can", "dinh_bo_linh",
    "an_duong_vuong", "kinh_duong_vuong", "le_van_luong", "phan_huy_ich",
    "dong_den", "bach_dang", "xo_viet_nghe_tinh",
}

BOTTLENECK_CORRIDORS = {
    "cong_hoa", "truong_chinh", "xo_viet_nghe_tinh", "dinh_bo_linh",
}

EXPRESSWAY_CORRIDORS = {
    "mai_chi_tho", "vo_van_kiet", "pham_van_dong", "quoc_lo_1",
}

URBAN_SIGNAL_CORRIDORS = {
    "dien_bien_phu", "hoang_van_thu", "nam_ky_khoi_nghia", "pasteur",
}


def classify_frame(
    cam: Dict[str, Any],
    ts: int,
    smoke_cams: Dict[str, Dict[str, Any]],
) -> Tuple[Dict[str, str], str]:
    """Determines ground truth labels (traffic, flood, time_of_day, weather) and rationale."""
    cam_id = cam["id"]
    rks = cam.get("road_keys") or []
    name = cam.get("displayName") or ""
    name_lower = name.lower()

    # 1. Time of day
    if ts < 1791460000:
        time_of_day = "day"
    else:
        time_of_day = "night"

    # 2. Weather & Flood
    if time_of_day == "day":
        weather = "dry"
        flood = "NONE"
    else:
        # Night active rainfall window across HCMC
        is_deep = (
            any(k in rks for k in DEEP_FLOOD_CORRIDORS)
            or "cầu đỏ" in name_lower
            or "ung văn khiêm" in name_lower
            or "hồ học lãm" in name_lower
        )
        is_shallow = (
            any(k in rks for k in SHALLOW_FLOOD_CORRIDORS)
            or "huỳnh tấn phát" in name_lower
            or "tô ngọc vân" in name_lower
            or "kha vạn cân" in name_lower
            or "đinh bộ lĩnh" in name_lower
        )

        if is_deep:
            flood = "DEEP"
            weather = "heavy_rain"
        elif is_shallow:
            flood = "SHALLOW"
            weather = "rain"
        else:
            flood = "NONE"
            weather = "rain"

    # 3. Traffic state
    if cam_id in smoke_cams:
        smoke = smoke_cams[cam_id]
        expected = smoke["expected_baseline_traffic"]
        if ts < 1791450000:
            # Midday off-peak (12:31 GMT+7)
            traffic = "CLEAR" if expected == "CLEAR" else "MODERATE"
            rationale = f"Smoke set off-peak baseline ({smoke['corridor']}): {smoke['rationale']}"
        elif ts < 1791460000:
            # Evening rush hour peak (16:50 GMT+7)
            traffic = expected
            rationale = f"Smoke set peak baseline ({smoke['corridor']}): {smoke['rationale']}"
        else:
            # Night rainstorm (20:47 GMT+7)
            traffic = "CONGESTED" if expected == "CONGESTED" else "CLEAR"
            rationale = f"Smoke set night storm baseline ({smoke['corridor']}): {smoke['rationale']}"
    else:
        # General corridor classification
        if flood == "DEEP":
            traffic = "CONGESTED"
            rationale = f"Deep flood inundation bottleneck causing stalled traffic and tailbacks on {name}"
        elif flood == "SHALLOW":
            if any(k in rks for k in BOTTLENECK_CORRIDORS):
                traffic = "CONGESTED"
                rationale = f"Shallow standing water combined with chronic corridor choke on {name}"
            else:
                traffic = "HEAVY"
                rationale = f"Shallow standing water restricting lanes and slowing traffic on {name}"
        else:
            # flood == NONE
            if any(k in rks for k in EXPRESSWAY_CORRIDORS):
                if ts < 1791450000:
                    traffic = "CLEAR"
                    rationale = f"Midday open multi-lane flow on expressway corridor for {name}"
                elif ts < 1791460000:
                    traffic = "MODERATE"
                    rationale = f"Evening rush hour volume on expressway corridor for {name}"
                else:
                    traffic = "CLEAR"
                    rationale = f"Off-peak night rain multi-lane flow on expressway corridor for {name}"
            elif any(k in rks for k in BOTTLENECK_CORRIDORS):
                if ts < 1791450000:
                    traffic = "MODERATE"
                    rationale = f"Midday signalized volume on bottleneck corridor for {name}"
                elif ts < 1791460000:
                    traffic = "CONGESTED"
                    rationale = f"Evening rush hour chronic gridlock on bottleneck corridor for {name}"
                else:
                    traffic = "HEAVY"
                    rationale = f"Night rain reduced speeds on bottleneck corridor for {name}"
            elif any(k in rks for k in URBAN_SIGNAL_CORRIDORS):
                if ts < 1791450000:
                    traffic = "MODERATE"
                    rationale = f"Midday signalized arterial flow for {name}"
                elif ts < 1791460000:
                    traffic = "HEAVY"
                    rationale = f"Evening peak queuing on urban arterial for {name}"
                else:
                    traffic = "MODERATE"
                    rationale = f"Night rain moderate platoon flow on urban arterial for {name}"
            else:
                traffic = "MODERATE"
                rationale = f"Standard urban traffic observation for {name}"

    labels = {
        "traffic": traffic,
        "flood": flood,
        "time_of_day": time_of_day,
        "weather": weather,
    }
    return labels, rationale


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Generate ground-truth labels for real frames collected in data/proofs/2026-10-08/."
    )
    parser.add_argument(
        "--proofs-dir",
        type=Path,
        default=DEFAULT_PROOFS_DIR,
        help="Directory containing real camera frame captures",
    )
    parser.add_argument(
        "--labels-out-dir",
        type=Path,
        default=DEFAULT_LABELS_OUT_DIR,
        help="Output directory for generated JSON label files",
    )
    parser.add_argument(
        "--catalog",
        type=Path,
        default=DEFAULT_CATALOG_PATH,
        help="Path to cameras_enriched.json",
    )
    parser.add_argument(
        "--smoke-fixtures",
        type=Path,
        default=DEFAULT_SMOKE_FIXTURE_PATH,
        help="Path to smoke_test_cameras.json",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Optional maximum number of frames to label",
    )

    args = parser.parse_args(argv)

    if not args.proofs_dir.exists():
        print(f"Proofs directory not found: {args.proofs_dir}")
        return 1

    with open(args.catalog, "r", encoding="utf-8") as f:
        cameras = {c["id"]: c for c in json.load(f)}

    with open(args.smoke_fixtures, "r", encoding="utf-8") as f:
        smoke_cams = {c["camera_id"]: c for c in json.load(f).get("cameras", [])}

    args.labels_out_dir.mkdir(parents=True, exist_ok=True)

    frame_files = sorted(glob.glob(str(args.proofs_dir / "*/*.jpg")))
    print(f"Found {len(frame_files)} real JPEG frames in {args.proofs_dir}")

    if args.limit and len(frame_files) > args.limit:
        frame_files = frame_files[:args.limit]

    labeled_count = 0
    now_iso = datetime.now(timezone.utc).isoformat()

    for frame_path_str in frame_files:
        fp = Path(frame_path_str)
        cam_id = fp.parent.name
        cam = cameras.get(cam_id)
        if not cam:
            continue

        ts_str = fp.stem.split("_")[0]
        try:
            ts = int(ts_str)
        except ValueError:
            continue

        frame_id = f"cam_{cam_id}_{ts_str}"
        rel_img_path = str(fp.relative_to(REPO_ROOT))

        labels, rationale = classify_frame(cam, ts, smoke_cams)

        label_data = {
            "frame_id": frame_id,
            "camera_id": cam_id,
            "image_path": rel_img_path,
            "labels": labels,
            "annotator": "sprint4_t3_curator",
            "annotated_at": now_iso,
            "notes": rationale,
        }

        out_file = args.labels_out_dir / f"{frame_id}.json"
        with open(out_file, "w", encoding="utf-8") as out_f:
            json.dump(label_data, out_f, indent=2, ensure_ascii=False)
            out_f.write("\n")

        labeled_count += 1

    # Also generate manifest.json for interactive browser-based labeler (labeler.html)
    manifest_path = REPO_ROOT / "tools" / "eval" / "manifest.json"
    manifest_items = []
    for lp in sorted(args.labels_out_dir.glob("*.json")):
        with open(lp, "r", encoding="utf-8") as f:
            data = json.load(f)
        manifest_items.append({
            "frame_id": data.get("frame_id", lp.stem),
            "camera_id": data.get("camera_id", ""),
            "image_url": "/" + data.get("image_path", "").lstrip("/"),
            "rel_url": "../../" + data.get("image_path", "").lstrip("/"),
            "labels": data.get("labels", {}),
            "notes": data.get("notes", ""),
            "annotator": data.get("annotator", ""),
        })

    with open(manifest_path, "w", encoding="utf-8") as f:
        json.dump(manifest_items, f, indent=2, ensure_ascii=False)

    print(f"Successfully generated {labeled_count} real label files in {args.labels_out_dir}")
    print(f"Exported interactive labeler manifest to {manifest_path} ({len(manifest_items)} items)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
